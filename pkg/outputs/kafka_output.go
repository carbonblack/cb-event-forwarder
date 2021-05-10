package outputs

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Shopify/sarama"
	. "github.com/carbonblack/cb-event-forwarder/pkg/config"
	"github.com/rcrowley/go-metrics"
	log "github.com/sirupsen/logrus"
)

func NewTLSConfig(config *Configuration, clientCertFile, clientKeyFile, caCertFile string) (*tls.Config, error) {
	tlsConfig := tls.Config{}

	// Load client cert
	cert, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
	if err != nil {
		return &tlsConfig, err
	}
	tlsConfig.Certificates = []tls.Certificate{cert}

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
	Config            *Configuration
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

func NewKafkaOutputFromConfig(cfg *Configuration) *KafkaOutput {
	return &KafkaOutput{Config: cfg}
}

type KafkaStatistics struct {
	DroppedEventCount int64 `json:"dropped_event_count"`
	EventSentCount    int64 `json:"event_sent_count"`
}

func (o *KafkaOutput) Initialize(unused string) (err error) {
	o.Lock()
	defer o.Unlock()

	o.brokers = strings.Split(*o.Config.KafkaBrokers, ",")
	o.topicSuffix = o.Config.KafkaTopicSuffix
	if len(o.Config.KafkaTopic) > 0 {
		o.topic = &o.Config.KafkaTopic
	}

	kafkaConfig := sarama.NewConfig()
	sarama.MaxRequestSize = o.Config.KafkaMaxRequestSize
	kafkaConfig.Producer.Return.Successes = true

	o.EventSent = metrics.NewRegisteredMeter("output.kafka.events_sent", metrics.DefaultRegistry)
	o.DroppedEvent = metrics.NewRegisteredMeter("output.kafka.events_dropped", metrics.DefaultRegistry)

	if o.Config.KafkaSSLKeyLocation != nil && o.Config.KafkaSSLCertificateLocation != nil && o.Config.KafkaSSLCALocation != nil {
		kafkaConfig.Net.TLS.Enable = true
		var tlsConfig, err = NewTLSConfig(o.Config, *o.Config.KafkaSSLCertificateLocation, *o.Config.KafkaSSLKeyLocation, *o.Config.KafkaSSLCALocation)
		if err != nil {
			err = fmt.Errorf("Error setting up tls for kafka %v", err)
			return err
		}
		kafkaConfig.Net.TLS.Config = tlsConfig
	}

	if o.Config.KafkaCompressionType != nil {
		var compressionTypeString = *o.Config.KafkaCompressionType
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

	if len(o.Config.KafkaUsername) > 0 && len(o.Config.KafkaPassword) > 0 {
		kafkaConfig.Net.SASL.User = o.Config.KafkaUsername
		kafkaConfig.Net.SASL.Password = o.Config.KafkaPassword
		kafkaConfig.Net.SASL.Enable = true
	}

	producer, err := sarama.NewAsyncProducer(o.brokers, kafkaConfig)

	o.producer = producer

	return err
}

func (o *KafkaOutput) Go(messages <-chan string, signals <-chan os.Signal, exitCond *sync.Cond) error {
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer exitCond.Signal()

		defer refreshTicker.Stop()

		defer func() {
			if err := o.producer.Close(); err != nil {
				log.Fatalln(err)
			}
		}()

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
						topicString = strings.ReplaceAll(topicString, "ingress.event.", "")
						topicString += o.topicSuffix

						o.output(topicString, message)
					} else {
						log.Info("ERROR: Topic was not a string")
					}
				}
			case signal := <-signals:
				switch signal {
				case syscall.SIGTERM, syscall.SIGINT:
					log.Infof("Kafka output handling SIGTERM...")
					return
				}
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
			log.Errorf("Dropped event due to %s", err)
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
