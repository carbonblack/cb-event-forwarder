package main

import (
	"encoding/json"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/internal/output"
	"github.com/confluentinc/confluent-kafka-go/kafka"
	log "github.com/sirupsen/logrus"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Producer implements a High-level Apache Kafka Producer instance ZE 2018
// This allows Mocking producers w/o actual contact to kafka broker for testing purposes
type WrappedProducer interface {
	String() string

	Produce(msg *kafka.Message, deliveryChan chan kafka.Event) error

	Events() chan kafka.Event

	ProduceChannel() chan *kafka.Message

	Len() int

	Flush(timeoutMs int) int

	Close()

	GetMetadata(topic *string, allTopics bool, timeoutMs int) (*kafka.Metadata, error)

	QueryWatermarkOffsets(topic string, partition int32, timeoutMs int) (low, high int64, err error)

	OffsetsForTimes(times []kafka.TopicPartition, timeoutMs int) (offsets []kafka.TopicPartition, err error)
}

type KafkaOutput struct {
	brokers           string
	topicSuffix       string
	Producer          WrappedProducer
	deliveryChannel   chan kafka.Event
	droppedEventCount int64
	eventSentCount    int64
	sync.RWMutex
}

type KafkaStatistics struct {
	DroppedEventCount int64 `json:"dropped_event_count"`
	EventSentCount    int64 `json:"event_sent_count"`
}

func NewKafkaOutputFromCfg( cfg map[interface{}] interface{}) (KafkaOutput, error) {
	ko := KafkaOutput{}

	log.Infof("Trying to create kafka output with plugin section: %s", cfg)


	var configMap map[interface{}]interface{} = make(map[interface{}] interface{})

	if configm, ok := cfg["producer"].(map[interface{}] interface{}); ok {
		configMap = configm
	}

	if topicsuffix, ok := cfg["topicSuffix"]; ok {
		if topicsuffix, ok := topicsuffix.(string); ok {
			ko.topicSuffix = topicsuffix
		} else {
			ko.brokers = ""
		}
	}


	kafkaConfigMap := kafka.ConfigMap{}

	for key, value := range configMap {
		ks := key.(string)
		switch value.(type) {
		case string:
			kafkaConfigMap[ks] = value.(string)
		case int:
			kafkaConfigMap[ks] = value.(int)
		case float32:
			kafkaConfigMap[ks] = value.(float32)
		case float64:
			kafkaConfigMap[ks] = value.(float64)
		case bool:
			kafkaConfigMap[ks] = value.(bool)
		default:
			kafkaConfigMap[ks] = fmt.Sprintf("%s", value)
		}
	}

	if brokers, ok := configMap["bootstrap.servers"]; ok {
		if brokers, ok := brokers.(string); ok {
			ko.brokers = brokers
		} else {
			ko.brokers = "localhost:9092"
		}
	}

	producer, err := kafka.NewProducer(&kafkaConfigMap)

	if err != nil {
		log.Infof("Failed to create producer: %s\n", err)
		return ko,err
	}

	log.Infof("Created Producer %v\n", producer)

	ko.Producer = producer

	ko.deliveryChannel = make(chan kafka.Event)
	return ko,nil
}

func (o *KafkaOutput) Go(messages <-chan string, errorChan chan<- error, controlchan <-chan os.Signal) error {
	stoppubchan := make(chan struct{}, 1)
	go func() {
		for {
			select {
			case message := <-messages:
				var parsedMsg map[string]interface{}
				json.Unmarshal([]byte(message), &parsedMsg)
				topic := parsedMsg["type"]
				if topicString, ok := topic.(string); ok {
					topicString = strings.Replace(topicString, "ingress.event.", "", -1)
					topicString += o.topicSuffix
					o.output(topicString, message)
				} else {
					log.Info("ERROR: Topic was not a string")
				}
			case <-stoppubchan:
				log.Info("stop request received ending publishing goroutine")
				return
			}
		}
	}()
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()

		for {
			select {
			case e := <-o.deliveryChannel:
				m := e.(*kafka.Message)
				if m.TopicPartition.Error != nil {
					log.Infof("Delivery failed: %v\n", m.TopicPartition.Error)
					atomic.AddInt64(&o.droppedEventCount, 1)
					errorChan <- m.TopicPartition.Error
				} else {
					log.Infof("Delivered message to topic %s [%d] at offset %v\n",
						*m.TopicPartition.Topic, m.TopicPartition.Partition, m.TopicPartition.Offset)
					atomic.AddInt64(&o.eventSentCount, 1)
				}
			case cmsg := <-controlchan:
				switch cmsg {
				case syscall.SIGTERM, syscall.SIGINT:
					// handle exit gracefully
					log.Info("Received SIGTERM. Exiting")
					stoppubchan <- struct{}{}
					return
				}
			}
		}
	}()

	return nil
}

func (o *KafkaOutput) Statistics() interface{} {
	o.RLock()
	defer o.RUnlock()
	return KafkaStatistics{DroppedEventCount: o.droppedEventCount, EventSentCount: o.eventSentCount}
}

func (o *KafkaOutput) String() string {
	o.RLock()
	defer o.RUnlock()

	return fmt.Sprintf("Brokers %s", o.brokers)
}

func (o *KafkaOutput) Key() string {
	o.RLock()
	defer o.RUnlock()
	return fmt.Sprintf("brokers:%s", o.brokers)
}

func (o *KafkaOutput) output(topic string, m string) {
	log.Infof("output got: %s topic %s message ", topic, m)
	o.Producer.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Value:          []byte(m),
	}, o.deliveryChannel)
	log.Infof("o.Producer.Produce returned")
}

func GetOutputHandler(cfg map[interface{}] interface{}) (output.OutputHandler, error) {
	ko, err := NewKafkaOutputFromCfg(cfg)
	return &ko, err
}
