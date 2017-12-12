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
	"github.com/carbonblack/cb-event-forwarder/leef"
	"github.com/carbonblack/cb-event-forwarder/sensor_events"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"
)

import _ "net/http/pprof"

var (
	checkConfiguration = flag.Bool("check", false, "Check the configuration file and exit")
	debug              = flag.Bool("debug", false, "Enable debugging mode")
)

var version = "NOT FOR RELEASE"

var wg sync.WaitGroup
var config Configuration

type Status struct {
	InputEventCount  *expvar.Int
	OutputEventCount *expvar.Int
	ErrorCount       *expvar.Int

	IsConnected     bool
	LastConnectTime time.Time
	StartTime       time.Time

	LastConnectError string
	ErrorTime        time.Time

	sync.RWMutex
}

var status Status

var (
	results       chan string
	output_errors chan error
)

/*
 * Initializations
 */
func init() {
	flag.Parse()
	status.InputEventCount = expvar.NewInt("input_event_count")
	status.OutputEventCount = expvar.NewInt("output_event_count")
	status.ErrorCount = expvar.NewInt("error_count")

	expvar.Publish("connection_status",
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
	expvar.Publish("uptime", expvar.Func(func() interface{} {
		return time.Now().Sub(status.StartTime).Seconds()
	}))
	expvar.Publish("subscribed_events", expvar.Func(func() interface{} {
		return config.EventTypes
	}))

	results = make(chan string, 100)
	output_errors = make(chan error)

	status.StartTime = time.Now()
}

/*
 * Types
 */
type Consumer struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	tag     string
}

type OutputHandler interface {
	Initialize(string) error
	Go(messages <-chan string, errorChan chan<- error) error
	String() string
	Statistics() interface{}
	Key() string
}

/*
 * worker
 */

// TODO: change this into an error channel
func reportError(d string, errmsg string, err error) {
	status.ErrorCount.Add(1)
	log.Errorf("%s when processing %s: %s", errmsg, d, err)
}

func reportBundleDetails(routingKey string, body []byte, headers amqp.Table) {
	log.Errorf("Error while processing message through routing key %s:", routingKey)

	var env *sensor_events.CbEnvironmentMsg
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

func processMessage(body []byte, routingKey, contentType string, headers amqp.Table, exchangeName string) {
	status.InputEventCount.Add(1)

	var err error
	var msgs []map[string]interface{}

	//
	// Process message based on ContentType
	//
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
		if config.PerformFeedPostprocessing {
			go func(msg map[string]interface{}) {
				outputMsg := PostprocessJSONMessage(msg)
				outputMessage(outputMsg)
			}(msg)
		} else {
			err = outputMessage(msg)
			if err != nil {
				reportError(string(body), "Error marshaling message", err)
			}
		}
	}
}

