package main

import (
	"encoding/json"
	"fmt"
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"github.com/carbonblack/cb-event-forwarder/internal/output"
	"github.com/confluentinc/confluent-kafka-go/kafka"
	log "github.com/sirupsen/logrus"
	"os"
	"os/signal"
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

func (o *KafkaOutput) Initialize(unused string, config *conf.Configuration) error {
	o.Lock()
	defer o.Unlock()

	var configMap map[string]interface{}

	configMap, err := config.GetMap("plugin", "kafkaconfig")
	if err != nil {
		log.Info("Error getting pluginconfig")
	}

	log.Infof("Trying to create kafka output with plugin section: %s", configMap)

	kafkaConfigMap := kafka.ConfigMap{}

	for key, value := range configMap {
		switch value.(type) {
		case string:
			kafkaConfigMap[key] = value.(string)
		case int:
			kafkaConfigMap[key] = value.(int)
		case float32:
			kafkaConfigMap[key] = value.(float32)
		case float64:
			kafkaConfigMap[key] = value.(float64)
		case bool:
			kafkaConfigMap[key] = value.(bool)
		default:
			kafkaConfigMap[key] = fmt.Sprintf("%s", value)
		}
	}

	if brokers, ok := configMap["bootstrap.servers"]; ok {
		if brokers, ok := brokers.(string); ok {
			o.brokers = brokers
		} else {
			o.brokers = "localhost:9092"
		}
	}

	topicSuffix, err := config.GetString("plugin", "topicsuffix")

	if err == nil {
		o.topicSuffix = topicSuffix
	} else {
		o.topicSuffix = ""
	}

	if o.Producer == nil {
		producer, err := kafka.NewProducer(&kafkaConfigMap)

		if err != nil {
			log.Infof("Failed to create producer: %s\n", err)
			return err
		}

		log.Infof("Created Producer %v\n", producer)

		o.Producer = producer
	}

	o.deliveryChannel = make(chan kafka.Event)
	log.Info("Done init")
	return nil
}

func (o *KafkaOutput) Go(messages <-chan string, errorChan chan<- error, stopchan <-chan struct{}) error {
	stoppubchan := make(chan struct{})
	go func() {
		for {
			select {
			case message := <-messages:
				log.Info("got message in plugin")
				var parsedMsg map[string]interface{}
				json.Unmarshal([]byte(message), &parsedMsg)
				topic := parsedMsg["type"]
				if topicString, ok := topic.(string); ok {
					topicString = strings.Replace(topicString, "ingress.event.", "", -1)
					topicString += o.topicSuffix
					log.Info("sending message to output")
					o.output(topicString, message)
				} else {
					log.Info("ERROR: Topic was not a string")
				}
			case <-stoppubchan:
				log.Info("stop request recv'd ending publishing goroutine")
				return
			}
		}
	}()
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()

		hup := make(chan os.Signal, 1)

		signal.Notify(hup, syscall.SIGHUP)

		defer signal.Stop(hup)

		term := make(chan os.Signal, 1)
		signal.Notify(term, syscall.SIGTERM)
		signal.Notify(term, syscall.SIGINT)

		defer signal.Stop(term)

		for {
			select {
			case e := <-o.deliveryChannel:
				log.Info("got delivery message in plugin")
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
			case <-stopchan:
				log.Infof("Plugin recvd stop request - exiting gracefully immediately")
				stoppubchan <- struct{}{}
				return
			case <-term:
				log.Info("Got terminate/interupt signal - exiting gracefully")
				stoppubchan <- struct{}{}
				return
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

func GetOutputHandler() output.OutputHandler {
	return &KafkaOutput{}
}

func main() {
	messages := make(chan string)
	errors := make(chan error)
	var kafkaOutput output.OutputHandler = &KafkaOutput{}
	c, _ := conf.ParseConfig(os.Args[1])
	kafkaOutput.Initialize("", c)
	go func() {
		kafkaOutput.Go(messages, errors, make(chan struct{}))
	}()
	messages <- "{\"type\":\"Lol\"}"
	log.Infof("%v", <-errors)
}
