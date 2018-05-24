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

type KafkaOutput struct {
	config            conf.Configuration
	brokers           string
	topicSuffix       string
	producer          *kafka.Producer
	deliveryChannel   chan kafka.Event
	droppedEventCount int64
	eventSentCount    int64
	sync.RWMutex
}

type KafkaStatistics struct {
	DroppedEventCount int64 `json:"dropped_event_count"`
	EventSentCount    int64 `json:"event_sent_count"`
}

func (o *KafkaOutput) Initialize(unused string, config conf.Configuration) error {
	o.Lock()
	defer o.Unlock()

	o.config = config

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
		default:
			kafkaConfigMap[key] = value.(string)
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

	producer, err := kafka.NewProducer(&kafkaConfigMap)

	if err != nil {
		log.Infof("Failed to create producer: %s\n", err)
		return err
	}

	log.Infof("Created Producer %v\n", producer)

	o.producer = producer

	o.deliveryChannel = make(chan kafka.Event)

	return nil
}

func (o *KafkaOutput) Go(messages <-chan string, errorChan chan<- error) error {
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()

		hup := make(chan os.Signal, 1)

		signal.Notify(hup, syscall.SIGHUP)

		defer signal.Stop(hup)

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
	o.producer.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Value:          []byte(m),
	}, o.deliveryChannel)
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
		kafkaOutput.Go(messages, errors)
	}()
	messages <- "{\"type\":\"Lol\"}"
	log.Infof("%v", <-errors)
}
