package main

/**
* Copyright 2018 Carbon Black
*
*
CARBON BLACK 2018 - Zachary Estep - Using this code as the basis for a producer interface that is mockable
*/

import (
	"github.com/confluentinc/confluent-kafka-go/kafka"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/mock"
	"os"
	"path"
)

type MockedProducer struct {
	mock.Mock
	outfile *os.File
}

func NewMockedKafkaProducer(tempfiledir string) (mp *MockedProducer, err error) {
	outputFile, err := os.Create(path.Join(tempfiledir, "/kafkaoutput"))
	if err != nil {
		log.Errorf("Coudln't open httpoutput file %v", err)
		return mp, err
	}
	mockProducer := new(MockedProducer)
	mockProducer.outfile = outputFile
	return mockProducer, nil
}

func (mp *MockedProducer) Produce(msg *kafka.Message, deliveryChan chan kafka.Event) error {
	log.Infof("MockProducer::Producer called with %s %v", msg, deliveryChan)
	deliveryChan <- msg
	mp.outfile.Write(msg.Value)
	log.Info("MockProducer::Producer returning")
	return nil
}

func (mp *MockedProducer) String() string {
	return "MockedKafkaProducer"
}

func (mp *MockedProducer) Events() chan kafka.Event {
	return make(chan kafka.Event, 1000000)
}

func (mp *MockedProducer) ProduceChannel() chan *kafka.Message {
	return make(chan *kafka.Message, 1000000)
}

func (mp *MockedProducer) Close() {
	return
}

func (mp *MockedProducer) Flush(timeout int) int {
	return 0
}
func (mp *MockedProducer) Len() int {
	return 0
}

func (mp *MockedProducer) GetMetadata(topic *string, allTopics bool, timeoutMs int) (*kafka.Metadata, error) {
	return nil, nil
}

func (mp *MockedProducer) QueryWatermarkOffsets(topic string, partition int32, timeoutMs int) (low, high int64, err error) {
	return 0, 0, nil
}

func (mp *MockedProducer) OffsetsForTimes(times []kafka.TopicPartition, timeoutMs int) (offsets []kafka.TopicPartition, err error) {
	return make([]kafka.TopicPartition, 0), nil
}
