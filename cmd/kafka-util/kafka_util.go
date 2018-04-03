package main

import (
	"bufio"
	"encoding/json"
	"github.com/Shopify/sarama"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func NewProducer(brokers []string) (sarama.AsyncProducer, error) {
	kafkaConfig := sarama.NewConfig()
	sarama.MaxRequestSize = 1000000
	kafkaConfig.Producer.Return.Successes = true
	kafkaConfig.Producer.RequiredAcks = sarama.WaitForLocal       // Only wait for the leader to ack
	kafkaConfig.Producer.Compression = sarama.CompressionSnappy   // Compress messages
	kafkaConfig.Producer.Flush.Frequency = 500 * time.Millisecond // Flush batches every 500ms
	return sarama.NewAsyncProducer(brokers, kafkaConfig)
}

func main() {
	args := os.Args
	if len(args) < 3 {
		log.Fatal("usage: kafka_util broker-list \"file-path\" optional-topic-suffix")
	}
	files, err := filepath.Glob(args[2])
	if err != nil {
		log.Fatalf("Filepath error %v", err)
	}
	log.Debugf("Files: %s", files)
	brokers := strings.Split(args[1], ",")
	log.Debugf("Brokers: %s", brokers)
	topic_suffix := ""
	if len(args) == 4 {
		topic_suffix = args[3]
	}
	log.Debugf("Topic_Suffix: %s", topic_suffix)
	kafkaProducer, err := NewProducer(brokers)

	if err != nil {
		log.Fatalf("Error setting up kafka producer: %v", err)
	} else {
		log.Debugf("Setup kafkaproducer Ok")
		defer kafkaProducer.Close()
	}
	for _, file_name := range files {
		log.Debugf("trying file %s", file_name)
		file, err := os.Open(file_name)
		if err != nil {
			log.Fatal(err)
		} else {
			log.Debugf("Opened file %s", file_name)
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
					log.Debugf("Failed to produce message", err)
				case <-kafkaProducer.Successes():
					log.Debugf("Produced message!")
				}
			} else {
				log.Debugf("Error unmarshalling json: %v", err)
			}

		}
		log.Debugf("Done scanning file ", file_name)
	}
}
