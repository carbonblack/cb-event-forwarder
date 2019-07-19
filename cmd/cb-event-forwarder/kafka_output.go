package main

import (
	"encoding/json"
	"fmt"
	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/rcrowley/go-metrics"
	log "github.com/sirupsen/logrus"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
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
	brokers           []string
	topicSuffix       string
	topic             string
	producers         []WrappedProducer
	droppedEventCount int64
	eventSentCount    int64
	EventSent         metrics.Meter
	EventSentBytes    metrics.Meter
	DroppedEvent      metrics.Meter
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
	o.topicSuffix = config.KafkaTopicSuffix
	o.topic = config.KafkaTopic
	o.producers = make([]WrappedProducer, len(o.brokers))

	o.EventSent = metrics.NewRegisteredMeter("output.kafka.events_sent", metrics.DefaultRegistry)
	o.DroppedEvent = metrics.NewRegisteredMeter("output.kafka.events_dropped", metrics.DefaultRegistry)
	o.EventSentBytes = metrics.NewRegisteredMeter("output.kafka.data_sent", metrics.DefaultRegistry)

	var kafkaConfig kafka.ConfigMap = nil
	//PLAINTEXT, SSL, SASL_PLAINTEXT, SASL_SSL
	if config.KafkaProducerProps == nil {
		switch config.KafkaProtocol {
		case "PLAINTEXT":
			kafkaConfig = kafka.ConfigMap{"bootstrap.servers": *config.KafkaBrokers}
		case "SASL":
			kafkaConfig = kafka.ConfigMap{"bootstrap.servers": *config.KafkaBrokers,
				"security.protocol": config.KafkaProtocol,
				"sasl.mechanism":    config.KafkaMechanism,
				"sasl.username":     config.KafkaUsername,
				"sasl.password":     config.KafkaPassword}
		case "SSL":
			kafkaConfig = kafka.ConfigMap{"bootstrap.servers": *config.KafkaBrokers,
				"security.protocol": config.KafkaProtocol}

			if config.KafkaSSLCertificateLocation != nil {
				kafkaConfig["ssl_certificate_location"] = config.KafkaSSLCertificateLocation
			}

			if config.KafkaSSLKeyLocation != nil && config.KafkaSSLKeyPassword != nil {
				kafkaConfig["ssl_key_location"] = config.KafkaSSLKeyLocation
				kafkaConfig["ssl_Key_password"] = config.KafkaSSLKeyPassword
			}

			if config.KafkaSSLEnabledProtocols != nil {
				kafkaConfig["ssl.enabled.protocols"] = config.KafkaSSLEnabledProtocols
			}

			if config.KafkaSSLCALocation != nil {
				kafkaConfig["ssl.ca.location"] = config.KafkaSSLCALocation
			}
		default:
			kafkaConfig = kafka.ConfigMap{"bootstrap.servers": *config.KafkaBrokers}
		}

		if config.KafkaCompressionType != nil {
			kafkaConfig["compression.type"] = *config.KafkaCompressionType
		}
	} else {
		kafkaConfig = config.KafkaProducerProps
	}

	for index, _ := range o.brokers {
		if config.DryRun {
			p, err := NewMockedKafkaProducer(".")
			if err != nil {
				panic(err)
			}
			o.producers[index] = p
		} else {
			p, err := kafka.NewProducer(&kafkaConfig)
			if err != nil {
				panic(err)
			}
			o.producers[index] = p
		}
	}

	return nil
}

func (o *KafkaOutput) Go(messages <-chan string, errorChan chan<- error) error {

	joinEventsChan := make(chan (kafka.Event), 100000)
	sigs := make(chan os.Signal, 1)
	stopProdChans := make([]chan struct{}, len(o.producers))

	signal.Notify(sigs, syscall.SIGHUP)
	signal.Notify(sigs, syscall.SIGTERM)
	signal.Notify(sigs, syscall.SIGINT)

	defer signal.Stop(sigs)

	for workernum, producer := range o.producers {
		stopProdChans[workernum] = make(chan struct{}, 1)
		go func(workernum int32, producer WrappedProducer, stopProdChan <-chan struct{}) {

			defer producer.Close()

			partition := kafka.PartitionAny
			shouldStop := false
			if (len(o.producers)) > 0 {
				partition = workernum
			}

			for {
				select {
				case message := <-messages:
					var topic string = config.KafkaTopic
					if topic == "" {
						var parsedMsg map[string]interface{}
						json.Unmarshal([]byte(message), &parsedMsg)
						topicRaw := parsedMsg["type"]
						if topicString, ok := topicRaw.(string); ok {
							topicString = strings.Replace(topicString, "ingress.event.", "", -1)
							topicString += o.topicSuffix
							topic = topicString
						} else {
							log.Info("ERROR: Topic was not a string")
						}
					}
					partition := kafka.TopicPartition{Topic: &topic, Partition: partition}
					output(message, o.producers[workernum], partition)
					o.EventSent.Mark(1)
					o.EventSentBytes.Mark(int64(len(message)))
				case <-stopProdChan:
					shouldStop = true
				case e := <-producer.Events():
					joinEventsChan <- e
				default:
					if shouldStop {
						return
					}
				}
			}
		}(int32(workernum), producer, stopProdChans[workernum])
	}
	go func() {
		for {
			select {
			case e := <-joinEventsChan:
				m := e.(*kafka.Message)
				if m.TopicPartition.Error != nil {
					o.DroppedEvent.Mark(1)
					errorChan <- m.TopicPartition.Error
				}
			case sig := <-sigs:
				switch sig {
				case syscall.SIGTERM, syscall.SIGINT:
					for _, stopChan := range stopProdChans {
						stopChan <- struct{}{}
					}
					return
				default:
					log.Debugf("Signal was %s", sig)
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

func output(m string, producer WrappedProducer, partition kafka.TopicPartition) {
	kafkamsg := &kafka.Message{
		TopicPartition: partition,
		Value:          []byte(m),
	}
	var err error = producer.Produce(kafkamsg, nil)

	for err != nil {
		log.Errorf("ERROR PRODUCING TO KAFKA %v", err)
		producer.Flush(1)
		err = producer.Produce(kafkamsg, nil)
	}

}
