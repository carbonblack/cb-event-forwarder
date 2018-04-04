package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"github.com/Shopify/sarama"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var requestMaxSize = flag.Int("MaxRequestSize", 1000000, "sarama.MaxRequestSize")
var filePath = flag.String("FilePath", "", "Path to files")
var brokerList = flag.String("BrokerList", "", "Comma seperated list of kafka-broker:ip pairs")
var topicSuffix = flag.String("topicSuffix", "", "Optional topic suffix to use")

func NewProducer(brokers []string) (sarama.AsyncProducer, error) {
	kafkaConfig := sarama.NewConfig()
	sarama.MaxRequestSize = int32(*requestMaxSize)
	kafkaConfig.Producer.Return.Successes = true
	kafkaConfig.Producer.RequiredAcks = sarama.WaitForLocal       // Only wait for the leader to ack
	kafkaConfig.Producer.Flush.Frequency = 500 * time.Millisecond // Flush batches every 500ms
	return sarama.NewAsyncProducer(brokers, kafkaConfig)
}

func main() {
    flag.Parse()
	if *filePath == "" || *brokerList == "" {
		log.Fatal("Usage: -BrokerList localhost:9092,localhost:9093 -FilePath \"/path/to/event_bridge_output.json\" [-topicSuffix suffix -requestMaxSize 9000]")
	}
	files, err := filepath.Glob(*filePath)
	if err != nil {
		log.Fatalf("Filepath error %v", err)
	}
	log.Infof("Files: %s", files)
	brokers := strings.Split(*brokerList, ",")
	log.Infof("Brokers: %s", brokers)
	topic_suffix := *topicSuffix
	log.Infof("Topic_Suffix: %s", topic_suffix)
	kafkaProducer, err := NewProducer(brokers)

	if err != nil {
		log.Fatalf("Error setting up kafka producer: %v", err)
	} else {
		log.Infof("Setup kafkaproducer Ok")
		defer kafkaProducer.Close()
	}
	for _, file_name := range files {
		log.Infof("trying file %s", file_name)
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
				select {
				case kafkaProducer.Input() <- &sarama.ProducerMessage{Topic: topic, Key: nil, Value: sarama.ByteEncoder(b)}:
				case err := <-kafkaProducer.Errors():
					log.Infof("Failed to produce message", err)
				case <-kafkaProducer.Successes():
					log.Infof("Produced message!")
				}
			} else {
				log.Infof("Error unmarshalling json: %v", err)
			}

		}
		log.Infof("Done scanning file ", file_name)
	}
}
