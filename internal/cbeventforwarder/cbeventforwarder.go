package cbeventforwarder

import (
	"encoding/json"
	"expvar"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/internal/cbapi"
	"github.com/carbonblack/cb-event-forwarder/internal/consumer"
	"github.com/carbonblack/cb-event-forwarder/internal/filter"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/output"
	"github.com/carbonblack/cb-event-forwarder/internal/pbmessageprocessor"
	log "github.com/sirupsen/logrus"
	"os"
	"sync"
	"syscall"
	"time"
)

type Status struct {
	OutputEventCount   *expvar.Int
	FilteredEventCount *expvar.Int
	ErrorCount         *expvar.Int
	IsConnected        bool
	LastConnectTime    time.Time
	StartTime          time.Time
	LastConnectError   string
	ErrorTime          time.Time
	sync.RWMutex
}

type CbEventForwarder struct {
	Config            map[string]interface{}
	AddToOutput       map[string]interface{}
	RemoveFromOutput  []string
	OutputErrors      chan error
	Results           []chan map[string]interface{}
	Consumers         []*consumer.Consumer
	Outputs           []output.OutputHandler
	Filter            *filter.Filter
	Controlchans      []*chan os.Signal ///controls output
	Status            Status
	DebugFlag         bool
	DebugStore        string
	Name              string
	ConsumerWaitGroup sync.WaitGroup
	OutputWaitGroup   sync.WaitGroup
}

/*
 * worker
 */

func (cbef *CbEventForwarder) OutputMessage(msg map[string]interface{}) error {
	var err error
	//
	// Marshal result into the correct output format
	//
	//msg["cb_server"] = cbef.CbServerName

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

		if len(msg) > 0 && err == nil {
			cbef.Status.OutputEventCount.Add(1)
			for _, r := range cbef.Results {
				select {
				case r <- msg:
					log.Debugf("Sent event...")
				default:
					log.Debugf("Dropping event for output because channel send failed/timedout... %s", msg)
				}
			}
		} else if err != nil {
			return err
		}
	} else { //EventDropped due to filter
		cbef.Status.FilteredEventCount.Add(1)
		log.Debugf("Filtered Event %d", cbef.Status.FilteredEventCount)
	}
	return nil
}

func (cbef *CbEventForwarder) InputFileProcessingLoop(inputFile string) <-chan error {
	errChan := make(chan error)
	go func() {
		log.Debugf("Opening input file : %s", inputFile)
		_, deliveries, err := consumer.NewFileConsumer(inputFile)
		if err != nil {
			cbef.Status.LastConnectError = err.Error()
			cbef.Status.ErrorTime = time.Now()
			errChan <- err
		}
		for delivery := range deliveries {
			log.Debug("Trying to deliver log message %s", delivery)
			msgMap := make(map[string]interface{})
			err := json.Unmarshal([]byte(delivery), &msgMap)
			if err != nil {
				cbef.Status.LastConnectError = err.Error()
				cbef.Status.ErrorTime = time.Now()
				errChan <- err
			}
			cbef.OutputMessage(msgMap)
		}
	}()
	return errChan
}

func (cbef *CbEventForwarder) startExpvarPublish() {
	expvar.Publish(fmt.Sprintf("connection_status_%s", cbef.Name),
		expvar.Func(func() interface{} {
			res := make(map[string]interface{}, 0)
			res["last_connect_time"] = cbef.Status.LastConnectTime
			res["last_error_text"] = cbef.Status.LastConnectError
			res["last_error_time"] = cbef.Status.ErrorTime
			if cbef.Status.IsConnected {
				res["connected"] = true
				res["uptime"] = time.Now().Sub(cbef.Status.LastConnectTime).Seconds()
			} else {
				res["connected"] = false
				res["uptime"] = 0.0
			}

			return res
		}))
}

//Terminate consumers

