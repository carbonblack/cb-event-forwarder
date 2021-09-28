package forwarder

import (
	"errors"
	"expvar"
	"fmt"
	. "github.com/carbonblack/cb-event-forwarder/pkg/config"
	. "github.com/carbonblack/cb-event-forwarder/pkg/outputs"
	"github.com/carbonblack/cb-event-forwarder/pkg/rabbitmq"
	"github.com/rcrowley/go-metrics"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

type OutputWithParameters struct {
	Output
	Parameters string
}

type EventForwarder struct {
	*Configuration
	Output           OutputWithParameters
	outputChan       chan string
	signalChan       chan os.Signal
	outputSignals    chan os.Signal
	consumer         *rabbitmq.Consumer
	workerWaitGroup  *sync.WaitGroup
	outputHasStopped *sync.Cond
	*Status
}

const OUTPUTCHANNELSIZE = 1000000

func NewEventForwarderFromConfig(signals chan os.Signal, cfg *Configuration) (EventForwarder, error) {
	output, err := loadOutputFromConfig(cfg)
	return EventForwarder{Status: NewStatus(), outputHasStopped: sync.NewCond(&sync.RWMutex{}), workerWaitGroup: &sync.WaitGroup{}, outputSignals: make(chan os.Signal), signalChan: signals, Configuration: cfg, Output: output, outputChan: make(chan string, OUTPUTCHANNELSIZE)}, err
}

func (forwarder *EventForwarder) Startup(hostname string) error {
	forwarder.setupMetrics()
	err := forwarder.StartOutput()
	if err != nil {
		return err
	}
	forwarder.outputMetrics()
	forwarder.startAMQPConsumer(hostname)
	forwarder.handleAuditLogs()
	return nil
}

func (forwarder *EventForwarder) StartOutput() error {

	err := forwarder.initializeOutput()
	if err != nil {
		return err
	}

	return forwarder.startOutput()
}

func (forwarder *EventForwarder) startOutput() error {
	return forwarder.Output.Go(forwarder.outputChan, forwarder.outputSignals, forwarder.outputHasStopped)
}

func (forwarder *EventForwarder) initializeOutput() error {
	err := forwarder.Output.Initialize(forwarder.Output.Parameters)
	if err != nil {
		return err
	}
	log.Infof("Initialized output: %s\n", forwarder.Output.String())
	return nil
}

func loadOutputFromConfig(cfg *Configuration) (output OutputWithParameters, err error) {
	output.Parameters = cfg.OutputParameters

	switch cfg.OutputType {
	case FileOutputType:
		output.Output = NewFileOutputFromConfig(cfg)
	case TCPOutputType:
		output.Output = NewNetOutputfromConfig(cfg)
		output.Parameters = "tcp:" + output.Parameters
	case UDPOutputType:
		output.Output = NewNetOutputfromConfig(cfg)
		output.Parameters = "udp:" + output.Parameters
	case S3OutputType:
		output.Output = NewNGS3OutputFromConfig(cfg)
	case OLDS3OutputType:
		output.Output = NewS3OutputFromConfig(cfg)
	case SyslogOutputType:
		output.Output = NewSyslogOutputFromConfig(cfg)
	case HTTPOutputType:
		output.Output = NewHTTPOutputFromConfig(cfg)
	case SplunkOutputType:
		output.Output = NewSplunkOutputFromConfig(cfg)
	case KafkaOutputType:
		output.Output = NewKafkaOutputFromConfig(cfg)
	default:
		return output, fmt.Errorf("No valid output handler found (%d)", cfg.OutputType)
	}
	return output, nil
}

func (forwarder *EventForwarder) startAMQPConsumer(hostname string) {
	queueName := fmt.Sprintf("cb-event-forwarder:%s:%d", hostname, os.Getpid())

	if forwarder.AMQPQueueName != "" {
		queueName = forwarder.AMQPQueueName
	}

	if forwarder.RunConsumer {
		go func(consumerNumber int) {
			log.Infof("Starting AMQP loop %d to %s on queue %s", consumerNumber, forwarder.AMQPURL(), queueName)
			for {
				err := forwarder.MessageProcessingLoop(forwarder.AMQPURL(), queueName, fmt.Sprintf("go-event-consumer-%d", consumerNumber))
				log.Infof("AMQP loop %d exited: %s. Sleeping for 30 seconds then retrying.", consumerNumber, err)
				time.Sleep(30 * time.Second)
			}
		}(1)
	}
}

func (forwarder *EventForwarder) MessageProcessingLoop(uri, queueName, consumerTag string) error {

	var dialer rabbitmq.AMQPDialer

	if forwarder.CannedInput {
		md := rabbitmq.NewMockAMQPDialer()
		go rabbitmq.RunCannedData(md.Connection, forwarder.CannedInputLocation)
		dialer = md
	} else {
		dialer = rabbitmq.StreadwayAMQPDialer{}
	}

	forwarder.consumer = rabbitmq.NewConsumer(uri, queueName, consumerTag, forwarder.UseRawSensorExchange, forwarder.EventTypes, dialer, forwarder.GetAMQPTLSConfigFromConf())

	deliveries, err := forwarder.consumer.Connect()

	if err != nil {
		forwarder.LastConnectError = err.Error()
		forwarder.ErrorTime = time.Now()
		return err
	}

	forwarder.LastConnectTime = time.Now()
	forwarder.IsConnected = true

	forwarder.startInputWorkers(deliveries)

	for closeError := range forwarder.consumer.ConnectionErrors {
		forwarder.IsConnected = false
		forwarder.LastConnectError = closeError.Error()
		forwarder.ErrorTime = time.Now()

		log.Errorf("Connection error: %s", closeError.Error())
	}

	log.Info("Waiting for all workers to exit")
	forwarder.workerWaitGroup.Wait()
	log.Info("All workers have exited")

	return nil
}

func (forwarder *EventForwarder) startInputWorkers(deliveries <-chan amqp.Delivery) {
	numProcessors := forwarder.NumProcessors

	log.Infof("Starting %d message processors\n", numProcessors)

	inputWorker := NewInputWorker(forwarder.OutputFormat, forwarder.outputChan, forwarder.Configuration, forwarder.Status)

	for i := 0; i < numProcessors; i++ {
		inputWorker.consume(forwarder.workerWaitGroup, deliveries)
	}
}

func (forwarder *EventForwarder) RunUntilExit() {

	defer log.Info("Event forwarder exited OK")

	handleExit := func(signal os.Signal) {
		log.Errorf("%s- exiting immediately.", signal)
		closeConsumer := func() {
			log.Debugf("AMQP Consumer shutdown %v", forwarder.consumer.Shutdown())
		}
		withTimeout(closeConsumer, 5*time.Second)
		withTimeout(forwarder.workerWaitGroup.Wait, forwarder.ExitTimeoutSeconds)
	}

	go func() {
		for {
			select {
			case signal := <-forwarder.signalChan:
				switch signal {
				case syscall.SIGTERM, syscall.SIGINT:
					handleExit(signal)
					forwarder.outputSignals <- signal
					return
				default:
					forwarder.outputSignals <- signal
				}
			}
		}
	}()

	forwarder.outputHasStopped.L.Lock()
	forwarder.outputHasStopped.Wait()
	forwarder.outputHasStopped.L.Unlock()
}

func (forwarder *EventForwarder) handleAuditLogs() {
	if forwarder.AuditLog == true {
		log.Info("starting log file processing loop")
		go func() {
			errChan := forwarder.logFileProcessingLoop()
			for {
				select {
				case err := <-errChan:
					log.Errorf("%v", err)
				}
			}
		}()

	} else {
		log.Info("Not starting file processing loop")
	}
}

func withTimeout(callback func(), timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		callback()
	}()
	select {
	case <-c:
		log.Debug("WaitTimeout callback completed successfully")
		return false
	case <-time.After(timeout):
		log.Debug("WaitTimeout hit")
		return true
	}
}

