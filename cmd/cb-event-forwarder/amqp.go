package main

import (
	"crypto/tls"
	"crypto/x509"
	"expvar"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/pbmessageprocessor"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"io/ioutil"
	"runtime"
	"strings"
	"sync"
	"time"
)

/*
 * AMQP bookkeeping
 */

func (c *Consumer) Consume() {
	c.startExpvarPublish()
	go func() {
		for {
			log.Infof("Starting AMQP loop for %s on queue %s ", c.CbServerName, c.amqpURI)
			for {
				err := c.Connect()
				if err != nil {
					log.Infof("Consumer couldn't connect - Will try again in 30 seconds")
					time.Sleep(30 * time.Second)
				} else {
					log.Infof("Consumer connected succesfully!")
					break
				}
			}

			numProcessors := runtime.NumCPU() * 2
			numProcessors = 1
			log.Infof("Starting %d message processors\n", numProcessors)
			c.wg.Add(numProcessors)
			workersstopchans := make([]chan struct{}, numProcessors)
			for w := 0; w < numProcessors; w++ {

				stopchan := make(chan struct{})
				workersstopchans[w] = stopchan
				//inline worker goroutine
				log.Infof("Launching AMQP message processor %d goroutine w/ deliveries = %s", w, c.deliveries)
				go func() {
					defer c.wg.Done()
					/*
						for delivery := range c.deliveries {
							log.Infof("AMQP WORKING PROCESSING DELIVERY")
							c.processMessage(delivery.Body,
								delivery.RoutingKey,
								delivery.ContentType,
								delivery.Headers,
								delivery.Exchange)
						}*/
					for {
						select {
						case d := <-c.deliveries:
							log.Infof("AMQP WORKING PROCESSING DELIVERY")
							c.processMessage(d.Body,
								d.RoutingKey,
								d.ContentType,
								d.Headers,
								d.Exchange)
						case <-stopchan:
							return
						}
					}
					log.Info("Worker exiting")
				}()
			}

			log.Infof("AMQP LOOP GOING TO SELECT stopchan is %s", &c.stopchan)

			for {
				select {
				case closeError := <-c.ConnectionErrors:
					c.status.IsConnected = false
					c.status.LastConnectError = closeError.Error()
					c.status.ErrorTime = time.Now()
					log.Errorf("Connection closed: %s", closeError.Error())
					log.Info("Waiting for all workers to exit")
					c.wg.Wait()
					log.Info("All workers have exited")
				case <-c.stopchan:
					log.Infof("Consumer told to stop - stopping workers")
					for _, workerstopchan := range workersstopchans {
						workerstopchan <- struct{}{}
					}
					c.wg.Wait()
					log.Info("Consumer - all workers done - ")
					return
				}
			}
			//wait and reconnect
			log.Infof("Loop exited for unknown reason")
			c.Shutdown()
			c.wg.Wait()
			log.Infof("Loop exited - Will try again in 30 seconds")
			time.Sleep(30 * time.Second)
		}
	}()
}

