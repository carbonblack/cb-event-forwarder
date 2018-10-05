package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/confluentinc/confluent-kafka-go/kafka"
	log "github.com/sirupsen/logrus"
	"os"
	"strings"
)

var requestMaxSize = flag.Int("MaxRequestSize", 1000000, "sarama.MaxRequestSize")
var brokerList = flag.String("BrokerList", "", "Comma seperated list of kafka-broker:ip pairs (required)")
var topicSuffix = flag.String("TopicSuffix", "", "Optional topic suffix to use")

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s -BrokerList localhost:9092,localhost:9093 [-TopicSuffix suffix -requestMaxSize 9000] files\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	if *brokerList == "" {
		fmt.Fprintln(os.Stderr, "Error: -BrokerList option is required\n")
		flag.Usage()
		os.Exit(1)
	}

	log.Infof("Kafka Utility:")
	files := flag.Args()
	log.Infof("Files: %s", files)
	brokers := strings.Split(*brokerList, ",")
	log.Infof("Brokers: %s", brokers)
	topic_suffix := *topicSuffix
	log.Infof("Topic_Suffix: %s", topic_suffix)
	kafkaProducer, err := kafka.NewProducer(&kafka.ConfigMap{"bootstrap.servers": brokers})
	deliveryChannel := make(chan kafka.Event)
	if err != nil {
		log.Fatalf("Error setting up kafka producer: %v", err)
	} else {
		log.Infof("Setup Ok")
		defer kafkaProducer.Close()
	}
	for _, file_name := range files {
		log.Debugf("trying file %s", file_name)
		file, err := os.Open(file_name)
		if err != nil {
			log.Fatal(err)
		} else {
			log.Infof("Opened file %s", file_name)
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Split(bufio.ScanLines)
		for scanner.Scan() {
			b := scanner.Bytes()
			var f interface{}
			err := json.Unmarshal(b, &f)
			if err == nil {

				m := f.(map[string]interface{})
				t, _ := m["type"].(string)
				topic := strings.Replace(t, "ingress.event.", "", -1)
				topic = topic + topic_suffix
				kafkaProducer.Produce(&kafka.Message{
					TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
					Value:          []byte(t),
				}, deliveryChannel)
				select {
				case e := <-deliveryChannel:
					m := e.(*kafka.Message)
					if m.TopicPartition.Error != nil {
						log.Infof("Delivery failed: %v\n", m.TopicPartition.Error)
						log.Infof("Error %v", m.TopicPartition.Error)

					} else {
						log.Infof("Delivered message to topic %s [%d] at offset %v\n",
							*m.TopicPartition.Topic, m.TopicPartition.Partition, m.TopicPartition.Offset)
					}
					log.Debugf("Produced message!")
				}
			} else {
				log.Infof("Error unmarshalling json: %v", err)
			}

		}
		log.Infof("Done processing file ", file_name)
	}
}
