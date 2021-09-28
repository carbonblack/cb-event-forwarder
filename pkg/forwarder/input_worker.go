package forwarder

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	. "github.com/carbonblack/cb-event-forwarder/pkg/config"
	"github.com/carbonblack/cb-event-forwarder/pkg/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/pkg/protobufmessageprocessor"
	. "github.com/carbonblack/cb-event-forwarder/pkg/sensorevents"
	. "github.com/carbonblack/cb-event-forwarder/pkg/utils"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"io/ioutil"
	"path"
	"sync"
)

func (inputWorker InputWorker) processZipPB(body []byte, routingKey, contentType string, headers amqp.Table, exchangeName string) {
	msgs, err := inputWorker.ProcessProtobufBundle(routingKey, body, headers)
	if err != nil {
		inputWorker.reportBundleDetails(routingKey, body, headers)
		inputWorker.reportError(routingKey, "Could not process raw zip bundle", err)
		return
	}
	inputWorker.OutputMessages(msgs)
}

func (inputWorker InputWorker) processPB(body []byte, routingKey, contentType string, headers amqp.Table, exchangeName string) {
	if exchangeName == "api.rawsensordata" {
		inputWorker.processZipPB(body, routingKey, contentType, headers, exchangeName)
	} else {
		msg, err := inputWorker.ProcessProtobufMessage(routingKey, body, headers)
		if err != nil {
			inputWorker.reportBundleDetails(routingKey, body, headers)
			inputWorker.reportError(routingKey, "Could not process body", err)
			return
		} else {
			outputMessage(msg, inputWorker.outputs, inputWorker.Status)
		}
	}
}

func (inputWorker InputWorker) OutputMessages(msgs [][]byte) {
	for _, msg := range msgs {
		outputMessage(msg, inputWorker.outputs, inputWorker.Status)
	}
}

func (inputWorker InputWorker) processJsonMessage(body []byte, routingKey string) {
	// Note for simplicity in implementation we are assuming the JSON output by the Cb server
	// is an object (that is, the top level JSON object is a dictionary and not an array or scalar value)
	var msg map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(body))

	// Ensure that we decode numbers in the JSON as integers and *not* float64s
	decoder.UseNumber()

	if err := decoder.Decode(&msg); err != nil {
		inputWorker.reportError(string(body), "Received error when unmarshaling JSON body", err)
		return
	}

	jsonMsgs, err := inputWorker.ProcessJSONMessage(msg, routingKey)
	if err == nil {
		for jsonMsg := range jsonMsgs {
			jsonBytes, err := json.Marshal(jsonMsg)
			if err == nil {
				outputMessage(jsonBytes, inputWorker.outputs, inputWorker.Status)
			}
		}
	}
}

func (inputWorker InputWorker) processMessage(body []byte, routingKey, contentType string, headers amqp.Table, exchangeName string) {
	inputWorker.InputEventCount.Mark(1)
	inputWorker.InputByteCount.Mark(int64(len(body)))
	//
	// Process message based on ContentType
	//
	switch contentType {
	case "application/zip":
		inputWorker.processZipPB(body, routingKey, contentType, headers, exchangeName)
	case "application/protobuf":
		inputWorker.processPB(body, routingKey, contentType, headers, exchangeName)
	case "application/json":
		inputWorker.processJsonMessage(body, routingKey)
	default:
		inputWorker.reportError(string(body), "Unknown content-type", errors.New(contentType))
		return
	}
}

func outputMessage(msg []byte, results chan<- string, status *Status) {
	outmsg := string(msg)

	if len(outmsg) > 0 {
		status.OutputEventCount.Mark(1)
		status.OutputByteCount.Mark(int64(len(outmsg)))
		results <- outmsg
	}
}

type InputWorker struct {
	outputs chan<- string
	protobufmessageprocessor.ProtobufMessageProcessor
	jsonmessageprocessor.JsonMessageProcessor
	*Status
	DebugFlag  bool
	DebugStore string
	OutputFormat int
}

func NewInputWorker(outputFormat int, outputs chan<- string, cfg *Configuration, status *Status) InputWorker {
	return InputWorker{OutputFormat: outputFormat, Status: status, outputs: outputs, ProtobufMessageProcessor: protobufmessageprocessor.NewProtobufMessageProcessor(cfg), JsonMessageProcessor: jsonmessageprocessor.NewJsonMessageProcessor(cfg), DebugStore: cfg.DebugStore, DebugFlag: cfg.DebugFlag}
}

func (inputWorker InputWorker) consume(wg *sync.WaitGroup, deliveries <-chan amqp.Delivery) {
	wg.Add(1)
	go func() {
		defer wg.Done()

		for delivery := range deliveries {
			inputWorker.processMessage(delivery.Body,
				delivery.RoutingKey,
				delivery.ContentType,
				delivery.Headers,
				delivery.Exchange)
		}

		log.Debug("AMQP INPUT Worker exiting")
	}()
}

/*
 * worker
 */

// TODO: change this into an error channel
func (inputWorker InputWorker) reportError(d string, errmsg string, err error) {
	inputWorker.ErrorCount.Mark(1)
	log.Debugf("%s when processing %s: %s", errmsg, d, err)
}

func (inputWorker InputWorker) reportBundleDetails(routingKey string, body []byte, headers amqp.Table) {
	log.Errorf("Error while processing message through routing key %s:", routingKey)

	var env *CbEnvironmentMsg
	env, err := CreateEnvMessage(headers)
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
	if inputWorker.DebugFlag {
		h := md5.New()
		h.Write(body)
		var fullFilePath string
		fullFilePath = path.Join(inputWorker.DebugStore, fmt.Sprintf("/event-forwarder-%X", h.Sum(nil)))
		log.Debugf("Writing Bundle to disk: %s", fullFilePath)
		ioutil.WriteFile(fullFilePath, body, 0444)
	}
}