func GetAMQPTLSConfig(tls_ca_cert, tls_client_cert, tls_client_key string, tls_insecure bool) (*tls.Config, error) {
	cfg := new(tls.Config)
	caCert, err := ioutil.ReadFile(tls_ca_cert)
	if err != nil {
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	cfg.RootCAs = caCertPool

	cert, err := tls.LoadX509KeyPair(tls_client_cert, tls_client_key)
	if err != nil {
		return nil, err
	}
	cfg.Certificates = []tls.Certificate{cert}
	cfg.InsecureSkipVerify = tls_insecure
	return cfg, nil
}

func NewConsumerFromConf(outputMessageFunc func(map[string]interface{}) error, serverName, consumerName string, consumerCfg map[interface{}]interface{}, debugFlag bool, debugStore string) (*Consumer, error) {
	var consumerTlsCfg *tls.Config = nil
	if temp, ok := consumerCfg["tls"]; ok {
		tlsCfg := temp.(map[string]interface{})
		tls_ca_cert := ""
		if temp, ok := tlsCfg["ca_cert"]; ok {
			tls_ca_cert = temp.(string)
		}
		tls_client_cert := ""
		if temp, ok := tlsCfg["client_cert"]; ok {
			tls_client_cert = temp.(string)
		}
		tls_client_key := ""
		if temp, ok := tlsCfg["client_key"]; ok {
			tls_client_key = temp.(string)
		}
		tls_insecure := false
		if temp, ok := tlsCfg["tls_insecure"]; ok {
			tls_insecure = temp.(bool)
		}
		consumerTlsCfg, _ = GetAMQPTLSConfig(tls_ca_cert, tls_client_cert, tls_client_key, tls_insecure)
	}

	durableQueues := false
	if temp, ok := consumerCfg["rabbit_mq_durable_queues"]; ok {
		durableQueues = temp.(bool)
	}

	auditLogging := false
	if temp, ok := consumerCfg["audit_log"]; ok {
		auditLogging = temp.(bool)
	}

	automaticAcking := true
	if temp, ok := consumerCfg["rabbit_mq_automatic_acking"]; ok {
		automaticAcking = temp.(bool)
	}

	bindToRawExchange := false
	if temp, ok := consumerCfg["bind_raw_exchange"]; ok {
		bindToRawExchange = temp.(bool)
	}

	ctag := "go-event-consumer-" + consumerName
	if temp, ok := consumerCfg["rabbit_mq_consumer_tag"]; ok {
		ctag = temp.(string)
	}

	amqpPassword := ""
	if temp, ok := consumerCfg["rabbit_mq_password"]; ok {
		amqpPassword = temp.(string)
	}

	cbServerURL := ""
	if temp, ok := consumerCfg["cb_server_url"]; ok {
		cbServerURL = temp.(string)
	}

	amqpHostname := "localhost"
	if temp, ok := consumerCfg["rabbit_mq_hostname"]; ok {
		amqpHostname = temp.(string)
	}

	amqpUsername := "cb"
	if temp, ok := consumerCfg["rabbit_mq_username"]; ok {
		amqpUsername = temp.(string)
	}
	amqpPort := 5004
	if temp, ok := consumerCfg["rabbit_mq_port"]; ok {
		amqpPort = temp.(int)
	}

	scheme := "amqp"
	if consumerTlsCfg != nil {
		scheme = "ampqs"
	}
	amqpURI := fmt.Sprintf("%s://%s:%s@%s:%d", scheme, amqpUsername, amqpPassword, amqpHostname, amqpPort)

	var eventMap map[string][]string = make(map[string][]string)
	var eventNames []string = make([]string, 0)
	if temp, ok := consumerCfg["event_map"]; ok {
		t := temp.(map[string][]string)
		for k, v := range t {
			eventMap[k] = v
		}
	} else {
		//Default to ALL events
		eventMap = map[string][]string{"events_watchlist": []string{
			"watchlist.#",
		},
			"events_feed": []string{
				"feed.#",
			},
			"events_alert": []string{
				"alert.#",
			},
			"events_raw_sensor": []string{
				"ingress.event.process",
				"ingress.event.procstart",
				"ingress.event.netconn",
				"ingress.event.procend",
				"ingress.event.childproc",
				"ingress.event.moduleload",
				"ingress.event.module",
				"ingress.event.filemod",
				"ingress.event.regmod",
				"ingress.event.tamper",
				"ingress.event.crossprocopen",
				"ingress.event.remotethread",
				"ingress.event.processblock",
				"ingress.event.emetmitigation",
			},
			"events_binary_observed": []string{
				"binaryinfo.#",
			},
			"events_binary_upload": []string{
				"binarystore.#",
			},
			"events_storage_partition": []string{
				"events.partition.#",
			},
		}
	}

	for _, names := range eventMap {
		for _, name := range names {
			eventNames = append(eventNames, name)
		}
	}

	return NewConsumer(outputMessageFunc, serverName, cbServerURL, consumerTlsCfg, auditLogging, durableQueues, automaticAcking, bindToRawExchange, amqpURI, ctag, eventNames, debugFlag, debugStore)
}

type Consumer struct {
	conn                      *amqp.Connection
	channel                   *amqp.Channel
	tag                       string
	stopchan                  chan struct{}
	CbServerName              string
	CbServerURL               string
	PerformFeedPostprocessing bool
	jsmp                      jsonmessageprocessor.JsonMessageProcessor
	pbmp                      pbmessageprocessor.PbMessageProcessor
	status                    ConsumerStatus
	wg                        sync.WaitGroup
	OutputMessageFunc         func(msg map[string]interface{}) error
	deliveries                <-chan amqp.Delivery
	DebugFlag                 bool
	DebugStore                string
	ConnectionErrors          chan *amqp.Error
	routingKeys               []string
	tls                       *tls.Config
	durableQueues             bool
	automaticAcking           bool
	bindToRawExchange         bool
	amqpURI                   string
	AuditLogging              bool
}

func NewConsumer(outputMessageFunc func(map[string]interface{}) error, serverName, serverURL string, tlsCfg *tls.Config, auditLogging, durableQueues, automaticAcking, bindToRawExchange bool, amqpURI, ctag string, routingKeys []string, debugFlag bool, debugStore string) (*Consumer, error) {
	c := &Consumer{
		conn:              nil,
		channel:           nil,
		tag:               ctag,
		CbServerName:      serverName,
		CbServerURL:       serverURL,
		DebugFlag:         debugFlag,
		DebugStore:        debugStore,
		routingKeys:       routingKeys,
		OutputMessageFunc: outputMessageFunc,
		ConnectionErrors:  make(chan *amqp.Error, 1),
		tls:               tlsCfg,
		durableQueues:     durableQueues,
		automaticAcking:   automaticAcking,
		bindToRawExchange: bindToRawExchange,
		amqpURI:           amqpURI,
		AuditLogging:      auditLogging,
		stopchan:          make(chan struct{}),
	}

	return c, nil
}

func (c *Consumer) Connect() error {

	var err error

	if c.tls != nil {
		log.Info("Connecting to message bus via TLS...")
		c.conn, err = amqp.DialTLS(c.amqpURI, c.tls)

		if err != nil {
			return err
		}
	} else {
		log.Info("Connecting to message bus...")
		c.conn, err = amqp.Dial(c.amqpURI)

		if err != nil {
			return err
		}
	}

	c.channel, err = c.conn.Channel()
	if err != nil {
		return err
	}

	queue, err := c.channel.QueueDeclare(
		c.CbServerName,
		c.durableQueues, // durable,
		true,            // delete when unused
		false,           // exclusive
		false,           // nowait
		nil,             // arguments
	)
	if err != nil {
		return err
	}

	if c.bindToRawExchange {
		err = c.channel.QueueBind(c.CbServerName, "", "api.rawsensordata", false, nil)
		if err != nil {
			return err
		}
		log.Info("Subscribed to bulk raw sensor event exchange")
	}

	for _, key := range c.routingKeys {
		err = c.channel.QueueBind(c.CbServerName, key, "api.events", false, nil)
		if err != nil {
			return err
		}
		log.Infof("Subscribed to %s", key)
	}

	deliveries, err := c.channel.Consume(
		queue.Name,
		c.tag,
		c.automaticAcking, // automatic or manual acking
		false,             // exclusive
		false,             // noLocal
		false,             // noWait
		nil,               // arguments
	)

	if err != nil {
		return err
	}
	c.deliveries = deliveries
	c.conn.NotifyClose(c.ConnectionErrors)
	return nil
}

func (c *Consumer) Shutdown() error {
	if err := c.channel.Cancel(c.tag, true); err != nil {
		return fmt.Errorf("Consumer cancel failed: %s", err)
	}

	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("AMQP connection close error: %s", err)
	}

	defer log.Infof("AMQP shutdown OK")

	return nil
}

