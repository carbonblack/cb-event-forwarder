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
	"github.com/carbonblack/cb-event-forwarder/internal/cbapi"
	"github.com/carbonblack/cb-event-forwarder/internal/cef"
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"github.com/carbonblack/cb-event-forwarder/internal/filter"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/leef"
	"github.com/carbonblack/cb-event-forwarder/internal/output"
	"github.com/carbonblack/cb-event-forwarder/internal/pbmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/sensor_events"
	te "github.com/carbonblack/cb-event-forwarder/internal/template_encoder"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"plugin"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

import _ "net/http/pprof"

var (
	checkConfiguration = flag.Bool("check", false, "Check the configuration file and exit")
	debug              = flag.Bool("debug", false, "Enable debugging mode")
	inputFile          = flag.String("inputfile", "", "Enter json file to read for input")
)

var version = "NOT FOR RELEASE"

type Status struct {
	InputEventCount    *expvar.Int
	OutputEventCount   *expvar.Int
	FilteredEventCount *expvar.Int
	ErrorCount         *expvar.Int
	IsConnected        bool
	LastConnectTime    time.Time
	StartTime          time.Time

	LastConnectError string
	ErrorTime        time.Time
	sync.RWMutex
}

/*
 * Initializations
 */
func init() {
	flag.Parse()
}

/*
 * Types
 */
type Consumer struct {
	conn     *amqp.Connection
	channel  *amqp.Channel
	tag      string
	stopchan chan struct{}
}

type CbEventForwarder struct {
	config       conf.Configuration
	jsmp         jsonmessageprocessor.JsonMessageProcessor
	pbmp         pbmessageprocessor.PbMessageProcessor
	cbapi        cbapi.CbAPIHandler
	status       Status
	name         string
	numConsumers int
	outputErrors chan error
	results      chan string
	wg           *sync.WaitGroup
	Consumers    []*Consumer
	controlchan  chan os.Signal
}

/*
 * worker
 */

