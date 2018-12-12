package main

import (
	"encoding/json"
	"fmt"
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
	brokers           []string
	topicSuffix       string
	topic			  string
	producer          kafka.Producer
	droppedEventCount int64
	eventSentCount    int64

	sync.RWMutex
}

type KafkaStatistics struct {
	DroppedEventCount int64 `json:"dropped_event_count"`
	EventSentCount    int64 `json:"event_sent_count"`
}

func (o *KafkaOutput) Initialize(unused string) error {
	o.Lock()
	defer o.Unlock()

	o.brokers = strings.Split(*(config.KafkaBrokers), ",")
	o.topicSuffix = *(config.KafkaTopicSuffix)
	o.topic = *(config.KafkaTopic)
    p, err := kafka.NewProducer(&kafka.ConfigMap{"bootstrap.servers": *config.KafkaBrokers,
                                                    "security.protocol": *config.KafkaProtocol,
                                                    "sasl.mechanism": *config.KafkaMechanism,
                                                    "sasl.username": *config.KafkaUsername,
                                                    "sasl.password": *config.KafkaPassword})

	if err != nil {
		panic(err)
	}

	o.producer = *p

	return nil
}

func (o *KafkaOutput) Go(messages <-chan string, errorChan chan<- error) error {
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()
		defer o.producer.Close()

		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)

		defer signal.Stop(hup)

		for {
			select {
			case message := <-messages:
				var parsedMsg map[string]interface{}
				json.Unmarshal([]byte(message), &parsedMsg)
				if o.topic != nil {
					o.output(o.topic, message)
				} else {
					topic := parsedMsg["type"]
					if topicString, ok := topic.(string); ok {
						topicString = strings.Replace(topicString, "ingress.event.", "", -1)
						topicString += o.topicSuffix
						o.output(topicString, message)
					} else {
						log.Info("ERROR: Topic was not a string")
					}
				 }
			case e := <-o.producer.Events():
				m := e.(*kafka.Message)
				if m.TopicPartition.Error != nil {
					log.Debugf("Delivery failed: %v\n", m.TopicPartition.Error)
					atomic.AddInt64(&o.droppedEventCount, 1)
					errorChan <- m.TopicPartition.Error
				} else {
					log.Debugf("Delivered message to topic %s [%d] at offset %v\n",
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
	o.producer.ProduceChannel() <- &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Value:          []byte(m),
	}
}