func (cbef *CbEventForwarder) TerminateConsumers() {
	log.Debugf("%s trying to stop consumers...", cbef.Name)
	for _, consumer := range cbef.Consumers {
		log.Debugf("Consumer = %s stopchan = %s", consumer.CbServerName, &consumer.Stopchan)
		consumer.Stopchan <- struct{}{}
	}
}

// launch the amqp consumer goroutines
func (cbef *CbEventForwarder) LaunchConsumers() {
	for _, c := range cbef.Consumers {
		log.Infof("%s launching amqp consumer %s", cbef.Name, c.CbServerName)
		c.Consume()
	}
}

func (cbef *CbEventForwarder) StartOutputs() error {
	for i, outputHandler := range cbef.Outputs {
		log.Infof("Trying to start output %s", outputHandler)
		expvar.Publish(fmt.Sprint("output_status_%d", i), expvar.Func(func() interface{} {
			ret := make(map[string]interface{})
			ret[outputHandler.Key()] = outputHandler.Statistics()
			ret["type"] = outputHandler.String()
			return ret
		}))
		if err := outputHandler.Go(cbef.Results[i], cbef.OutputErrors, *cbef.Controlchans[i], cbef.OutputWaitGroup); err != nil {
			return err
		}
		log.Infof("Successfully Initialized output: %s ", outputHandler.String())

		go func() {
			select {
			case outputError := <-cbef.OutputErrors:
				log.Errorf("ERROR during output: %s", outputError.Error())
			}
		}()
	}
	return nil
}

func conversionFailure(i interface{}) {
	switch t := i.(type) {
	default:
		log.Errorf("Failed to convert %T %s", t, t)
	}
}

func GetCbEventForwarderFromCfg(config map[string]interface{}) CbEventForwarder {

	debugFlag := false
	if t, ok := config["debug"]; ok {
		debugFlag = t.(bool)
	}

	debugStore := "/tmp"
	if t, ok := config["debug_store"]; ok {
		debugStore = t.(string)
	}

	useTimeFloat := true
	if t, ok := config["use_time_float"]; ok {
		useTimeFloat = t.(bool)
	}

	log.Debugf("Trying to load event forwarder for config: %s", config)

	outputE := make(chan error)

	var myfilter *filter.Filter
	var err error = nil

	if t, ok := config["filter"]; ok {
		myfilter = filter.GetFilterFromCfg(t.(map[interface{}]interface{}))
	}

	consumerconfigs := make(map[interface{}]interface{})
	if t, ok := config["input"]; ok {
		consumerconfigs = t.(map[interface{}]interface{})
	}

	outputconfigs := make([]interface{}, 0)
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
		log.Infof("Found %d ouputs...", len(outputs))
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

	name := "cb-event-forwarder"
	if n, ok := config["name"]; ok {
		name = n.(string)
	}

	cbef := CbEventForwarder{Controlchans: outputcontrolchannels, AddToOutput: addToOutput, RemoveFromOutput: removeFromOutput, Name: name, Outputs: outputs, Filter: myfilter, OutputErrors: outputE, Results: res, Config: config, Status: Status{ErrorCount: expvar.NewInt("cbef_error_count"), FilteredEventCount: expvar.NewInt("filtered_event_count"), OutputEventCount: expvar.NewInt("output_event_count")}}

	log.Infof("Starting Cb Event Forwarder %s", cbef.Name)
	log.Infof("Configured to remove keys: %s", cbef.RemoveFromOutput)
	log.Infof("Configured to add k-vs to output: %s", cbef.AddToOutput)

	for cbServerNameI, consumerConf := range consumerconfigs {
		cbServerName := cbServerNameI.(string)
		log.Debugf("%s , %s ", cbServerName, consumerConf)

		consumerConfMap, ok := consumerConf.(map[interface{}]interface{})
		if !ok {
			conversionFailure(consumerConf)
		}

		cbServerURL := ""
		if t, ok := consumerConfMap["cb_server_url"]; ok {
			cbServerURL = t.(string)
		}

		var cbapihandler *cbapi.CbAPIHandler = nil

		if postprocess, ok := consumerConfMap["post_processing"]; ok {
			if ppmap, ok := postprocess.(map[interface{}]interface{}); ok {
				cbapihandler_temp, err := cbapi.CbAPIHandlerFromCfg(ppmap, cbServerURL)
				if err != nil {
					log.Panicf("Error getting cbapihandler from configuration: %v", err)
				} else {
					cbapihandler = cbapihandler_temp
				}
			} else {
				log.Panicf("Error getting cbapihandler from configuration: %v", err)
			}
		}

		myjsmp := jsonmessageprocessor.JsonMessageProcessor{DebugFlag: debugFlag, DebugStore: debugStore, CbAPI: cbapihandler, CbServerURL: cbServerURL, UseTimeFloat: useTimeFloat}
		mypbmp := pbmessageprocessor.PbMessageProcessor{DebugFlag: debugFlag, DebugStore: debugStore, CbServerURL: cbServerURL, UseTimeFloat: useTimeFloat}
		c, err := consumer.NewConsumerFromConf(cbef.OutputMessage, cbServerName, cbServerName, consumerConfMap, debugFlag, debugStore, cbef.ConsumerWaitGroup)
		if err != nil {
			log.Panicf("Error consturcting consumer from configuration: %v", err)
		}
		eventMap := make(map[string]interface{})
		for _, e := range c.RoutingKeys {
			eventMap[e] = true
		}
		mypbmp.EventMap = eventMap
		c.Jsmp = myjsmp
		c.Pbmp = mypbmp
		cbef.Consumers = append(cbef.Consumers, c)
	}
	return cbef
}

