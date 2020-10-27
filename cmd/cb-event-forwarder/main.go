package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"expvar"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	graphite "github.com/cyberdelia/go-metrics-graphite"
	"github.com/facebookgo/pidfile"
	"github.com/rcrowley/go-metrics"
	"github.com/rcrowley/go-metrics/exp"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"

	_ "net/http/pprof"
)

var (
	pidFileLocation    = flag.String("pid-file", "", "PID file location")
	checkConfiguration = flag.Bool("check", false, "Check the configuration file and exit")
	debug              = flag.Bool("debug", false, "Enable debugging mode")
)

var version = "3.7.0"

var inputChannelSize = 100000
var outputChannelSize = 1000000

var wg sync.WaitGroup
var config Configuration

type Status struct {
	InputEventCount  metrics.Meter
	InputByteCount   metrics.Meter
	OutputEventCount metrics.Meter
	OutputByteCount  metrics.Meter
	ErrorCount       metrics.Meter

	InputChannelCount  metrics.Gauge
	OutputChannelCount metrics.Gauge

	IsConnected     bool
	LastConnectTime time.Time
	StartTime       time.Time

	LastConnectError string
	ErrorTime        time.Time
	Healthy          metrics.Healthcheck

	sync.RWMutex
}

var status Status

var (
	results       chan string
	outputErrors  chan error
	outputSignals chan os.Signal
)

func init() {
	flag.Parse()
}

func setupMetrics() {
	status.InputEventCount = metrics.NewRegisteredMeter("core.input.events", metrics.DefaultRegistry)
	status.InputByteCount = metrics.NewRegisteredMeter("core.input.data", metrics.DefaultRegistry)
	status.OutputEventCount = metrics.NewRegisteredMeter("core.output.events", metrics.DefaultRegistry)
	status.OutputByteCount = metrics.NewRegisteredMeter("core.output.data", metrics.DefaultRegistry)
	status.ErrorCount = metrics.NewRegisteredMeter("errors", metrics.DefaultRegistry)
	status.InputChannelCount = metrics.NewRegisteredGauge("core.channel.input.events", metrics.DefaultRegistry)
	status.OutputChannelCount = metrics.NewRegisteredGauge("core.channel.output.events", metrics.DefaultRegistry)

	status.Healthy = metrics.NewHealthcheck(func(h metrics.Healthcheck) {
		if status.IsConnected {
			h.Healthy()
		} else {
			h.Unhealthy(errors.New("Event Forwarder is not connected"))
		}
	})

	metrics.RegisterRuntimeMemStats(metrics.DefaultRegistry)

	metrics.Register("connection_status",
		expvar.Func(func() interface{} {
			res := make(map[string]interface{}, 0)
			res["last_connect_time"] = status.LastConnectTime
			res["last_error_text"] = status.LastConnectError
			res["last_error_time"] = status.ErrorTime
			if status.IsConnected {
				res["connected"] = true
				res["uptime"] = time.Now().Sub(status.LastConnectTime).Seconds()
			} else {
				res["connected"] = false
				res["uptime"] = 0.0
			}

			return res
		}))
	metrics.Register("uptime", expvar.Func(func() interface{} {
		return time.Now().Sub(status.StartTime).Seconds()
	}))
	metrics.Register("subscribed_events", expvar.Func(func() interface{} {
		return config.EventTypes
	}))

	results = make(chan string, outputChannelSize)
	outputErrors = make(chan error)
	outputSignals = make(chan os.Signal)

	status.StartTime = time.Now()
}

/*
 * Types
 */

type OutputHandler interface {
	Initialize(string) error
	Go(messages <-chan string, errorChan chan<- error, signalChan chan<- os.Signal) error
	String() string
	Statistics() interface{}
	Key() string
}

/*
 * worker
 */

// TODO: change this into an error channel
func reportError(d string, errmsg string, err error) {
	status.ErrorCount.Mark(1)
	log.Debugf("%s when processing %s: %s", errmsg, d, err)
}

func reportBundleDetails(routingKey string, body []byte, headers amqp.Table) {
	log.Errorf("Error while processing message through routing key %s:", routingKey)

	var env *CbEnvironmentMsg
	env, err := createEnvMessage(headers)
	if err != nil {
		log.Errorf("  Message was received from sensor %d; hostname %s", env.Endpoint.GetSensorId(),
			env.Endpoint.GetSensorHostName())
	}

	if len(body) < 4 {
		log.Info("  Message is less than 4 bytes long; malformed")
	} else {
		log.Info("  First four bytes of message were:")
		log.Errorf("  %s", hex.Dump(body[0:4]))
	}

	/*
	 * We are going to store this bundle in the DebugStore
	 */
	if config.DebugFlag {
		h := md5.New()
		h.Write(body)
		var fullFilePath string
		fullFilePath = path.Join(config.DebugStore, fmt.Sprintf("/event-forwarder-%X", h.Sum(nil)))
		log.Debugf("Writing Bundle to disk: %s", fullFilePath)
		ioutil.WriteFile(fullFilePath, body, 0444)
	}
}