func (forwarder *EventForwarder) handleMetricsEnablement() {
	if !forwarder.RunMetrics {
		log.Infof("Running without metrics")
		metrics.UseNilMetrics = true
	} else {
		metrics.UseNilMetrics = false
		log.Infof("Running with metrics")
	}
}

func (forwarder *EventForwarder) setupMetrics() {
	forwarder.handleMetricsEnablement()

	forwarder.Healthy = metrics.NewHealthcheck(func(h metrics.Healthcheck) {
		if forwarder.IsConnected {
			h.Healthy()
		} else {
			h.Unhealthy(errors.New("Event Forwarder is not connected"))
		}
	})

	metrics.RegisterRuntimeMemStats(metrics.DefaultRegistry)

	metrics.Register("connection_status",
		expvar.Func(func() interface{} {
			res := make(map[string]interface{}, 0)
			res["last_connect_time"] = forwarder.LastConnectTime
			res["last_error_text"] = forwarder.LastConnectError
			res["last_error_time"] = forwarder.ErrorTime
			if forwarder.IsConnected {
				res["connected"] = true
				res["uptime"] = time.Now().Sub(forwarder.LastConnectTime).Seconds()
			} else {
				res["connected"] = false
				res["uptime"] = 0.0
			}

			return res
		}))
	metrics.Register("uptime", expvar.Func(func() interface{} {
		return time.Now().Sub(forwarder.StartTime).Seconds()
	}))
	metrics.Register("subscribed_events", expvar.Func(func() interface{} {
		return forwarder.EventTypes
	}))

	forwarder.StartTime = time.Now()
}