// The event forwarder GO
//  The sigs channel should be hooked up to system or similar
// This controls the event forwarder, and should be used to cause it to gracefully exit/clear output buffers
// The optional (pass null to opt-out) inputFile argument (usually from commandline) explicity lists an input jsonfile to grab
// and use for (additional) input
func (cbef *CbEventForwarder) Go(sigs chan os.Signal, inputFile *string) {
	cbef.startExpvarPublish()

	if err := cbef.StartOutputs(); err != nil {
		log.Fatalf("Could not startOutputs: %v", err)
	}

	cbef.LaunchConsumers()

	if inputFile != nil {
		go func() {
			errChan := cbef.InputFileProcessingLoop(*inputFile)
			for {
				select {
				case err := <-errChan:
					log.Errorf("Input file processing loop error: %v", err)
				}
			}
		}()
	}

	for {
		log.Info("cb-event forwarder running...")
		select {
		case sig := <-sigs:
			log.Debugf("Signal handler got Signal %s ", sig)
			switch sig {
			case syscall.SIGTERM, syscall.SIGINT:
				//termiante consumers, then outputs
				log.Info("cb-event-forwarder signalled to shutdown...beginning shutdown")
				cbef.TerminateConsumers()

				//should also be a method something like 'stopOutputs(sig)'
				for _, controlchan := range cbef.Controlchans {
					log.Debugf("Propogating Signal %s to output control channel", sig)
					*controlchan <- sig
				}
				log.Debugf("cb-event-forwarder waiting for AMQP consumer(s) to be done...")
				cbef.ConsumerWaitGroup.Wait()
				log.Debugf("cb-event-forwarder service waiting for output(s) to be done...")
				cbef.OutputWaitGroup.Wait()
				log.Info("Consumer(s), output(s) finished gracefully - cb-event-forwarder service exiting")
				return
			case syscall.SIGHUP: //propgate signals down to the outputs (HUP)
				for _, controlchan := range cbef.Controlchans {
					log.Debugf("Propogating  HUP signal to output control channel ")
					*controlchan <- sig
				}
			}
		}
	}
}
