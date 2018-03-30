package main 

import (
	"bufio"
	"encoding/json"
	"github.com/Shopify/sarama"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"strings"
)

func NewProducer(brokers []string) (sarama.AsyncProducer, error) {
	kafkaConfig := sarama.NewConfig()
	kafkaConfig.Producer.Return.Successes = true
	sarama.MaxRequestSize = 1024 * 1024 * 1024
	return sarama.NewAsyncProducer(brokers, kafkaConfig)
}

func main() {
	args := os.Args
	if len(args) < 2 {
		log.Panic("usage: kafka_util broker-list file-path optional-topic-suffix")
	}
	files, _ := filepath.Glob(args[2])
	brokers := strings.Split(args[1], ",")
	topic_suffix := ""
	if len(args) == 3 {
		topic_suffix = args[3]
	}
	kafkaProducer, _ := NewProducer(brokers)
	for _, file_name := range files {
		file, err := os.Open(file_name)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			b := scanner.Bytes()
			var f interface{}
			json.Unmarshal(b, &f)
			m := f.(map[string]interface{})
            t, _ := m["type"].(string)
			topic := strings.Replace(t, "ingress.event", "", -1)
			topic = topic + topic_suffix
			kafkaProducer.Input() <- &sarama.ProducerMessage{
				Topic: topic,
				Key:   nil,
				Value: sarama.ByteEncoder(b),
			}
		}

		if err := scanner.Err(); err != nil {
			log.Fatal(err)
		}

	}

}