func (forwarder *EventForwarder) outputMetrics() {
	metrics.Register("output_status", expvar.Func(func() interface{} {
		ret := make(map[string]interface{})
		ret[forwarder.Output.Key()] = forwarder.Output.Statistics()

		switch forwarder.OutputFormat {
		case LEEFOutputFormat:
			ret["format"] = "leef"
		case JSONOutputFormat:
			ret["format"] = "json"
		}

		switch forwarder.OutputType {
		case FileOutputType:
			ret["type"] = "file"
		case UDPOutputType:
			ret["type"] = "net"
		case TCPOutputType:
			ret["type"] = "net"
		case OLDS3OutputType:
			fallthrough
		case S3OutputType:
			ret["type"] = "s3"
		case HTTPOutputType:
			ret["type"] = "http"
		case SplunkOutputType:
			ret["type"] = "splunk"
		}

		return ret
	}))
}

func (forwarder *EventForwarder) logFileProcessingLoop() <-chan error {

	errChan := make(chan error)

	spawnTailer := func(fName string, label string) {

		log.Debugf("Spawning file-tail routine for file: %s", fName)

		_, deliveries, err := NewFileConsumer(fName)

		if err != nil {
			errChan <- err
		}

		for delivery := range deliveries {
			log.Debugf("Trying to deliver log message %s", delivery)
			trimmedDelivery := strings.TrimSuffix(delivery, "\n")
			auditLogEvent := NewAuditLogEvent(trimmedDelivery, label, forwarder.ServerName)
			rawLogEvent, _ := auditLogEvent.asJson()
			outputMessage(rawLogEvent, forwarder.outputChan, forwarder.Status)
		}

	}

	go spawnTailer("/var/log/cb/audit/live-response.log", "audit.log.liveresponse")
	go spawnTailer("/var/log/cb/audit/banning.log", "audit.log.banning")
	go spawnTailer("/var/log/cb/audit/isolation.log", "audit.log.isolation")
	go spawnTailer("/var/log/cb/audit/useractivity.log", "audit.log.useractivity")
	return errChan
}
