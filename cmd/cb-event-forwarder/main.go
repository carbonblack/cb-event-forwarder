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
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"github.com/carbonblack/cb-event-forwarder/internal/filter"
	"github.com/carbonblack/cb-event-forwarder/internal/encoder"
	"github.com/carbonblack/cb-event-forwarder/internal/output"
	"github.com/carbonblack/cb-event-forwarder/internal/sensor_events"
	"github.com/carbonblack/cb-event-forwarder/internal/pbmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"plugin"
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



type CbEventForwarder struct {
	config       map[string] interface{}
	name         string
	AddToOutput	map[string] interface{}
	RemoveFromOutput [] string
	outputErrors chan error
	results      []chan string
	Consumers    []*Consumer
	Outputs []output.OutputHandler
	Encoder encoder.Encoder
	Filter	*filter.Filter
	controlchan  chan os.Signal
	status Status
	DebugFlag bool
	DebugStore string
}



/*
 * worker
 */

// TODO: change this into an error channel
func (c * Consumer) reportError(d string, errmsg string, err error) {
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

func (c * Consumer) processMessage(body []byte, routingKey, contentType string, headers amqp.Table, exchangeName string) {
	log.Infof("In process message!")
	//c.status.InputEventCount.Add(1)

	var err error
	var msgs []map[string]interface{}

	//
	// Process message based on ContentType
	//
	if contentType == "application/zip" {
		msgs, err = c.pbmp.ProcessRawZipBundle(routingKey, body, headers)
		if err != nil {
			reportBundleDetails(routingKey, body, headers, c.DebugFlag, c.DebugStore)
			return
		}
	} else if contentType == "application/protobuf" {
		// if we receive a protobuf through the raw sensor exchange, it's actually a protobuf "bundle" and not a
		// single protobuf
		if exchangeName == "api.rawsensordata" {
			msgs, err = c.pbmp.ProcessProtobufBundle(routingKey, body, headers)
		} else {
			msg, err := c.pbmp.ProcessProtobufMessage(routingKey, body, headers)
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

		msgs, err = c.jsmp.ProcessJSONMessage(msg, routingKey)
	} else {
		c.reportError(string(body), "Unknown content-type", errors.New(contentType))
		return
	}

	for _, msg := range msgs {
		msg["cb_server"] = c.CbServerName
		if c.PerformFeedPostprocessing {
			go func(msg map[string]interface{}) {
				outputMsg := c.jsmp.PostprocessJSONMessage(msg)
				c.OutputMessageFunc(outputMsg)
			}(msg)
		} else {
			err = c.OutputMessageFunc(msg)
			if err != nil {
				c.reportError(string(body), "Error marshaling message", err)
			}
		}
	}
}

func (cbef *CbEventForwarder) outputMessage(msg map[string]interface{}) error {

	log.Infof("Cb event forardering processing message %s",msg)
	var err error

	//
	// Marshal result into the correct output format
	//
	//msg["cb_server"] = cbef.ServerName

	// Add key=value pairs that has been configured to be added
	for key, val := range cbef.AddToOutput {
		msg[key] = val
	}

	// Remove keys that have been configured to be removed
	for _, v := range cbef.RemoveFromOutput {
		delete(msg, v)
	}

	//Apply Event Filter if specified
	keepEvent := true
	if cbef.Filter != nil {
		keepEvent = cbef.Filter.FilterEvent(msg)
	}

	if keepEvent {
		outmsg,_ := cbef.Encoder.Encode(msg)

		if len(outmsg) > 0 && err == nil {
			//cbef.status.OutputEventCount.Add(1)
			for _,r := range cbef.results {
				r <- string(outmsg)
			}
		} else if err != nil {
			return err
		}
	} else { //EventDropped due to filter
		//cbef.status.FilteredEventCount.Add(1)
	}
	log.Infof("Done outputing message")
	return nil
}
/*
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

	} */

	/* maps audit log labels to event types
	AUDIT_TYPES = {
	    "cb-audit-isolation": Audit_Log_Isolation,
	    "cb-audit-banning": Audit_Log_Banning,
	    "cb-audit-live-response": Audit_Log_Liveresponse,
	    "cb-audit-useractivity": Audit_Log_Useractivity
	}
	*/
	/*
	go spawnTailer("/var/log/cb/audit/live-response.log", "audit.log.liveresponse")
	go spawnTailer("/var/log/cb/audit/banning.log", "audit.log.banning")
	go spawnTailer("/var/log/cb/audit/isolation.log", "audit.log.isolation")
	go spawnTailer("/var/log/cb/audit/useractivity.log", "audit.log.useractivity")
	return errChan
}*/
/*

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
				cbefs.status.ErrorTime = time.Now()
				errChan <- err
			}
			cbef.outputMessage(msgMap)
		}
	}()
	return errChan
}*/

func (cbef *Consumer) startExpvarPublish() {
	cbef.status.InputEventCount = expvar.NewInt("input_event_count")
	cbef.status.OutputEventCount = expvar.NewInt("output_event_count")
	cbef.status.FilteredEventCount = expvar.NewInt("filtered_event_count")
	cbef.status.ErrorCount = expvar.NewInt("error_count")
	/*
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
		return cbef.EventTypes
	})) */

	cbef.status.StartTime = time.Now()
}

func (cbef *CbEventForwarder) terminateConsumers() {
	for _, consumer := range cbef.Consumers {
		consumer.stopchan <- struct{}{}
	}
}

func (cbef *CbEventForwarder) launchConsumers() {
	//cbef.startExpvarPublish()

	for _, c  := range cbef.Consumers{
		c.Consume()
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
	for i, outputHandler := range cbef.Outputs {
		/*expvar.Publish(fmt.Sprint("output_status_%s", cbef.name), expvar.Func(func() interface{} {
			ret := make(map[string]interface{})
			ret[outputHandler.Key()] = outputHandler.Statistics()
			ret["format"] = cbef.Encoder.String()
			ret["type"] = outputHandler.String()
			return ret
		}))*/

		log.Infof("Initialized output: %sb\n", outputHandler.String())
		if err := outputHandler.Go(cbef.results[i], cbef.outputErrors, cbef.controlchan); err != nil {
			return err
		}
		go func() {
			select {
			case outputError := <-cbef.outputErrors:
				log.Errorf("ERROR during output: %s", outputError.Error())
				os.Exit(1)
			}
		}()
	}
	return nil
}

func conversionFailure(i interface{}) {
	switch t := i.(type) {
		default:
			log.Infof("Failed to convert %T %s",t,t)
	}
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

	debugFlag := false
	if t, ok := global_variables["debug"]; ok {
		debugFlag = t.(bool)
	}

	debugStore := "/tmp"
	if t, ok := global_variables["debug_store"]; ok {
		debugStore = t.(string)
	}

	for _, config := range configs {
		log.Infof("Trying to load event forwarder for config: %s",config)
		outputE := make(chan error)

		var err error = nil
		var myencoder encoder.Encoder
		if t, ok := config["format"]; ok {
			myencoder,err = encoder.GetEncoderFromCfg(t.(map[interface{}] interface{}))
		} else if err != nil {
			log.Panicf("Error instantiating Encoder: %v",err)
		}

		var myfilter * filter.Filter
		if t, ok := config["filter"]; ok {
			myfilter, err = filter.GetFilterFromCfg(t.(map[interface{}] interface{}))
		} else if err != nil {
			log.Panicf("Error instantiating Filter: %v",err)
		}

		consumerconfigs := make(map[interface{}] interface{})
		if t, ok  := config["input"]; ok {
			consumerconfigs = t.(map [interface{}] interface{})
		}

		outputconfigs := make(map[interface{}] interface{})
		if t, ok  := config["output"]; ok {
			outputconfigs = t.(map [interface{}] interface{})
		}
		res := make([] chan string,len(outputconfigs))

		outputs,err := output.GetOutputsFromCfg(outputconfigs)
		if err != nil {
			log.Panicf("ERROR PROCESSING OUTPUT CONFIGURATIONS %v",err)
		}
		i := 0
		for ( i < len(outputconfigs) ) {
			res[i] = make(chan string, 100)
			i++
		}

		cbef := CbEventForwarder{ Outputs: outputs ,Encoder: myencoder, Filter: myfilter, outputErrors: outputE, results: res, controlchan: make(chan os.Signal, 2), config: config}

		for cbServerNamei, consumerConf := range consumerconfigs {
				log.Infof("%s , %s ", cbServerNamei, consumerConf)
				consumerConfMap,ok := consumerConf.(map[interface{}] interface{})
				if !ok {
					conversionFailure(consumerConf)
				}
				cbServerName,ok := cbServerNamei.(string)
				if !ok {
					conversionFailure(cbServerNamei)

				}
				cbServerURL := ""
				if t, ok := consumerConfMap["cb_server_url"]; ok {
					cbServerURL = t.(string)
				}
				cbapihandler, err := cbapi.CbAPIHandlerFromCfg(consumerConfMap)
				if err != nil {
					log.Panicf("%v",err)

				}
				myjsmp := jsonmessageprocessor.JsonMessageProcessor{DebugFlag:debugFlag, DebugStore: debugStore, CbAPI: cbapihandler, CbServerURL: cbServerURL}
				mypbmp := pbmessageprocessor.PbMessageProcessor{DebugFlag: debugFlag, DebugStore: debugStore, CbServerURL: cbServerURL}
				c, err := NewConsumerFromConf(cbef.outputMessage,cbServerName,cbServerName,consumerConfMap,debugFlag, debugStore)
				if err != nil  {
					log.Panicf("%v",err)
				}
				c.jsmp = myjsmp
				c.pbmp = mypbmp
				//c.status.LastConnectTime = time.Now()
				//c.status.IsConnected = true
			        cbef.Consumers = append(cbef.Consumers, c)
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

		if err := cbef.startOutputs(); err != nil {
			log.Fatalf("Could not startOutputs: %s", err)
		}



		cbef.launchConsumers()
		/*
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
		}*/
		/*
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
		}*/

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