func outputMessage(msg map[string]interface{}) error {
	var err error

	//
	// Marshal result into the correct output format
	//
	msg["cb_server"] = config.ServerName

	var outmsg string

	switch config.OutputFormat {
	case JSONOutputFormat:
		var b []byte
		b, err = json.Marshal(msg)
		outmsg = string(b)
	case LEEFOutputFormat:
		outmsg, err = leef.Encode(msg)
	default:
		panic("Impossible: invalid output_format, exiting immediately")
	}

	if len(outmsg) > 0 && err == nil {
		status.OutputEventCount.Add(1)
		results <- string(outmsg)
	} else {
		return err
	}

	return nil
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

	err_chan := make(chan error)

	spawn_tailer := func(fName string, label string) {

		log.Debugf("Spawn tailer: %s", fName)

		_, deliveries, err := NewFileConsumer(fName)

		if err != nil {
			status.LastConnectError = err.Error()
			status.ErrorTime = time.Now()
			err_chan <- err
		}


		for delivery := range deliveries {
			log.Debug("Trying to deliver log message %s", delivery)
			msg_map := make(map[string]interface{})
			msg_map["message"] = strings.TrimSuffix(delivery, "\n")
			msg_map["type"] = label
			outputMessage(msg_map)
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
	go spawn_tailer("/var/log/cb/audit/live-response.log","audit.log.liveresponse")
	go spawn_tailer("/var/log/cb/audit/banning.log","audit.log.banning")
	go spawn_tailer("/var/log/cb/audit/isolation.log","audit.log.isolation")
	go spawn_tailer("/var/log/cb/audit/useractivity.log","audit.log.useractivity")
	return err_chan
}

func messageProcessingLoop(uri, queueName, consumerTag string) error {
	connection_error := make(chan *amqp.Error, 1)

	c, deliveries, err := NewConsumer(uri, queueName, consumerTag, config.UseRawSensorExchange, config.EventTypes)
	if err != nil {
		status.LastConnectError = err.Error()
		status.ErrorTime = time.Now()
		return err
	}

	status.LastConnectTime = time.Now()
	status.IsConnected = true

	c.conn.NotifyClose(connection_error)

	numProcessors := runtime.NumCPU() * 2
	log.Infof("Starting %d message processors\n", numProcessors)

	wg.Add(numProcessors)
	for i := 0; i < numProcessors; i++ {
		go worker(deliveries)
	}

	for {
		select {
		case output_error := <-output_errors:
			log.Errorf("ERROR during output: %s", output_error.Error())

			// hack to exit if the error happens while we are writing to a file
			if config.OutputType == FileOutputType || config.OutputType == SplunkOutputType || config.OutputType == HttpOutputType {
				log.Error("File output error; exiting immediately.")
				c.Shutdown()
				wg.Wait()
				os.Exit(1)
			}
		case close_error := <-connection_error:
			status.IsConnected = false
			status.LastConnectError = close_error.Error()
			status.ErrorTime = time.Now()

			log.Errorf("Connection closed: %s", close_error.Error())
			log.Info("Waiting for all workers to exit")
			wg.Wait()
			log.Info("All workers have exited")

			return close_error
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
	case HttpOutputType:
		outputHandler = &BundledOutput{behavior: &HttpBehavior{}}
	case SplunkOutputType:
		outputHandler = &BundledOutput{behavior: &SplunkBehavior{}}
	case KafkaOutputType:
		outputHandler = &KafkaOutput{}
	default:
		return errors.New(fmt.Sprintf("No valid output handler found (%d)", config.OutputType))
	}

	err := outputHandler.Initialize(parameters)
	if err != nil {
		return err
	}

	expvar.Publish("output_status", expvar.Func(func() interface{} {
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
		case HttpOutputType:
			ret["type"] = "http"
		case SplunkOutputType:
			ret["type"] = "splunk"
		}

		return ret
	}))

	log.Infof("Initialized output: %s\n", outputHandler.String())
	return outputHandler.Go(results, output_errors)
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
	config, err = ParseConfig(configLocation)
	if err != nil {
		log.Fatal(err)
	}

	if config.PerformFeedPostprocessing {
		apiVersion, err := GetCbVersion()
		if err != nil {
			log.Fatal("Could not get cb version: " + err.Error())
		} else {
			log.Infof("Enabling feed post-processing for server %s version %s.", config.CbServerURL, apiVersion)
		}
	}

	if *checkConfiguration {
		if err := startOutputs(); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}

	addrs, err := net.InterfaceAddrs()

	if err != nil {
		log.Fatal("Could not get IP addresses")
	}

	log.Infof("cb-event-forwarder version %s starting", version)

	exportedVersion := expvar.NewString("version")
	if *debug {
		exportedVersion.Set(version + " (debugging on)")
		log.Debugf("*** Debugging enabled: messages may be sent via http://%s:%d/debug/sendmessage ***",
			hostname, config.HTTPServerPort)
	} else {
		exportedVersion.Set(version)
	}
	expvar.Publish("debug", expvar.Func(func() interface{} { return *debug }))

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			log.Infof("Interface address %s", ipnet.IP.String())
		}
	}

	log.Infof("Configured to capture events: %v", config.EventTypes)
	if err := startOutputs(); err != nil {
		log.Fatalf("Could not startOutputs: %s", err)
	}

	dirs := [...]string{
		"/usr/share/cb/integrations/event-forwarder/content",
		"./static",
	}

	for _, dirname := range dirs {
		finfo, err := os.Stat(dirname)
		if err == nil && finfo.IsDir() {
			http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(dirname))))
			http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/static/", 301)
			})
			log.Infof("Diagnostics available via HTTP at http://%s:%d/", hostname, config.HTTPServerPort)
			break
		}
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

	go http.ListenAndServe(fmt.Sprintf(":%d", config.HTTPServerPort), nil)

	numConsumers := 1
	if runtime.NumCPU() > 1 && config.OutputType == KafkaOutputType {
		numConsumers = runtime.NumCPU() / 2
	}

	queueName := fmt.Sprintf("cb-event-forwarder:%s:%d", hostname, os.Getpid())

	if config.AMQPQueueName != "" {
		queueName = config.AMQPQueueName
	}

	for i := 0; i < numConsumers; i++ {
		go func(consumerNumber int) {
			log.Infof("Starting AMQP loop %d to %s on queue %s", consumerNumber, config.AMQPURL(), queueName)
			for {
				err := messageProcessingLoop(config.AMQPURL(), queueName, fmt.Sprintf("go-event-consumer-%d", consumerNumber))
				log.Infof("AMQP loop %d exited: %s. Sleeping for 30 seconds then retrying.", consumerNumber, err)
				time.Sleep(30 * time.Second)
			}
		}(i)
	}

	if config.AuditLog == true {
		log.Info("starting log file processing loop")
		go func() {
			err_chan := logFileProcessingLoop()
			for {
				select {
				case err := <-err_chan:
					log.Infof("%v", err)
				}
			}
		}()

	} else {
		log.Info("Not starting file processing loop")
	}

	for {
		time.Sleep(30 * time.Second)
	}

	log.Info("cb-event-forwarder exiting")
}
