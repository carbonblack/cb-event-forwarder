package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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
	log "github.com/sirupsen/logrus"
)

type KafkaOutput struct {
	brokers           []string
	topicSuffix       string
	topic             *string
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
	if len(config.KafkaTopic) > 0 {
		o.topic = &config.KafkaTopic
	}

	kafkaConfig := sarama.NewConfig()
	sarama.MaxRequestSize = config.KafkaMaxRequestSize
	kafkaConfig.Producer.Return.Successes = true

	var useTLS = false
	var tlsConfig = tls.Config{}
	if config.KafkaSSLKeyLocation != nil && config.KafkaSSLCertificateLocation != nil {
		useTLS = true
		var sslKeyPair, err = tls.LoadX509KeyPair(*config.KafkaSSLKeyLocation, *config.KafkaSSLCertificateLocation)
		if err != nil {
			log.Fatalf("Could not load x509 pair for kafka client")
		}
		tlsConfig.Certificates = make([]tls.Certificate, 1)
		tlsConfig.Certificates[0] = sslKeyPair
	}
	if config.KafkaSSLCALocation != nil {
		useTLS = true
		var certPool, err = x509.SystemCertPool()
		if err != nil {
			log.Fatalf("Could not create new cet pool for kafka client")
		}
		cf, e := ioutil.ReadFile(*config.KafkaSSLCALocation)
		if e != nil {
			log.Fatalf("Could not open ca cert file for kafka client")
		}
		cpb, _ := pem.Decode(cf)
		crt, e := x509.ParseCertificate(cpb.Bytes)
		if e != nil {
			log.Fatalf("Could not parse ca cert for kafka client")
		}
		certPool.AddCert(crt)
		tlsConfig.RootCAs = certPool
	}

	if useTLS {
		kafkaConfig.Net.TLS.Enable = true
		kafkaConfig.Net.TLS.Config = &tlsConfig
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
