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
<<<<<<< HEAD
	producer          kafka.Producer
	deliveryChannel   chan kafka.Event
=======
	ProduceChannel chan *kafka.Message
	ProduceStop chan bool
	Events	chan kafka.Event
	producers         [] * kafka.Producer
>>>>>>> 326bf51... updates
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
<<<<<<< HEAD
	p, err := kafka.NewProducer(&kafka.ConfigMap{"bootstrap.servers": *config.KafkaBrokers})

	if err != nil {
		panic(err)
=======
	o.Events = make(chan kafka.Event)
	o.ProduceChannel = make(chan *kafka.Message)
	o.ProduceStop = make(chan bool)
	o.producers = make([] *kafka.Producer, len(o.brokers))
	pi := 0
	for pi < len(o.brokers) {
		p, err := kafka.NewProducer(&kafka.ConfigMap{"bootstrap.servers": *config.KafkaBrokers})
		if err != nil {
			panic(err)
		}
		o.producers[pi] = p
		pi = pi + 1
>>>>>>> 326bf51... updates
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
				topic := parsedMsg["type"]
				if topicString, ok := topic.(string); ok {
					topicString = strings.Replace(topicString, "ingress.event.", "", -1)
					topicString += o.topicSuffix

					o.output(topicString, message)
				} else {
					log.Info("ERROR: Topic was not a string")
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