// TODO: change this into an error channel
func (cbef *CbEventForwarder) reportError(d string, errmsg string, err error) {
	cbef.status.ErrorCount.Add(1)
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
		log.Info("  Message is less than 4 bytes long; malformed")
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

func (cbef *CbEventForwarder) processMessage(body []byte, routingKey, contentType string, headers amqp.Table, exchangeName string) {
	cbef.status.InputEventCount.Add(1)

	var err error
	var msgs []map[string]interface{}

	//
	// Process message based on ContentType
	//
	if contentType == "application/zip" {
		msgs, err = cbef.pbmp.ProcessRawZipBundle(routingKey, body, headers)
		if err != nil {
			reportBundleDetails(routingKey, body, headers, cbef.config.DebugFlag, cbef.config.DebugStore)
			cbef.reportError(routingKey, "Could not process raw zip bundle", err)
			return
		}
	} else if contentType == "application/protobuf" {
		// if we receive a protobuf through the raw sensor exchange, it's actually a protobuf "bundle" and not a
		// single protobuf
		if exchangeName == "api.rawsensordata" {
			msgs, err = cbef.pbmp.ProcessProtobufBundle(routingKey, body, headers)
		} else {
			msg, err := cbef.pbmp.ProcessProtobufMessage(routingKey, body, headers)
			if err != nil {
				reportBundleDetails(routingKey, body, headers, cbef.config.DebugFlag, cbef.config.DebugStore)
				cbef.reportError(routingKey, "Could not process body", err)
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
			cbef.reportError(string(body), "Received error when unmarshaling JSON body", err)
			return
		}

		msgs, err = cbef.jsmp.ProcessJSONMessage(msg, routingKey)
	} else {
		cbef.reportError(string(body), "Unknown content-type", errors.New(contentType))
		return
	}

	for _, msg := range msgs {
		if cbef.config.PerformFeedPostprocessing {
			go func(msg map[string]interface{}) {
				outputMsg := cbef.jsmp.PostprocessJSONMessage(msg)
				cbef.outputMessage(outputMsg)
			}(msg)
		} else {
			err = cbef.outputMessage(msg)
			if err != nil {
				cbef.reportError(string(body), "Error marshaling message", err)
			}
		}
	}
}

func (cbef *CbEventForwarder) outputMessage(msg map[string]interface{}) error {
	var err error

	//
	// Marshal result into the correct output format
	//
	msg["cb_server"] = cbef.config.ServerName

	// Add key=value pairs that has been configured to be added
	for key, val := range cbef.config.AddToOutput {
		msg[key] = val
	}

	// Remove keys that have been configured to be removed
	for _, v := range cbef.config.RemoveFromOutput {
		delete(msg, v)
	}

	//Apply Event Filter if specified
	keepEvent := true
	if cbef.config.FilterEnabled && cbef.config.FilterTemplate != nil {
		keepEvent = filter.FilterWithTemplate(msg, cbef.config.FilterTemplate)
	}

	if keepEvent {
		var outmsg string

		switch cbef.config.OutputFormat {
		case conf.JSONOutputFormat:
			var b []byte
			b, err = json.Marshal(msg)
			outmsg = string(b)
		case conf.LEEFOutputFormat:
			outmsg, err = leef.Encode(msg)
		case conf.CEFOutputFormat:
			outmsg, err = cef.EncodeWithSeverity(msg, cbef.config.CefEventSeverity)
		case conf.TemplateOutputFormat:
			outmsg, err = te.EncodeWithTemplate(msg, cbef.config.EncoderTemplate)
		default:
			panic("Impossible: invalid output_format, exiting immediately")
		}

		if len(outmsg) > 0 && err == nil {
			cbef.status.OutputEventCount.Add(1)
			cbef.results <- string(outmsg)
		} else if err != nil {
			return err
		}
	} else { //EventDropped due to filter
		cbef.status.FilteredEventCount.Add(1)
	}

	return nil
}

func (cbef *CbEventForwarder) logFileProcessingLoop() <-chan error {

	errChan := make(chan error)

	spawnTailer := func(fName string, label string) {

		log.Debugf("Spawn tailer: %s", fName)

		_, deliveries, err := NewFileConsumer(fName)

		if err != nil {
			cbef.status.LastConnectError = err.Error()
			cbef.status.ErrorTime = time.Now()
			errChan <- err
		}

		for delivery := range deliveries {
			log.Debug("Trying to deliver log message %s", delivery)
			msgMap := make(map[string]interface{})
			msgMap["message"] = strings.TrimSuffix(delivery, "\n")
			msgMap["type"] = label
			cbef.outputMessage(msgMap)
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

func (cbef *CbEventForwarder) inputFileProcessingLoop(inputFile string) <-chan error {

	errChan := make(chan error)

	go func() {

		log.Debugf("Opening input file : %s", inputFile)
		_, deliveries, err := NewFileConsumer(inputFile)
		if err != nil {
			cbef.status.LastConnectError = err.Error()
			cbef.status.ErrorTime = time.Now()
			errChan <- err
		}
		for delivery := range deliveries {
			log.Debug("Trying to deliver log message %s", delivery)
			msgMap := make(map[string]interface{})
			err := json.Unmarshal([]byte(delivery), &msgMap)
			if err != nil {
				cbef.status.LastConnectError = err.Error()
				cbef.status.ErrorTime = time.Now()
				errChan <- err
			}
			cbef.outputMessage(msgMap)
		}
	}()
	return errChan
}

func (cbef *CbEventForwarder) startExpvarPublish() {
	cbef.status.InputEventCount = expvar.NewInt("input_event_count")
	cbef.status.OutputEventCount = expvar.NewInt("output_event_count")
	cbef.status.FilteredEventCount = expvar.NewInt("filtered_event_count")
	cbef.status.ErrorCount = expvar.NewInt("error_count")

	expvar.Publish(fmt.Sprintf("connection_status_%s", cbef.name),
		expvar.Func(func() interface{} {
			res := make(map[string]interface{}, 0)
			res["last_connect_time"] = cbef.status.LastConnectTime
			res["last_error_text"] = cbef.status.LastConnectError
			res["last_error_time"] = cbef.status.ErrorTime
			if cbef.status.IsConnected {
				res["connected"] = true
				res["uptime"] = time.Now().Sub(cbef.status.LastConnectTime).Seconds()
			} else {
				res["connected"] = false
				res["uptime"] = 0.0
			}

			return res
		}))
	expvar.Publish(fmt.Sprintf("uptime_%s", cbef.name), expvar.Func(func() interface{} {
		return time.Now().Sub(cbef.status.StartTime).Seconds()
	}))
	expvar.Publish(fmt.Sprintf("subscribed_events_%s", cbef.name), expvar.Func(func() interface{} {
		return cbef.config.EventTypes
	}))

	cbef.status.StartTime = time.Now()
}

func (cbef *CbEventForwarder) terminateConsumers() {
	for _, consumer := range cbef.Consumers {
		consumer.stopchan <- struct{}{}
	}
}

func (cbef *CbEventForwarder) launchConsumers(queueName string) {
	cbef.startExpvarPublish()
	for i := 0; i < cbef.numConsumers; i++ {
		go func(consumerNumber int) {
			log.Infof("Starting AMQP loop %d to %s on queue %s", consumerNumber, cbef.config.AMQPURL(), queueName)
			for {
				connectionError := make(chan *amqp.Error, 1)
				consumerTag := fmt.Sprintf("go-event-consumer-%d", consumerNumber)
				c, deliveries, err := NewConsumer(&cbef.config, cbef.config.AMQPURL(), queueName, consumerTag, cbef.config.UseRawSensorExchange, cbef.config.EventTypes)
				if err != nil {
					cbef.status.LastConnectError = err.Error()
					cbef.status.ErrorTime = time.Now()
					break
				}

				cbef.Consumers = append(cbef.Consumers, c)

				cbef.status.LastConnectTime = time.Now()
				cbef.status.IsConnected = true

				c.conn.NotifyClose(connectionError)

				numProcessors := runtime.NumCPU() * 2
				log.Infof("Starting %d message processors\n", numProcessors)

				cbef.wg.Add(numProcessors)

				for w := 0; w < numProcessors; w++ {
					//inline worker goroutine
					log.Infof("Launching AMQP working %d goroutine", w)
					go func(cbef *CbEventForwarder, deliveries <-chan amqp.Delivery) {
						defer cbef.wg.Done()
						for delivery := range deliveries {
							cbef.processMessage(delivery.Body,
								delivery.RoutingKey,
								delivery.ContentType,
								delivery.Headers,
								delivery.Exchange)
						}

						log.Info("Worker exiting")
					}(cbef, deliveries)
				}

				for {
					select {
					case outputError := <-cbef.outputErrors:
						log.Errorf("ERROR during output: %s", outputError.Error())

						// hack to exit if the error happens while we are writing to a file
						outputType := cbef.config.OutputType
						if outputType == conf.FileOutputType || outputType == conf.SplunkOutputType || outputType == conf.HTTPOutputType {
							log.Error("File output error; exiting immediately.")
							c.Shutdown()
							cbef.wg.Wait()
							os.Exit(1)
						}
						err = outputError
					case closeError := <-connectionError:
						cbef.status.IsConnected = false
						cbef.status.LastConnectError = closeError.Error()
						cbef.status.ErrorTime = time.Now()

						log.Errorf("Connection closed: %s", closeError.Error())
						log.Info("Waiting for all workers to exit")
						cbef.wg.Wait()
						log.Info("All workers have exited")
						err = closeError
					case <-c.stopchan:
						log.Infof("Consumer told to stop")
						cbef.wg.Wait()
						log.Info("Consumer - all workers done - ")
						return
					}
				}
				//wait and reconnect
				log.Infof("Loop exited for unknown reason %v", err)
				c.Shutdown()
				cbef.wg.Wait()
				log.Infof("Loop exited - Will try again in 30 seconds %v ", err)
				time.Sleep(30 * time.Second)
			}
		}(i)
	}
}

func loadOutputFromPlugin(pluginPath string, pluginName string) output.OutputHandler {
	log.Infof("loadOutputFromPlugin: Trying to load plugin %s at %s", pluginName, pluginPath)
	plug, err := plugin.Open(path.Join(pluginPath, pluginName+".so"))
	if err != nil {
		log.Panic(err)
	}
	pluginHandlerFuncRaw, err := plug.Lookup("GetOutputHandler")
	if err != nil {
		log.Panicf("Failed to load plugin %v", err)
	}
	return pluginHandlerFuncRaw.(func() output.OutputHandler)()
}

func (cbef *CbEventForwarder) startOutputs() error {
	// Configure the specific output.
	// Valid options are: 'udp', 'tcp', 'file', 's3', 'syslog' ,"http",'splunk'
	var outputHandler output.OutputHandler

	parameters := cbef.config.OutputParameters

	switch cbef.config.OutputType {
	case conf.FileOutputType:
		outputHandler = &output.FileOutput{}
	case conf.TCPOutputType:
		outputHandler = &output.NetOutput{}
		parameters = "tcp:" + parameters
	case conf.UDPOutputType:
		outputHandler = &output.NetOutput{}
		parameters = "udp:" + parameters
	case conf.S3OutputType:
		outputHandler = &output.BundledOutput{Behavior: &output.S3Behavior{}}
	case conf.SyslogOutputType:
		outputHandler = &output.SyslogOutput{}
	case conf.HTTPOutputType:
		outputHandler = &output.BundledOutput{Behavior: &output.HTTPBehavior{}}
	case conf.SplunkOutputType:
		outputHandler = &output.BundledOutput{Behavior: &output.SplunkBehavior{}}
	case conf.PluginOutputType:
		outputHandler = loadOutputFromPlugin(cbef.config.PluginPath, cbef.config.Plugin)
	default:
		return fmt.Errorf("No valid output handler found (%d)", cbef.config.OutputType)
	}

	err := outputHandler.Initialize(parameters, &cbef.config)
	if err != nil {
		return err
	}

	expvar.Publish(fmt.Sprint("output_status_%s", cbef.name), expvar.Func(func() interface{} {
		ret := make(map[string]interface{})
		ret[outputHandler.Key()] = outputHandler.Statistics()

		switch cbef.config.OutputFormat {
		case conf.CEFOutputFormat:
			ret["format"] = "cef"
		case conf.LEEFOutputFormat:
			ret["format"] = "leef"
		case conf.JSONOutputFormat:
			ret["format"] = "json"
		case conf.TemplateOutputFormat:
			ret["format"] = "template"
		}

		switch cbef.config.OutputType {
		case conf.FileOutputType:
			ret["type"] = "file"
		case conf.UDPOutputType:
			ret["type"] = "net"
		case conf.TCPOutputType:
			ret["type"] = "net"
		case conf.S3OutputType:
			ret["type"] = "s3"
		case conf.HTTPOutputType:
			ret["type"] = "http"
		case conf.SplunkOutputType:
			ret["type"] = "splunk"
		}

		return ret
	}))

	log.Infof("Initialized output: %sb\n", outputHandler.String())
	return outputHandler.Go(cbef.results, cbef.outputErrors, cbef.controlchan)
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

	var controlchans []chan os.Signal = make([]chan os.Signal, 2)
	global_variables, configs, err := conf.ParseConfigs(configLocation)
	if err != nil {
		log.Fatal(err)
	}

	HTTPServerPort, _ := global_variables["http_server_port"].(string)

	dirs := [...]string{
		"/usr/share/cb/integrations/event-forwarder/content",
		"./static",
	}

	exportedVersion := expvar.NewString("version")
	if *debug {
		exportedVersion.Set(version + " (debugging on)")
		log.Debugf("*** Debugging enabled: messages may be sent via http://%s:%d/debug/sendmessage/<cbefinputname> ***",
			hostname, HTTPServerPort)
	} else {
		exportedVersion.Set(version)
	}
	expvar.Publish("debug", expvar.Func(func() interface{} {
		return *debug
	}))

	for _, dirname := range dirs {
		finfo, err := os.Stat(dirname)
		if err == nil && finfo.IsDir() {
			http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(dirname))))
			http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/static/", 301)
			})
			log.Infof("Diagnostics available via HTTP at http://%s:%d/", hostname, HTTPServerPort)
			break
		}
	}

	cbefs := make([]CbEventForwarder, 1)

	for _, config := range configs {

		cbapihandler := cbapi.CbAPIHandler{Config: &config}

		myjsmp := jsonmessageprocessor.JsonMessageProcessor{Config: &config, CbAPI: &cbapihandler}
		mypbmp := pbmessageprocessor.PbMessageProcessor{Config: &config}

		outputE := make(chan error)
		res := make(chan string, 100)

		cbef := CbEventForwarder{jsmp: myjsmp, pbmp: mypbmp, name: config.ServerName, outputErrors: outputE, results: res, numConsumers: 1, wg: &sync.WaitGroup{}, controlchan: make(chan os.Signal, 2), config: config, cbapi: cbapihandler}

		if config.PerformFeedPostprocessing {
			apiVersion, err := cbapihandler.GetCbVersion()
			if err != nil {
				log.Fatal("Could not get cb version: " + err.Error())
			} else {
				log.Infof("Enabling feed post-processing for server %s version %s.", config.CbServerURL, apiVersion)
			}
		}

		if *checkConfiguration {
			if err := cbef.startOutputs(); err != nil {
				log.Fatal(err)
			}
			os.Exit(0)
		}

		addrs, err := net.InterfaceAddrs()

		if err != nil {
			log.Fatal("Could not get IP addresses")
		}

		log.Infof("cb-event-forwarder version %s starting", version)

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				log.Infof("Interface address %s", ipnet.IP.String())
			}
		}

		log.Infof("Configured to capture events: %v", config.EventTypes)
		if err := cbef.startOutputs(); err != nil {
			log.Fatalf("Could not startOutputs: %s", err)
		}

		queueName := fmt.Sprintf("cb-event-forwarder:%s:%d", hostname, os.Getpid())

		if config.AMQPQueueName != "" {
			queueName = config.AMQPQueueName
		}

		cbef.launchConsumers(queueName)

		if cbef.config.AuditLog == true {
			log.Info("starting log file processing loop")
			go func() {
				errChan := cbef.logFileProcessingLoop()
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

		if *inputFile != "" {
			go func() {
				errChan := cbef.logFileProcessingLoop()
				for {
					select {
					case err := <-errChan:
						log.Infof("%v", err)
					}
				}
			}()
		}

		if *debug {
			http.HandleFunc(fmt.Sprintf("/debug/sendmessage/%", cbef.name), func(w http.ResponseWriter, r *http.Request) {
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

					err = cbef.outputMessage(parsedMsg)
					if err != nil {
						errMsg, _ := json.Marshal(map[string]string{"status": "error", "error": err.Error()})
						_, _ = w.Write(errMsg)
						return
					}
					log.Errorf("Sent test message: %s\n", string(msg))
				} else {
					err = cbef.outputMessage(map[string]interface{}{
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

		controlchans = append(controlchans, cbef.controlchan)
		cbefs = append(cbefs, cbef)
	}

	go http.ListenAndServe(fmt.Sprintf(":%d", HTTPServerPort), nil)
	log.Info("cb-event forwarder running...")
	sigs := make(chan os.Signal, 5)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	for {
		select {
		case sig := <-sigs:
			shouldret := false
			switch sig {
			case syscall.SIGTERM, syscall.SIGINT:
				for _, cbef := range cbefs {
					cbef.terminateConsumers()
				}
				shouldret = true
				fallthrough
			default:
				log.Debugf("CbEF process got signal %s propogating it to forwarders", sig)
				for _, cbef := range cbefs {
					cbef.controlchan <- sig
				}
				if shouldret {
					log.Debugf("CbEF process got exit signal")
					return
				}
			}
		}
	}
	log.Info("cb-event-forwarder exiting")
}