func (c *Consumer) logFileProcessingLoop() <-chan error {

	errChan := make(chan error)

	spawnTailer := func(fName string, label string) {

		log.Debugf("Spawn tailer: %s", fName)

		_, deliveries, err := NewFileConsumer(fName)

		if err != nil {
			//c.status.LastConnectError = err.Error()
			//c.status.ErrorTime = time.Now()
			errChan <- err
		}

		for delivery := range deliveries {
			log.Debug("Trying to deliver log message %s", delivery)
			msgMap := make(map[string]interface{})
			msgMap["message"] = strings.TrimSuffix(delivery, "\n")
			msgMap["type"] = label
			c.OutputMessageFunc(msgMap)
		}

	}

	/* maps audit log labels to event types
	AUDIT_TYPES = {
	    "cb-audit-isolation": Audit_Log_Isolation,
	    "cb-audit-banning": Audit_Log_Banning,
	    "cb-audit-live-response": Audit_Log_Liveresponse,
	    "cb-audit-useractivity": Audit_Log_Useractivity
	}
	*/

	go spawnTailer("/var/log/cb/audit/live-response.log", "audit.log.liveresponse")
	go spawnTailer("/var/log/cb/audit/banning.log", "audit.log.banning")
	go spawnTailer("/var/log/cb/audit/isolation.log", "audit.log.isolation")
	go spawnTailer("/var/log/cb/audit/useractivity.log", "audit.log.useractivity")
	return errChan
}

func (c *Consumer) startExpvarPublish() {
	c.status.InputEventCount = expvar.NewInt("input_event_count")
	c.status.ErrorCount = expvar.NewInt("error_count")
	c.status.StartTime = time.Now()
	expvar.Publish(fmt.Sprintf("uptime_%s", c.CbServerName), expvar.Func(func() interface{} {
		return time.Now().Sub(c.status.StartTime).Seconds()
	}))
	expvar.Publish(fmt.Sprintf("subscribed_events_%s", c.CbServerName), expvar.Func(func() interface{} {
		return c.routingKeys
	}))
}
