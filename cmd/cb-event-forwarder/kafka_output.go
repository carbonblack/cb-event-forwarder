package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Shopify/sarama"
	"github.com/rcrowley/go-metrics"
	log "github.com/sirupsen/logrus"
)

func NewTLSConfig(clientCertFile, clientKeyFile, caCertFile string) (*tls.Config, error) {
	tlsConfig := tls.Config{}
        
	// check that the client cert auth is in the config
	if clientCertFile != nil && clientKeyFile != nil {

		// Load client cert
		cert, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
		if err != nil {
			return &tlsConfig, err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// Load CA cert
	caCert, err := ioutil.ReadFile(caCertFile)
	if err != nil {
		return &tlsConfig, err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	tlsConfig.RootCAs = caCertPool

	tlsConfig.BuildNameToCertificate()
	tlsConfig.InsecureSkipVerify = !config.TLSVerify
	return &tlsConfig, err
}

type KafkaOutput struct {
	brokers           []string
	topicSuffix       string
	topic             *string
	producer          sarama.AsyncProducer
	droppedEventCount int64
	eventSentCount    int64
	EventSent         metrics.Meter
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

	o.brokers = strings.Split(*config.KafkaBrokers, ",")
	o.topicSuffix = config.KafkaTopicSuffix
	if len(config.KafkaTopic) > 0 {
		o.topic = &config.KafkaTopic
	}

	kafkaConfig := sarama.NewConfig()
	sarama.MaxRequestSize = config.KafkaMaxRequestSize
	kafkaConfig.Producer.Return.Successes = true

	o.EventSent = metrics.NewRegisteredMeter("output.kafka.events_sent", metrics.DefaultRegistry)
	o.DroppedEvent = metrics.NewRegisteredMeter("output.kafka.events_dropped", metrics.DefaultRegistry)

	if config.KafkaSSLCALocation != nil {
		kafkaConfig.Net.TLS.Enable = true
		if config.KafkaSSLKeyLocation != nil && config.KafkaSSLCertificateLocation != nil {
			var tlsConfig, err = NewTLSConfig(*config.KafkaSSLCertificateLocation, *config.KafkaSSLKeyLocation, *config.KafkaSSLCALocation)
		} else {
			var tlsConfig, err = NewTLSConfig(nil, nil, *config.KafkaSSLCALocation)
		}
		if err != nil {
			log.Fatalf("Error setting up tls for kafka %v", err)
		}
		kafkaConfig.Net.TLS.Config = tlsConfig
	}

	if config.KafkaCompressionType != nil {
		var compressionTypeString = *config.KafkaCompressionType
		var compresionCodec = sarama.CompressionNone
		switch compressionTypeString {
		case "gzip":
			compresionCodec = sarama.CompressionGZIP
		case "zstd":
			compresionCodec = sarama.CompressionZSTD
		case "snappy":
			compresionCodec = sarama.CompressionSnappy
		case "lz4":
			compresionCodec = sarama.CompressionLZ4
		default:
			compresionCodec = sarama.CompressionNone
		}
		if compresionCodec != sarama.CompressionNone {
			kafkaConfig.Producer.Compression = compresionCodec
		}
	}

	if len(config.KafkaUsername) > 0 && len(config.KafkaPassword) > 0 {
		kafkaConfig.Net.SASL.User = config.KafkaUsername
		kafkaConfig.Net.SASL.Password = config.KafkaPassword
		kafkaConfig.Net.SASL.Enable = true
	}

	producer, err := sarama.NewAsyncProducer(o.brokers, kafkaConfig)

	if err != nil {
		panic(err)
	}

	o.producer = producer

	return nil
}

func (o *KafkaOutput) Go(messages <-chan string, errorChan chan<- error, signals chan<- os.Signal) error {
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()

		defer func() {
			if err := o.producer.Close(); err != nil {
				log.Fatalln(err)
			}
		}()

		hup := make(chan os.Signal, 1)
		term := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		signal.Notify(term, syscall.SIGTERM)

		defer signal.Stop(hup)
		defer signal.Stop(term)

		for {
			select {
			case message := <-messages:
				if o.topic != nil {
					o.output(*o.topic, message)
				} else {
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
			case sigterm := <-term:
				log.Infof("Kafka output handling SIGTERM...")
				signals <- sigterm
				return
			}
		}

	}()

	go func() {
		for range o.producer.Successes() {
			atomic.AddInt64(&o.eventSentCount, 1)
			o.EventSent.Mark(1)
		}
	}()

	go func() {
		for err := range o.producer.Errors() {
			log.Info(err)
			atomic.AddInt64(&o.droppedEventCount, 1)
			o.DroppedEvent.Mark(1)
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
