package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Shopify/sarama"
	log "github.com/sirupsen/logrus"
)

type KafkaOutput struct {
	brokers           []string
	topicSuffix       string
	producer          sarama.AsyncProducer
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

	o.brokers = strings.Split(*config.KafkaBrokers, ",")
	o.topicSuffix = config.KafkaTopicSuffix

	kafkaConfig := sarama.NewConfig()
	sarama.MaxRequestSize = config.KafkaMaxRequestSize
	kafkaConfig.Producer.Return.Successes = true

	producer, err := sarama.NewAsyncProducer(o.brokers, kafkaConfig)

	if err != nil {
		panic(err)
	}

	o.producer = producer

	return nil
}

func (o *KafkaOutput) Go(messages <-chan string, errorChan chan<- error) error {
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()

		defer func() {
			if err := o.producer.Close(); err != nil {
				log.Fatalln(err)
			}
		}()

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
			}
		}

	}()

	go func() {
		for range o.producer.Successes() {
			atomic.AddInt64(&o.eventSentCount, 1)
		}
	}()

	go func() {
		for err := range o.producer.Errors() {
			log.Info(err)
			atomic.AddInt64(&o.droppedEventCount, 1)
			errorChan <- err
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
	o.producer.Input() <- &sarama.ProducerMessage{
		Topic: topic,
		Key:   nil,
		Value: sarama.StringEncoder(m),
	}
}
