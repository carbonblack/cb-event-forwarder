package main

/**
* Copyright 2016 Confluent Inc.
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
* http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.


CARBON BLACK 2018 - Zachary Estep - Using this code as the basis for a producer interface that is mockable
*/

import (
	"github.com/carbonblack/cb-event-forwarder/internal/encoder"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/output"
	"github.com/carbonblack/cb-event-forwarder/internal/pbmessageprocessor"
	"github.com/confluentinc/confluent-kafka-go/kafka"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/mock"
	"io/ioutil"
	"os"
	"path"
	"sync"
	"syscall"
	"testing"
)

var jsmp jsonmessageprocessor.JsonMessageProcessor = jsonmessageprocessor.JsonMessageProcessor{}
var eventMap map[string]interface{} = map[string]interface{}{
	"watchlist.#":                  true,
	"feed.#":                       true,
	"alert.#":                      true,
	"ingress.event.process":        true,
	"ingress.event.procstart":      true,
	"ingress.event.netconn":        true,
	"ingress.event.procend":        true,
	"ingress.event.childproc":      true,
	"ingress.event.moduleload":     true,
	"ingress.event.module":         true,
	"ingress.event.filemod":        true,
	"ingress.event.regmod":         true,
	"ingress.event.tamper":         true,
	"ingress.event.crossprocopen":  true,
	"ingress.event.remotethread":   true,
	"ingress.event.processblock":   true,
	"ingress.event.emetmitigation": true,
	"binaryinfo.#":                 true,
	"binarystore.#":                true,
	"events.partition.#":           true,
}
var pbmp pbmessageprocessor.PbMessageProcessor = pbmessageprocessor.PbMessageProcessor{EventMap: eventMap}

type MockedProducer struct {
	mock.Mock
	outfile *os.File
}

func (mp *MockedProducer) Produce(msg *kafka.Message, deliveryChan chan kafka.Event) error {
	log.Infof("MockProducer::Producer called with %s %v", msg, deliveryChan)
	topic := "topic"
	deliveryChan <- &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Value:          msg.Value,
	}
	mp.outfile.Write(msg.Value)
	log.Info("MockProducer::Producer returning")
	return nil
}

func (mp *MockedProducer) String() string {
	return "MockedKafkaProducer"
}

func (mp *MockedProducer) Events() chan kafka.Event {
	return make(chan kafka.Event, 1)
}

func (mp *MockedProducer) ProduceChannel() chan *kafka.Message {
	return make(chan *kafka.Message, 1)
}

func (mp *MockedProducer) Close() {
	return
}

func (mp *MockedProducer) Flush(timeout int) int {
	return 0
}
func (mp *MockedProducer) Len() int {
	return 0
}

func (mp *MockedProducer) GetMetadata(topic *string, allTopics bool, timeoutMs int) (*kafka.Metadata, error) {
	return nil, nil
}

func (mp *MockedProducer) QueryWatermarkOffsets(topic string, partition int32, timeoutMs int) (low, high int64, err error) {
	return 0, 0, nil
}

func (mp *MockedProducer) OffsetsForTimes(times []kafka.TopicPartition, timeoutMs int) (offsets []kafka.TopicPartition, err error) {
	return make([]kafka.TopicPartition, 0), nil
}

type outputMessageFunc func([]map[string]interface{}) (string, error)

func TestKafkaOutput(t *testing.T) {
	// create an instance of our test object

	t.Logf("Starting kafkaoutput test")
	outputDir := "../../../test_output/real_output_kafka"
	os.MkdirAll(outputDir, 0755)

	/*outputFile, err := os.Create(path.Join(outputDir, "/kafkaoutput")) // For read access.
	if err != nil {
		t.Errorf("Coudln't open httpoutput file %v", err)
		t.FailNow()
		return
	}*/
	//mockProducer := new(MockedProducer)
	//mockProducer.outfile = outputFile
	producer, _ := kafka.NewProducer(&kafka.ConfigMap{"bootstrap.servers": "localhost:9092"})
	testEncoder := encoder.NewJSONEncoder()
	var outputHandler output.OutputHandler = &KafkaOutput{Producer: producer, Encoder: &testEncoder, deliveryChannel: make(chan kafka.Event)}

	processTestEventsWithRealHandler(t, outputDir, jsonmessageprocessor.MarshalJSON, outputHandler)

}

func processTestEventsWithRealHandler(t *testing.T, outputDir string, outputFunc outputMessageFunc, oh output.OutputHandler) {
	t.Logf("Tring to preform test with %v %s", oh, oh)

	/*formats := [...]struct {
		formatType string
		process    func(string, []byte) ([]map[string]interface{}, error)
	}{{"json", jsmp.ProcessJSON}, {"protobuf", pbmp.ProcessProtobuf}}
	*/

	formats := [...]struct {
		formatType string
		process    func(string, []byte) ([]map[string]interface{}, error)
	}{{"protobuf", pbmp.ProcessProtobuf}}

	messages := make(chan map[string]interface{}, 100)
	errors := make(chan error)
	controlchan := make(chan os.Signal, 5)
	var wg sync.WaitGroup
	wg.Add(1)

	oh.Go(messages, errors, controlchan, wg)
	for true {
		for _, format := range formats {
			pathname := path.Join("../../../test/raw_data", format.formatType)
			fp, err := os.Open(pathname)
			if err != nil {
				t.Logf("Could not open %s", pathname)
				t.FailNow()
			}

			infos, err := fp.Readdir(0)
			if err != nil {
				t.Logf("Could not enumerate directory %s", pathname)
				t.FailNow()
			}

			fp.Close()

			for _, info := range infos {
				if !info.IsDir() {
					continue
				}

				routingKey := info.Name()
				os.MkdirAll(outputDir, 0755)

				// process all files inside this directory
				routingDir := path.Join(pathname, info.Name())
				fp, err := os.Open(routingDir)
				if err != nil {
					t.Logf("Could not open directory %s", routingDir)
					t.FailNow()
				}

				files, err := fp.Readdir(0)
				if err != nil {
					t.Errorf("Could not enumerate directory %s; continuing", routingDir)
					continue
				}

				fp.Close()
				for _, fn := range files {
					if fn.IsDir() {
						continue
					}

					fp, err := os.Open(path.Join(routingDir, fn.Name()))
					if err != nil {
						t.Errorf("Could not open %s for reading", path.Join(routingDir, fn.Name()))
						continue
					}
					b, err := ioutil.ReadAll(fp)
					if err != nil {
						t.Errorf("Could not read %s", path.Join(routingDir, fn.Name()))
						continue
					}

					fp.Close()

					msgs, err := format.process(routingKey, b)
					if err != nil {
						t.Errorf("Error processing %s: %s", path.Join(routingDir, fn.Name()), err)
						continue
					}
					if len(msgs[0]) == 0 {
						t.Errorf("got zero messages out of: %s/%s", routingDir, fn.Name())
						continue
					}
					for _, msg := range msgs {
						messages <- msg
					}
					_, err = outputFunc(msgs)
					if err != nil {
						t.Errorf("Error serializing %s: %s", path.Join(routingDir, fn.Name()), err)
						continue
					}
				}
			}
		}

	}
	t.Logf("Done with test for %s ", oh)
	controlchan <- syscall.SIGTERM
}
