package consumer

import (
	"bytes"
	"crypto/md5"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"expvar"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/pbmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/sensor_events"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"github.com/vaughan0/go-ini"
	"io/ioutil"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"
)

/*
 * AMQP bookkeeping
 */

// TODO: change this into an error channel
func (c *Consumer) reportError(d string, errmsg string, err error) {
	//c.status.ErrorCount.Add(1)
	log.Errorf("%s when processing %s: %s", errmsg, d, err)
}

func reportBundleDetails(routingKey string, body []byte, headers amqp.Table, debugFlag bool, debugStore string) {
	log.Errorf("Error while processing message through routing key %s:", routingKey)

	var env *sensor_events.CbEnvironmentMsg
	env, err := pbmessageprocessor.CreateEnvMessage(headers)
	if err != nil {
		log.Errorf("  Message was received from sensor %d; hostname %s", env.Endpoint.GetSensorId(),
			env.Endpoint.GetSensorHostName())
	}

	if len(body) < 4 {
		log.Errorf("  Message is less than 4 bytes long; malformed")
	} else {
		log.Info("  First four bytes of message were:")
		log.Errorf("  %s", hex.Dump(body[0:4]))
	}

	/*
	 * We are going to store this bundle in the DebugStore
	 */
	if debugFlag {
		h := md5.New()
		h.Write(body)
		var fullFilePath string
		fullFilePath = path.Join(debugStore, fmt.Sprintf("/event-forwarder-%X", h.Sum(nil)))
		log.Debugf("Writing Bundle to disk: %s", fullFilePath)
		ioutil.WriteFile(fullFilePath, body, 0444)
	}
}

func (c *Consumer) Consume() {
	c.status.StartTime = time.Now()
	c.startExpvarPublish()
	c.wg.Add(1)
	go func() {
		for {
			log.Infof("Starting AMQP loop for %s on queue %s ", c.CbServerName, c.amqpURI)
			for {
				err := c.Connect()
				if err != nil {
					log.Infof("Consumer couldn't connect - Will try again in 30 seconds")
					//time.Sleep(30 * time.Second)
					select {
					case <-c.Stopchan:
						c.wg.Done()
						log.Infof("Consumer told to exit before connection succesfully established")
						return
					case <-time.After(30 * time.Second):
					}
				} else {
					log.Infof("Consumer connected succesfully!")
					break
				}
			}

			numProcessors := runtime.NumCPU() * 2
			numProcessors = 1
			log.Infof("Starting %d message processors\n", numProcessors)
			var wg sync.WaitGroup
			wg.Add(numProcessors)
			workersstopchans := make([]chan struct{}, numProcessors)
			for w := 0; w < numProcessors; w++ {
				stopchan := make(chan struct{})
				workersstopchans[w] = stopchan
				log.Debugf("Launching AMQP message processor %d goroutine w/ deliveries = %s", w, c.deliveries)
				go func() {
					defer wg.Done()
					for {
						select {
						case d := <-c.deliveries:
							log.Debugf("AMQP WORKING PROCESSING DELIVERY")
							c.processMessage(d.Body,
								d.RoutingKey,
								d.ContentType,
								d.Headers,
								d.Exchange)
						case <-stopchan:
							log.Info("AMQP Worker exiting")
							return
						}
					}
				}()
			}

			log.Debugf("AMQP LOOP GOING TO SELECT stopchan is %s", &c.Stopchan)

			for {
				select {
				case closeError := <-c.ConnectionErrors:
					c.status.IsConnected = false
					c.status.LastConnectError = closeError.Error()
					c.status.ErrorTime = time.Now()
					log.Errorf("Connection closed: %s", closeError.Error())
					log.Debug("Waiting for all workers to exit")
					wg.Wait()
					log.Debug("All workers have exited")
				case <-c.Stopchan:
					log.Debug("Consumer told to stop - stopping workers")
					for _, workerstopchan := range workersstopchans {
						workerstopchan <- struct{}{}
					}
					c.Shutdown()
					log.Debug("Consumer - Waiting for workers to complete - ")
					wg.Wait()
					log.Debug("Consumer - all workers done -  Signalling cbef waitgroup done")
					c.wg.Done()
					return
				}
			}
			//wait and reconnect
			log.Debug("Loop exited for unknown reason")
			c.Shutdown()
			wg.Wait()
			log.Debug("Loop exited - Will try again in 30 seconds")
			time.Sleep(30 * time.Second)
		}
	}()
}

