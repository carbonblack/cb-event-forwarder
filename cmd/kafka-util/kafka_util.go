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
	//sarama.MaxRequestSize = 1024 * 1024 * 1024
	return sarama.NewAsyncProducer(brokers, kafkaConfig)
}

func main() {
	args := os.Args
	if len(args) < 3 {
		log.Panic("usage: kafka_util broker-list file-path optional-topic-suffix")
	}
	files, err := filepath.Glob(args[2])
	if err != nil {
		log.Panicf("Filepath error %v",err)
	}
	log.Infof("Files: %s",files)
	brokers := strings.Split(args[1], ",")
	log.Infof("Brokers: %s",brokers)
	topic_suffix := ""
	if len(args) == 4 {
		topic_suffix = args[3]
	}
	log.Infof("Topic_Suffix: %s",topic_suffix)
	kafkaProducer, err := NewProducer(brokers)
	defer kafkaProducer.Close()

	if err != nil {
		log.Panicf("Error setting up kafka producer: %v",err)
	} else {
		log.Infof("Setup kafkaproducer Ok")
	}
	returns := make(chan bool)
	for _, file_name := range files {
		go func() {
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
					log.Infof("JSON: %s", f)
					m := f.(map[string]interface{})
					t, _ := m["type"].(string)
					topic := strings.Replace(t, "ingress.event.", "", -1)
					topic = topic + topic_suffix
					kafkaProducer.Input() <- &sarama.ProducerMessage{
						Topic: topic,
						Key:   nil,
						Value: sarama.ByteEncoder(b),
					}
					log.Infof("Wrote message ok")
				} else {
					log.Infof("Error unmarshalling json: %v", err)
				}

			}
			log.Infof("Done scanning file")
			returns <- true
		}()
	}
	count := 0
	for _ = range returns {
		count = count + 1
		if count == len(files) {
			return
		}
	}
}
