package main

import (
	"encoding/json"
	"expvar"
	"flag"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/internal/cbapi"
	"github.com/carbonblack/cb-event-forwarder/internal/cbeventforwarder"
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"github.com/carbonblack/cb-event-forwarder/internal/consumer"
	"github.com/carbonblack/cb-event-forwarder/internal/filter"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/output"
	"github.com/carbonblack/cb-event-forwarder/internal/pbmessageprocessor"
	log "github.com/sirupsen/logrus"
	"net"
	"net/http"
	"os"
	"os/signal"
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

/*
 * Initializations
 */
func init() {
	flag.Parse()
}

func conversionFailure(i interface{}) {
	switch t := i.(type) {
	default:
		log.Infof("Failed to convert %T %s", t, t)
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

	config, err := conf.ParseConfig(configLocation)
	if err != nil {
		log.Fatal(err)
	}

	HTTPServerPort, _ := config["http_server_port"].(string)

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

	debugFlag := false
	if t, ok := config["debug"]; ok {
		debugFlag = t.(bool)
	}

	debugStore := "/tmp"
	if t, ok := config["debug_store"]; ok {
		debugStore = t.(string)
	}

	log.Infof("Trying to load event forwarder for config: %s", config)
	outputE := make(chan error)
	var myfilter *filter.Filter
	if t, ok := config["filter"]; ok {
		myfilter, err = filter.GetFilterFromCfg(t.(map[interface{}]interface{}))
	} else if err != nil {
		log.Panicf("Error instantiating Filter: %v", err)
	}

	consumerconfigs := make(map[interface{}]interface{})
	if t, ok := config["input"]; ok {
		consumerconfigs = t.(map[interface{}]interface{})
	}

	outputconfigs := make([]interface{},0)
	if t, ok := config["output"]; ok {
		outputconfigs = t.([]interface{})
	}

	res := make([]chan map[string]interface{}, len(outputconfigs))
	outputcontrolchannels := make([]*chan os.Signal, len(outputconfigs))
	i := 0
	for i < len(outputconfigs) {
		res[i] = make(chan map[string]interface{}, 100)
		controlchan := make(chan os.Signal, 2)
		outputcontrolchannels[i] = &controlchan
		i++
	}

	outputs, err := output.GetOutputsFromCfg(outputconfigs)
	if err != nil {
		log.Panicf("ERROR PROCESSING OUTPUT CONFIGURATIONS %v", err)
	} else {
		log.Infof("Found %d ouputs", len(outputs))
	}

	addToOutput := make(map[string]interface{})
	if toadd, ok := config["addToOutput"]; ok {
		addToOutputI := toadd.(map[interface{}]interface{})
		for k, v := range addToOutputI {
			addToOutput[k.(string)] = v
		}
	}

	removeFromOutput := make([]string, 0)
	if remove, ok := config["removeFromOutput"]; ok {
		rai := remove.([]interface{})
		removeFromOutput = make([]string, len(rai))
		for i, r := range rai {
			removeFromOutput[i] = r.(string)
		}
	}

	name := ""
	if n, ok := config["name"]; ok {
		name = n.(string)
	} else {
		log.Panicf("Must provide a name for each event-forwarder in configuration file")

	}

	cbef := cbeventforwarder.CbEventForwarder{Controlchans: outputcontrolchannels, AddToOutput: addToOutput, RemoveFromOutput: removeFromOutput, Name: name, Outputs: outputs, Filter: myfilter, OutputErrors: outputE, Results: res, Config: config}

	log.Infof("Starting Cb Event Forwarder %s", cbef.Name)
	log.Infof("Configured to remove keys: %s", cbef.RemoveFromOutput)
	log.Infof("Configured to add k-vs to output: %s", cbef.AddToOutput)

	for cbServerNamei, consumerConf := range consumerconfigs {
		log.Infof("%s , %s ", cbServerNamei, consumerConf)
		consumerConfMap, ok := consumerConf.(map[interface{}]interface{})
		if !ok {
			conversionFailure(consumerConf)
		}
		cbServerName, ok := cbServerNamei.(string)
		if !ok {
			conversionFailure(cbServerNamei)

		}
		cbServerURL := ""
		if t, ok := consumerConfMap["cb_server_url"]; ok {
			cbServerURL = t.(string)
		}
		cbapihandler, err := cbapi.CbAPIHandlerFromCfg(consumerConfMap)
		if err != nil {
			log.Panicf("%v", err)

		}
		myjsmp := jsonmessageprocessor.JsonMessageProcessor{DebugFlag: debugFlag, DebugStore: debugStore, CbAPI: cbapihandler, CbServerURL: cbServerURL}
		mypbmp := pbmessageprocessor.PbMessageProcessor{DebugFlag: debugFlag, DebugStore: debugStore, CbServerURL: cbServerURL}
		c, err := consumer.NewConsumerFromConf(cbef.OutputMessage, cbServerName, cbServerName, consumerConfMap, debugFlag, debugStore, cbef.ConsumerWaitGroup)
		if err != nil {
			log.Panicf("%v", err)
		}
		c.Jsmp = myjsmp
		c.Pbmp = mypbmp
		cbef.Consumers = append(cbef.Consumers, c)
	}

	if *checkConfiguration {
		if err := cbef.StartOutputs(); err != nil {
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

	if err := cbef.StartOutputs(); err != nil {
		log.Fatalf("Could not startOutputs: %s", err)
	}

	cbef.LaunchConsumers()

	if *inputFile != "" {
		go func() {
			errChan := cbef.InputFileProcessingLoop(*inputFile)
			for {
				select {
				case err := <-errChan:
					log.Infof("%v", err)
				}
			}
		}()
	}

	if *debug {
		http.HandleFunc(fmt.Sprintf("/debug/sendmessage/%", cbef.Name), func(w http.ResponseWriter, r *http.Request) {
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

				err = cbef.OutputMessage(parsedMsg)
				if err != nil {
					errMsg, _ := json.Marshal(map[string]string{"status": "error", "error": err.Error()})
					_, _ = w.Write(errMsg)
					return
				}
				log.Errorf("Sent test message: %s\n", string(msg))
			} else {
				err = cbef.OutputMessage(map[string]interface{}{
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

	go http.ListenAndServe(fmt.Sprintf(":%d", HTTPServerPort), nil)
	sigs := make(chan os.Signal)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	for {
		log.Info("cb-event forwarder running...")
		select {
		case sig := <-sigs:
			log.Infof("Signal handler got Signal %s ", sig)
			switch sig {
			case syscall.SIGTERM, syscall.SIGINT:
				//termiante consumers, then outputs
				cbef.TerminateConsumers()
				for _, controlchan := range cbef.Controlchans {
					log.Infof("trying to send signal to control channel %s", controlchan)
					*controlchan <- sig
					log.Infof("sent signal to control channel")
				}
				log.Info("cb-event-forwarders service waiting for consumers to be done")
				cbef.ConsumerWaitGroup.Wait()
				log.Info("cb-event-forwarders service waiting for outputs to be done")
				cbef.OutputWaitGroup.Wait()
				log.Info("cb-event-forwarder service exiting")
				return
			case syscall.SIGHUP: //propgate signals down to the outputs (HUP)
				for _, controlchan := range cbef.Controlchans {
					log.Infof("trying to send signal to control channel %s", controlchan)
					*controlchan <- sig
				}
			}
		}
	}
}