func GetAMQPTLSConfig(tls_ca_cert, tls_client_cert, tls_client_key string, tls_insecure bool) (*tls.Config, error) {
	cfg := new(tls.Config)
	caCert, err := ioutil.ReadFile(tls_ca_cert)
	if err != nil {
		log.Warnf("Error building consumer AMQPS tls config: %v", err)
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	cfg.RootCAs = caCertPool

	cert, err := tls.LoadX509KeyPair(tls_client_cert, tls_client_key)
	if err != nil {
		log.Warnf("Error building consumer AMQPS tls config: %v", err)
		return nil, err
	}
	cfg.Certificates = []tls.Certificate{cert}
	cfg.InsecureSkipVerify = tls_insecure
	return cfg, nil
}

func NewConsumerFromConf(outputMessageFunc func(map[string]interface{}) error, serverName, consumerName string, consumerCfg map[interface{}]interface{}, debugFlag bool, debugStore string, wg sync.WaitGroup) (*Consumer, error) {
	var consumerTlsCfg *tls.Config = nil
	if temp, ok := consumerCfg["tls"]; ok {
		log.Debugf("Trying to get tls config for amqp consumer...")
		tlsCfg := temp.(map[interface{}]interface{})
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

	usetimefloat := false
	if temp, ok := consumerCfg["use_time_float"]; ok {
		usetimefloat = temp.(bool)
	}

	ctag := "go-event-consumer-" + consumerName
	if temp, ok := consumerCfg["rabbit_mq_consumer_tag"]; ok {
		ctag = temp.(string)
	}
	amqpUsername := "cb"
	amqpPassword := ""
	if temp, ok := consumerCfg["rabbit_mq_password"]; ok {
		amqpPassword = temp.(string)
	} else { //load rabbit creds from (local) disk
		// TODO: make this only happen for 1 input entry , or something along those lines to prevent confusion ?
		var err error = nil
		amqpUsername, amqpPassword, err = GetLocalRabbitMQCredentials()
		if err != nil {
			log.Errorf("Couldn't get rabbit mq credentials from /etc/cb.conf")
			log.Panic("%v", err)
		}
	}

	amqpHostname := "localhost"
	if temp, ok := consumerCfg["rabbit_mq_hostname"]; ok {
		amqpHostname = temp.(string)
	}

	if temp, ok := consumerCfg["rabbit_mq_username"]; ok {
		amqpUsername = temp.(string)
	}
	amqpPort := 5004
	if temp, ok := consumerCfg["rabbit_mq_port"]; ok {
		amqpPort = temp.(int)
	}

	cbServerURL := ""
	if temp, ok := consumerCfg["cb_server_url"]; ok {
		cbServerURL = temp.(string)
	}

	scheme := "amqp"
	if consumerTlsCfg != nil {
		scheme = "ampqs"
	}
	amqpURI := fmt.Sprintf("%s://%s:%s@%s:%d", scheme, amqpUsername, amqpPassword, amqpHostname, amqpPort)

	var eventMap map[interface{}]interface{} = make(map[interface{}]interface{})
	var eventNames []string = make([]string, 0)
	if temp, ok := consumerCfg["event_map"]; ok {
		t := temp.(map[interface{}]interface{})
		/**for k, v := range t {
			eventMap[k] = v
		}*/
		eventMap = t
	} else {
		//Default to ALL events
		eventMap = map[interface{}]interface{}{
			"events_watchlist": []string{
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
		if ns, ok := names.([]string); ok {
			for _, name := range ns {
				eventNames = append(eventNames, name)
			}
		} else if ns, ok := names.([]interface{}); ok {
			for _, name := range ns {
				eventNames = append(eventNames, name.(string))
			}
		}
	}

	return NewConsumer(wg, outputMessageFunc, serverName, cbServerURL, consumerTlsCfg, auditLogging, durableQueues, automaticAcking, bindToRawExchange, amqpURI, ctag, eventNames, debugFlag, debugStore,usetimefloat)
}

type ConsumerStatus struct {
	InputEventCount  *expvar.Int
	ErrorCount       *expvar.Int
	IsConnected      bool
	LastConnectTime  time.Time
	StartTime        time.Time
	LastConnectError string
	ErrorTime        time.Time
	sync.RWMutex
}

type Consumer struct {
	Conn                      *amqp.Connection
	Channel                   *amqp.Channel
	Tag                       string
	Stopchan                  chan struct{}
	CbServerName              string
	CbServerURL               string
	PerformFeedPostprocessing bool
	Jsmp                      jsonmessageprocessor.JsonMessageProcessor
	Pbmp                      pbmessageprocessor.PbMessageProcessor
	status                    ConsumerStatus
	wg                        sync.WaitGroup
	OutputMessageFunc         func(msg map[string]interface{}) error
	deliveries                <-chan amqp.Delivery
	DebugFlag                 bool
	DebugStore                string
	ConnectionErrors          chan *amqp.Error
	RoutingKeys               []string
	tls                       *tls.Config
	durableQueues             bool
	automaticAcking           bool
	bindToRawExchange         bool
	amqpURI                   string
	AuditLogging              bool
}

func NewConsumer(wg sync.WaitGroup, outputMessageFunc func(map[string]interface{}) error, serverName, serverURL string, tlsCfg *tls.Config, auditLogging, durableQueues, automaticAcking, bindToRawExchange bool, amqpURI, ctag string, routingKeys []string, debugFlag bool, debugStore string, useTimeFloat bool) (*Consumer, error) {
	c := &Consumer{
		Conn:              nil,
		Channel:           nil,
		Tag:               ctag,
		CbServerName:      serverName,
		CbServerURL:       serverURL,
		DebugFlag:         debugFlag,
		DebugStore:        debugStore,
		RoutingKeys:       routingKeys,
		OutputMessageFunc: outputMessageFunc,
		ConnectionErrors:  make(chan *amqp.Error, 1),
		tls:               tlsCfg,
		durableQueues:     durableQueues,
		automaticAcking:   automaticAcking,
		bindToRawExchange: bindToRawExchange,
		amqpURI:           amqpURI,
		AuditLogging:      auditLogging,
		Stopchan:          make(chan struct{}),
		wg:                wg,
		status:            ConsumerStatus{InputEventCount: expvar.NewInt(fmt.Sprintf("%_input_event_count", serverName)), ErrorCount: expvar.NewInt(fmt.Sprintf("%_error_count", serverName))},
	}
	eventMap := make(map[string]interface{})
	for _, rk := range routingKeys {
		eventMap[rk] = true
	}
	pbmp := pbmessageprocessor.PbMessageProcessor{EventMap: eventMap, UseTimeFloat: useTimeFloat, DebugFlag: debugFlag, DebugStore: debugStore}
	c.Pbmp = pbmp

	jsmp := jsonmessageprocessor.JsonMessageProcessor{EventMap: eventMap, UseTimeFloat: useTimeFloat, DebugFlag: debugFlag, DebugStore: debugStore}
	c.Jsmp = jsmp

	return c, nil
}

func (c *Consumer) Connect() error {

	var err error

	if c.tls != nil {
		log.Debug("Connecting to message bus via TLS...")
		c.Conn, err = amqp.DialTLS(c.amqpURI, c.tls)

		if err != nil {
			return err
		}
	} else {
		log.Debug("Connecting to message bus...")
		c.Conn, err = amqp.Dial(c.amqpURI)

		if err != nil {
			return err
		}
	}

	c.Channel, err = c.Conn.Channel()
	if err != nil {
		return err
	}

	queue, err := c.Channel.QueueDeclare(
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
		err = c.Channel.QueueBind(c.CbServerName, "", "api.rawsensordata", false, nil)
		if err != nil {
			return err
		}
		log.Infof(" Subscribed to bulk raw sensor event exchange on %s", c.CbServerName)
	}

	for _, key := range c.RoutingKeys {
		err = c.Channel.QueueBind(c.CbServerName, key, "api.events", false, nil)
		if err != nil {
			return err
		}
		log.Infof("Subscribed to %s on %s", key, c.CbServerName)
	}

	deliveries, err := c.Channel.Consume(
		queue.Name,
		c.Tag,
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
	c.Conn.NotifyClose(c.ConnectionErrors)
	return nil
}

func (c *Consumer) Shutdown() error {
	if err := c.Channel.Cancel(c.Tag, true); err != nil {
		return fmt.Errorf("Consumer cancel failed: %s", err)
	}

	if err := c.Conn.Close(); err != nil {
		return fmt.Errorf("AMQP connection close error: %s", err)
	}

	defer log.Infof("AMQP shutdown OK")

	return nil
}

func (c *Consumer) LogFileProcessingLoop() <-chan error {

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
	expvar.Publish(fmt.Sprintf("uptime_%s", c.CbServerName), expvar.Func(func() interface{} {
		return time.Now().Sub(c.status.StartTime).Seconds()
	}))
	expvar.Publish(fmt.Sprintf("subscribed_events_%s", c.CbServerName), expvar.Func(func() interface{} {
		return c.RoutingKeys
	}))
}

func GetLocalRabbitMQCredentials() (username, password string, err error) {
	input, err := ini.LoadFile("/etc/cb/cb.conf")
	if err != nil {
		return username, password, err
	}
	username, _ = input.Get("cb", "RabbitMQUser")
	password, _ = input.Get("", "RabbitMQPassword")

	if len(username) == 0 || len(password) == 0 {
		return username, password, errors.New("Could not get RabbitMQ credentials from /etc/cb/cb.conf")
	}
	return username, password, nil
}

func (c *Consumer) processMessage(body []byte, routingKey, contentType string, headers amqp.Table, exchangeName string) {
	c.status.InputEventCount.Add(1)

	var err error
	var msgs []map[string]interface{}
	//
	// Process message based on ContentType
	//
	if contentType == "application/zip" {
		msgs, err = c.Pbmp.ProcessRawZipBundle(routingKey, body, headers)
		if err != nil {
			reportBundleDetails(routingKey, body, headers, c.DebugFlag, c.DebugStore)
			return
		}
	} else if contentType == "application/protobuf" {
		// if we receive a protobuf through the raw sensor exchange, it's actually a protobuf "bundle" and not a
		// single protobuf
		if exchangeName == "api.rawsensordata" {
			msgs, err = c.Pbmp.ProcessProtobufBundle(routingKey, body, headers)
		} else {
			msg, err := c.Pbmp.ProcessProtobufMessage(routingKey, body, headers)
			if err != nil {
				reportBundleDetails(routingKey, body, headers, c.DebugFlag, c.DebugStore)
				c.reportError(routingKey, "Could not process body", err)
				return
			} else if msg != nil {
				msgs = make([]map[string]interface{}, 0, 1)
				msgs = append(msgs, msg)
			}
		}
	} else if contentType == "application/json" {
		// Note for simplicity in implementation we are assuming the JSON output by the Cb server
		// is an object (that is, the top level JSON object is a dictionary and not an array or scalar value)
		var msg map[string]interface{}
		decoder := json.NewDecoder(bytes.NewReader(body))

		// Ensure that we decode numbers in the JSON as integers and *not* float64s
		decoder.UseNumber()

		if err := decoder.Decode(&msg); err != nil {
			c.reportError(string(body), "Received error when unmarshaling JSON body", err)
			return
		}

		msgs, err = c.Jsmp.ProcessJSONMessage(msg, routingKey)
	} else {
		c.reportError(string(body), "Unknown content-type", errors.New(contentType))
		return
	}

	for _, msg := range msgs {
		//This semantically could be a clustername , as well since the data does not make it easy to disambiguate
		// The hope is that when multiple distinct CbR's are being managed this will be more helpful than
		// defaulting all "cb_server" entries to "cbserver" as was done in the past.
		msg["cb_server"] = c.CbServerName
		t, ok := msg["type"]
		if c.PerformFeedPostprocessing && ok && strings.HasPrefix(t.(string), "feed.") {
			go func(msg map[string]interface{}) {
				outputMsg := c.Jsmp.PostprocessJSONMessage(msg)
				c.OutputMessageFunc(outputMsg)
			}(msg)
		} else {
			err = c.OutputMessageFunc(msg)
			if err != nil {
				c.reportError(string(body), "Error marshaling message for output: ", err)
			}
		}
	}
}
