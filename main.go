package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"expvar"
	"flag"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/leef"
	"github.com/streadway/amqp"
	"log"
	"net"
	"net/http"
	//	_ "net/http/pprof"          // DEBUG: profiling support
	"os"
	"runtime"
	"sync"
	"time"
	"github.com/paulbellamy/ratecounter"
)

var (
	checkConfiguration = flag.Bool("check", false, "Check the configuration file and exit")
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

	EventCounter *ratecounter.RateCounter
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
	exportedVersion := expvar.NewString("version")
	exportedVersion.Set(version)

	status.EventCounter = ratecounter.NewRateCounter(5 * time.Second)
	expvar.Publish("events_per_second",
		expvar.Func(func() interface{} { return float64(status.EventCounter.Rate()) / 5.0 }))

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
	log.Printf("%s when processing %s: %s", errmsg, d, err)
}

func processMessage(body []byte, routingKey, contentType string) {
	status.InputEventCount.Add(1)
	status.EventCounter.Incr(1)

	msgs := make([]map[string]interface{}, 0, 1)
	var err error

	//
	// Process message based on ContentType
	//
	if contentType == "application/protobuf" {
		msg, err := ProcessProtobufMessage(routingKey, body)
		if err != nil {
			reportError(routingKey, "Could not process body", err)
			return
		}

		msgs = append(msgs, msg)
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
		reportError(string(body), "Unknown content-type:", errors.New(contentType))
		return
	}

	//
	// Marshal result into the correct output format
	//
	for _, msg := range msgs {
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
			reportError(string(body), "Error marshaling message", err)
			return
		}
	}
}

func worker(deliveries <-chan amqp.Delivery) {
	defer wg.Done()

	for delivery := range deliveries {
		processMessage(delivery.Body, delivery.RoutingKey, delivery.ContentType)
	}

	log.Printf("Worker exiting")
}

func messageProcessingLoop(uri, queueName, consumerTag string) error {
	connection_error := make(chan *amqp.Error, 1)

	c, deliveries, err := NewConsumer(uri, queueName, consumerTag, config.EventTypes)
	if err != nil {
		status.LastConnectError = err.Error()
		status.ErrorTime = time.Now()
		return err
	}

	status.LastConnectTime = time.Now()
	status.IsConnected = true

	c.conn.NotifyClose(connection_error)

	numProcessors := runtime.NumCPU() * 2
	log.Printf("Starting %d message processors\n", numProcessors)

	wg.Add(numProcessors)
	for i := 0; i < numProcessors; i++ {
		go worker(deliveries)
	}

	for {
		select {
		case output_error := <-output_errors:
			log.Printf("ERROR during output: %s. Exiting the program", output_error.Error())

			// TODO what should we do in this case? Right now we'll exit the program entirely
			c.Shutdown()
			wg.Wait()
			os.Exit(1)
		case close_error := <-connection_error:
			status.IsConnected = false
			status.LastConnectError = close_error.Error()
			status.ErrorTime = time.Now()

			log.Printf("Connection closed: %s", close_error.Error())
			log.Println("Waiting for all workers to exit")
			wg.Wait()
			log.Println("All workers have exited")

			return close_error
		}
	}
	log.Println("Loop exited for unknown reason")
	c.Shutdown()
	wg.Wait()

	return nil
}

func startOutputs() error {
	// Configure the specific output.
	// Valid options are: 'udp', 'tcp', 'file', 's3'
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
		outputHandler = &S3Output{}
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
		case TCPOutputType:
			ret["type"] = "net"
		case S3OutputType:
			ret["type"] = "s3"
		}

		return ret
	}))

	log.Printf("Initialized output: %s\n", outputHandler.String())
	return outputHandler.Go(results, output_errors)
}

func main() {
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}

	queueName := fmt.Sprintf("cb-event-forwarder:%s:%d", hostname, os.Getpid())

	configLocation := "/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf"
	if flag.NArg() > 0 {
		configLocation = flag.Arg(0)
	}
	config, err = ParseConfig(configLocation)
	if err != nil {
		log.Fatal(err)
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

	log.Printf("cb-event-forwarder version %s starting", version)

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			log.Printf("Interface address %s", ipnet.IP.String())
		}
	}

	log.Printf("Configured to capture events: %v", config.EventTypes)
	if err := startOutputs(); err != nil {
		log.Fatalf("Could not startOutputs: %s", err)
	}

	log.Printf("Diagnostics available via HTTP at http://%s:%d/debug/vars", hostname, config.HTTPServerPort)

	// TODO: disabling /static until we have a better index page
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
	go http.ListenAndServe(fmt.Sprintf(":%d", config.HTTPServerPort), nil)

	log.Println("Starting AMQP loop")
	for {
		err := messageProcessingLoop(config.AMQPURL(), queueName, "go-event-consumer")
		log.Printf("AMQP loop exited: %s. Sleeping for 30 seconds then retrying.", err)
		time.Sleep(30 * time.Second)
	}
}