func monitorChannels(ticker *time.Ticker, inputChannel chan<- amqp.Delivery, outputChannel chan<- string) {
	for range ticker.C {
		status.InputChannelCount.Update(int64(len(inputChannel)))
		status.OutputChannelCount.Update(int64(len(outputChannel)))
	}
}

func processMessage(body []byte, routingKey, contentType string, headers amqp.Table, exchangeName string) {
	status.InputEventCount.Mark(1)
	status.InputByteCount.Mark(int64(len(body)))
	var err error
	var msgs []map[string]interface{}

	//
	// Process message based on ContentType
	//
	//log.Errorf("PROCESS MESSAGE CALLED ROUTINGKEY = %s contentType = %s exchange = %s ",routingKey, contentType, exchangeName)
	if contentType == "application/zip" {
		msgs, err = ProcessRawZipBundle(routingKey, body, headers)
		if err != nil {
			reportBundleDetails(routingKey, body, headers)
			reportError(routingKey, "Could not process raw zip bundle", err)
			return
		}
	} else if contentType == "application/protobuf" {
		// if we receive a protobuf through the raw sensor exchange, it's actually a protobuf "bundle" and not a
		// single protobuf
		if exchangeName == "api.rawsensordata" {
			msgs, err = ProcessProtobufBundle(routingKey, body, headers)
			//log.Infof("Process Protobuf bundle returned %d messages and error = %v",len(msgs),err)
		} else {
			msg, err := ProcessProtobufMessage(routingKey, body, headers)
			if err != nil {
				reportBundleDetails(routingKey, body, headers)
				reportError(routingKey, "Could not process body", err)
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
			reportError(string(body), "Received error when unmarshaling JSON body", err)
			return
		}

		msgs, err = ProcessJSONMessage(msg, routingKey)
	} else {
		reportError(string(body), "Unknown content-type", errors.New(contentType))
		return
	}

	for _, msg := range msgs {
		err = outputMessage(msg)
		if err != nil {
			reportError(string(body), "Error marshaling message", err)
		}
	}
}

func outputMessage(msg map[string]interface{}) error {
	var err error

	//
	// Marshal result into the correct output format
	//
	msg["cb_server"] = config.ServerName

	// Remove keys that have been configured to be removed
	for _, v := range config.RemoveFromOutput {
		delete(msg, v)
	}

	var outmsg string

	switch config.OutputFormat {
	case JSONOutputFormat:
		var b []byte
		b, err = json.Marshal(msg)
		outmsg = string(b)
	case LEEFOutputFormat:
		outmsg, err = Encode(msg)
	default:
		panic("Impossible: invalid output_format, exiting immediately")
	}

	if len(outmsg) > 0 && err == nil {
		status.OutputEventCount.Mark(1)
		status.OutputByteCount.Mark(int64(len(outmsg)))
		results <- string(outmsg)
	} else {
		return err
	}

	return nil
}

func splitDelivery(deliveries <-chan amqp.Delivery, messages chan<- amqp.Delivery) {
	defer close(messages)
	for delivery := range deliveries {
		messages <- delivery
	}
}

func worker(deliveries <-chan amqp.Delivery) {
	defer wg.Done()

	for delivery := range deliveries {
		processMessage(delivery.Body,
			delivery.RoutingKey,
			delivery.ContentType,
			delivery.Headers,
			delivery.Exchange)
	}

	log.Info("Worker exiting")
}

func logFileProcessingLoop() <-chan error {

	errChan := make(chan error)

	spawnTailer := func(fName string, label string) {

		log.Debugf("Spawn tailer: %s", fName)

		_, deliveries, err := NewFileConsumer(fName)

		if err != nil {
			status.LastConnectError = err.Error()
			status.ErrorTime = time.Now()
			errChan <- err
		}

		for delivery := range deliveries {
			log.Debugf("Trying to deliver log message %s", delivery)
			msgMap := make(map[string]interface{})
			msgMap["message"] = strings.TrimSuffix(delivery, "\n")
			msgMap["type"] = label
			outputMessage(msgMap)
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

func messageProcessingLoop(uri, queueName, consumerTag string) error {

	var dialer AMQPDialer

	if config.CannedInput {
		md := NewMockAMQPDialer()
		mockChan, _ := md.Connection.Channel()
		go RunCannedData(mockChan)
		dialer = md
	} else {
		dialer = StreadwayAMQPDialer{}
	}

	var c *Consumer = NewConsumer(uri, queueName, consumerTag, config.UseRawSensorExchange, config.EventTypes, dialer)

	messages := make(chan amqp.Delivery, inputChannelSize)

	deliveries, err := c.Connect()

	if err != nil {
		status.LastConnectError = err.Error()
		status.ErrorTime = time.Now()
		return err
	}

	status.LastConnectTime = time.Now()
	status.IsConnected = true

	numProcessors := config.NumProcessors
	log.Infof("Starting %d message processors\n", numProcessors)

	wg.Add(numProcessors)

	if config.RunMetrics {
		go monitorChannels(time.NewTicker(100*time.Millisecond), messages, results)
	}
	go splitDelivery(deliveries, messages)
	for i := 0; i < numProcessors; i++ {
		go worker(messages)
	}

	for {
		select {
		case outputError := <-outputErrors:
			log.Debugf("ERROR during output: %s", outputError.Error())
		case signal := <-outputSignals:
			log.Errorf("%s- exiting immediately.", signal)
			c.Shutdown()
			wg.Wait()
			os.Exit(1)

		case closeError := <-c.connectionErrors:
			status.IsConnected = false
			status.LastConnectError = closeError.Error()
			status.ErrorTime = time.Now()

			log.Errorf("Connection error: %s", closeError.Error())
			// This assumes that after the error, workers don't get any more messagesa nd will eventually return
			log.Info("Waiting for all workers to exit")
			wg.Wait()
			log.Info("All workers have exited")

			return closeError
		}
	}
	log.Info("Loop exited for unknown reason")
	c.Shutdown()
	wg.Wait()

	return nil
}

func startOutputs() error {
	// Configure the specific output.
	// Valid options are: 'udp', 'tcp', 'file', 's3', 'syslog' ,"http",'splunk'
	var outputHandler OutputHandler

	parameters := config.OutputParameters

	switch config.OutputType {
	case FileOutputType:
		outputHandler = &FileOutput{}
	case TCPOutputType:
		outputHandler = &NetOutput{}
		parameters = "tcp:" + parameters
	case UDPOutputType:
		outputHandler = &NetOutput{}
		parameters = "udp:" + parameters
	case S3OutputType:
		outputHandler = &BundledOutput{behavior: &S3Behavior{}}
	case SyslogOutputType:
		outputHandler = &SyslogOutput{}
	case HTTPOutputType:
		outputHandler = &BundledOutput{behavior: &HTTPBehavior{}}
	case SplunkOutputType:
		outputHandler = &BundledOutput{behavior: &SplunkBehavior{}}
	case KafkaOutputType:
		outputHandler = &KafkaOutput{}
	default:
		return fmt.Errorf("No valid output handler found (%d)", config.OutputType)
	}

	err := outputHandler.Initialize(parameters)
	if err != nil {
		return err
	}

	metrics.Register("output_status", expvar.Func(func() interface{} {
		ret := make(map[string]interface{})
		ret[outputHandler.Key()] = outputHandler.Statistics()

		switch config.OutputFormat {
		case LEEFOutputFormat:
			ret["format"] = "leef"
		case JSONOutputFormat:
			ret["format"] = "json"
		}

		switch config.OutputType {
		case FileOutputType:
			ret["type"] = "file"
		case UDPOutputType:
			ret["type"] = "net"
		case TCPOutputType:
			ret["type"] = "net"
		case S3OutputType:
			ret["type"] = "s3"
		case HTTPOutputType:
			ret["type"] = "http"
		case SplunkOutputType:
			ret["type"] = "splunk"
		}

		return ret
	}))

	log.Infof("Initialized output: %s\n", outputHandler.String())
	return outputHandler.Go(results, outputErrors, outputSignals)
}

func main() {
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}

	configLocation := "/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf"
	if flag.NArg() > 0 {
		configLocation = flag.Arg(0)
	}
	log.Infof("Using config file %s\n", configLocation)
	config, err = ParseConfig(configLocation)
	if err != nil {
		log.Fatal(err)
	}

	if !config.RunMetrics {
		log.Infof("Running without metrics")
		metrics.UseNilMetrics = true
	} else {
		metrics.UseNilMetrics = false
		log.Infof("Running with metrics")
	}

	setupMetrics()

	if *checkConfiguration {
		if err := startOutputs(); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}

	defaultPidFileLocation := "/run/cb/integrations/cb-event-forwarder/cb-event-forwarder.pid"
	if *pidFileLocation == "" {
		*pidFileLocation = defaultPidFileLocation
	}
	log.Infof("PID file will be written to %s\n", *pidFileLocation)
	pidfile.SetPidfilePath(*pidFileLocation)
	err = pidfile.Write()
	if err != nil {
		log.Warn("Could not write PID file: %s\n", err)
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Fatal("Could not get IP addresses")
	}

	log.Infof("cb-event-forwarder version %s starting", version)

	exportedVersion := &expvar.String{}
	metrics.Register("version", exportedVersion)
	if *debug {
		exportedVersion.Set(version + " (debugging on)")
		log.Debugf("*** Debugging enabled: messages may be sent via http://%s:%d/debug/sendmessage ***",
			hostname, config.HTTPServerPort)
		log.SetLevel(log.DebugLevel)

	} else {
		exportedVersion.Set(version)
	}

	metrics.Register("debug", expvar.Func(func() interface{} { return *debug }))

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			log.Infof("Interface address %s", ipnet.IP.String())
		}
	}

	log.Infof("Configured to capture events: %v", config.EventTypes)
	if err := startOutputs(); err != nil {
		log.Fatalf("Could not startOutputs: %s", err)
	}

	if *debug {
		http.HandleFunc("/debug/sendmessage", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				msg := make([]byte, r.ContentLength)
				_, err := r.Body.Read(msg)
				var parsedMsg map[string]interface{}

				err = json.Unmarshal(msg, &parsedMsg)
				if err != nil {
					errMsg, _ := json.Marshal(map[string]string{"status": "error", "error": err.Error()})
					_, _ = w.Write(errMsg)
					return
				}

				err = outputMessage(parsedMsg)
				if err != nil {
					errMsg, _ := json.Marshal(map[string]string{"status": "error", "error": err.Error()})
					_, _ = w.Write(errMsg)
					return
				}
				log.Errorf("Sent test message: %s\n", string(msg))
			} else {
				err = outputMessage(map[string]interface{}{
					"type":    "debug.message",
					"message": fmt.Sprintf("Debugging test message sent at %s", time.Now().String()),
				})
				if err != nil {
					errMsg, _ := json.Marshal(map[string]string{"status": "error", "error": err.Error()})
					_, _ = w.Write(errMsg)
					return
				}
				log.Info("Sent test debugging message")
			}

			errMsg, _ := json.Marshal(map[string]string{"status": "success"})
			_, _ = w.Write(errMsg)
		})
	}

	http.HandleFunc("/debug/healthcheck", func(w http.ResponseWriter, r *http.Request) {
		if !status.IsConnected {
			payload, _ := json.Marshal(map[string]interface{}{"status": "FORWARDER IS NOT CONNECTED", "error": status.LastConnectError})
			http.Error(w, string(payload), http.StatusNetworkAuthenticationRequired)
		} else {
			payload, _ := json.Marshal(map[string]interface{}{"status": "FORWARDER IS CONNECTED"})
			w.Write(payload)
		}
	})

	go http.ListenAndServe(fmt.Sprintf(":%d", config.HTTPServerPort), nil)

	queueName := fmt.Sprintf("cb-event-forwarder:%s:%d", hostname, os.Getpid())

	if config.AMQPQueueName != "" {
		queueName = config.AMQPQueueName
	}

	if config.RunConsumer {
		go func(consumerNumber int) {
			log.Infof("Starting AMQP loop %d to %s on queue %s", consumerNumber, config.AMQPURL(), queueName)
			for {
				err := messageProcessingLoop(config.AMQPURL(), queueName, fmt.Sprintf("go-event-consumer-%d", consumerNumber))
				log.Infof("AMQP loop %d exited: %s. Sleeping for 30 seconds then retrying.", consumerNumber, err)
				time.Sleep(30 * time.Second)
			}
		}(1)
	}

	if config.AuditLog == true {
		log.Info("starting log file processing loop")
		go func() {
			errChan := logFileProcessingLoop()
			for {
				select {
				case err := <-errChan:
					log.Infof("%v", err)
				}
			}
		}()

	} else {
		log.Info("Not starting file processing loop")
	}

	//Try to send to metrics to carbon if configured to do so
	if config.CarbonMetricsEndpoint != nil && config.RunMetrics {
		//log.Infof("Trying to resolve TCP ADDR for %s\n", *config.CarbonMetricsEndpoint)
		addr, err := net.ResolveTCPAddr("tcp4", *config.CarbonMetricsEndpoint)
		if err != nil {
			log.Panicf("Failing resolving carbon endpoint %v", err)
		}

		go graphite.Graphite(metrics.DefaultRegistry, 1*time.Second, config.MetricTag, addr)
		log.Infof("Sending metrics to graphite")
	}

	exp.Exp(metrics.DefaultRegistry)

	for {
		time.Sleep(30 * time.Second)
	}

	log.Info("cb-event-forwarder exiting")
}
