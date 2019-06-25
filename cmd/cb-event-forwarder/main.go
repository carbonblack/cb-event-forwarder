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
	"github.com/carbonblack/cb-event-forwarder/internal/leef"
	"github.com/carbonblack/cb-event-forwarder/internal/sensor_events"
	"github.com/rcrowley/go-metrics"
	"github.com/rcrowley/go-metrics/exp"
	"github.com/cyberdelia/go-metrics-graphite"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

//"github.com/rcrowley/go-metrics"
//	"github.com/rcrowley/go-metrics/exp"
import _ "net/http/pprof"

var (
	checkConfiguration = flag.Bool("check", false, "Check the configuration file and exit")
	debug              = flag.Bool("debug", false, "Enable debugging mode")
)

var version = "NOT FOR RELEASE"

var wg sync.WaitGroup
var config Configuration

type Status struct {
	InputEventCount  metrics.Gauge
	InputByteCount   metrics.Gauge
	OutputEventCount metrics.Gauge
	OutputByteCount  metrics.Gauge
	ErrorCount       metrics.GaugeFloat64

	IsConnected     bool
	LastConnectTime time.Time
	StartTime       time.Time

	LastConnectError string
	ErrorTime        time.Time

	sync.RWMutex
}

var status Status

var (
	results          chan string
	outputErrors     chan error
)

/*
 * Initializations
 *

type bufwriter chan []byte

func (bw bufwriter) Write(p []byte) (int, error) {
    bw <- p
    return len(p), nil
}
func NewBufwriter(n int) bufwriter {
    w := make(bufwriter, n)
    go func() {
        for p := range w {
            os.Stdout.Write(p)
        }
    }()
    return w
}
*/
func init() {
	flag.Parse()
	status.InputEventCount = metrics.NewGauge()
	status.InputByteCount = metrics.NewGauge()
	status.OutputEventCount = metrics.NewGauge()
	status.OutputByteCount = metrics.NewGauge()
	status.ErrorCount = metrics.NewGaugeFloat64()
	metrics.Register("input_event_count", status.InputEventCount)
	metrics.Register("output_event_count", status.OutputEventCount)
	metrics.Register("error_count", status.ErrorCount)
	//metrics.Register("output_event_rate", status.OutputEventRate)
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

	results = make(chan string)
	outputErrors = make(chan error)

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
	status.ErrorCount.Update(1)
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
	status.InputEventCount.Update(1)
	status.InputByteCount.Update(int64(len(body)))
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
		outmsg, err = leef.Encode(msg)
	default:
		panic("Impossible: invalid output_format, exiting immediately")
	}

	if len(outmsg) > 0 && err == nil {
		status.OutputEventCount.Update(1)
		status.OutputByteCount.Update(int64(len(outmsg)))
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
	connectionError := make(chan *amqp.Error, 1)

	c, deliveries, err := NewConsumer(uri, queueName, consumerTag, config.UseRawSensorExchange, config.EventTypes)
	if err != nil {
		status.LastConnectError = err.Error()
		status.ErrorTime = time.Now()
		return err
	}

	status.LastConnectTime = time.Now()
	status.IsConnected = true

	c.conn.NotifyClose(connectionError)

	numProcessors := config.NumProcessors
	log.Infof("Starting %d message processors\n", numProcessors)

	wg.Add(numProcessors)
	for i := 0; i < numProcessors; i++ {
		go worker(deliveries)
	}

	for {
		select {
		case outputError := <-outputErrors:
			log.Errorf("ERROR during output: %s", outputError.Error())

			// hack to exit if the error happens while we are writing to a file
			if config.OutputType == FileOutputType || config.OutputType == SplunkOutputType || config.OutputType == HTTPOutputType {
				log.Error("File output error; exiting immediately.")
				c.Shutdown()
				wg.Wait()
				os.Exit(1)
			}
		case closeError := <-connectionError:
			status.IsConnected = false
			status.LastConnectError = closeError.Error()
			status.ErrorTime = time.Now()

			log.Errorf("Connection closed: %s", closeError.Error())
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
	return outputHandler.Go(results, outputErrors)
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

	queueName := fmt.Sprintf("cb-event-forwarder:%s:%d", hostname, os.Getpid())

	if config.AMQPQueueName != "" {
		queueName = config.AMQPQueueName
	}

	go func(consumerNumber int) {
		log.Infof("Starting AMQP loop %d to %s on queue %s", consumerNumber, config.AMQPURL(), queueName)
		for {
			err := messageProcessingLoop(config.AMQPURL(), queueName, fmt.Sprintf("go-event-consumer-%d", consumerNumber))
			log.Infof("AMQP loop %d exited: %s. Sleeping for 30 seconds then retrying.", consumerNumber, err)
			time.Sleep(30 * time.Second)
		}
	}(1)

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
	if config.CarbonMetricsEndpoint != nil {
		log.Infof("Trying to resolve TCP ADDR for %s\n",*config.CarbonMetricsEndpoint)
		addr, err := net.ResolveTCPAddr("tcp4", *config.CarbonMetricsEndpoint)
		if err != nil {
			log.Panicf("Failing resolving carbon endpoint %v", err)
		}
		go graphite.Graphite(metrics.DefaultRegistry, 1*time.Second, "cb.eventforwarder", addr)
	}

	exp.Exp(metrics.DefaultRegistry)

	for {
		time.Sleep(30 * time.Second)
		//status.OutputEventRate.Set(float64(status.OutputEventCount.Value()) / float64(time.Now().Sub(status.StartTime)))
	}

	log.Info("cb-event-forwarder exiting")
}
