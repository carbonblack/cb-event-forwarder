package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

type MockAMQPConnection struct {
	AMQPURL  string
	AMQPCHAN *MockAMQPChannel
}

func (mock MockAMQPConnection) Close() error {
	return nil
}

func (mock MockAMQPConnection) NotifyClose(receiver chan *amqp.Error) chan *amqp.Error {
	return receiver
}

func (mock *MockAMQPConnection) Channel() (AMQPChannel, error) {
	if mock.AMQPCHAN == nil {
		mock.AMQPCHAN = &MockAMQPChannel{}
	}
	return mock.AMQPCHAN, nil
}

type MockAMQPQueue struct {
	Name           string
	Deliveries     chan amqp.Delivery
	BoundExchanges map[string][]string
}

func (mock *MockAMQPQueue) String() string {
	return fmt.Sprintf("%s has exchanges %s", mock.Name, mock.BoundExchanges)
}

type MockAMQPChannel struct {
	Queues []MockAMQPQueue
}

func (mock *MockAMQPChannel) Publish(exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {

	for _, queue := range mock.Queues {
		if _, ok := queue.BoundExchanges[exchange]; ok {
			log.Infof("amqp.Publishing types: %s ", msg.ContentType)
			queue.Deliveries <- amqp.Delivery{Exchange: exchange, RoutingKey: key, Body: msg.Body, ContentType: msg.ContentType}
		} else {
			log.Debugf("Not bound to %s", exchange)
		}
	}
	return nil
}

func (mock *MockAMQPChannel) QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
	mock.Queues = append(mock.Queues, MockAMQPQueue{Deliveries: make(chan amqp.Delivery), Name: name, BoundExchanges: make(map[string][]string, 0)})
	return amqp.Queue{Name: name, Messages: 0, Consumers: 0}, nil
}

func (mock *MockAMQPChannel) QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error {

	for i, queue := range mock.Queues {
		if queue.Name == name {
			existingKeys, ok := queue.BoundExchanges[exchange]
			if ok {
				mock.Queues[i].BoundExchanges[exchange] = append(existingKeys, key)
			} else {
				mock.Queues[i].BoundExchanges[exchange] = []string{key}
			}
		}
	}
	return nil
}

func (mock MockAMQPChannel) Cancel(consumer string, noWait bool) error {
	return nil
}

func (mock MockAMQPChannel) Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
	for _, q := range mock.Queues {
		if q.Name == queue {
			return q.Deliveries, nil
		}
	}
	return nil, errors.New("Couldn't find queue by name")
}

type MockAMQPDialer struct {
	Connection MockAMQPConnection
}

func (mdial MockAMQPDialer) Dial(s string) (AMQPConnection, error) {
	return &mdial.Connection, nil
}

func (mdial MockAMQPDialer) DialTLS(s string, tlscfg *tls.Config) (AMQPConnection, error) {
	return &mdial.Connection, nil
}

func NewMockAMQPDialer() MockAMQPDialer {
	return MockAMQPDialer{Connection: MockAMQPConnection{AMQPURL: "amqp://cb:lol@localhost:5672"}}
}

func RunCannedData(mockChan AMQPChannel) {

	filename := "test/stress_rabbit/zipbundles/bundleone"

	fp, err := os.Open(filename)

	b, err := ioutil.ReadAll(fp)
	if err != nil {
		log.Fatalf("Could not read %s", filename)
	}

	fp.Close()

	for {

		exchange := "api.rawsensordata"
		contentType := "application/protobuf"

		if err = mockChan.Publish(
			exchange, // publish to an exchange
			"",       // routing to 0 or more queues
			false,    // mandatory
			false,    // immediate
			amqp.Publishing{
				Headers:         amqp.Table{},
				ContentType:     contentType,
				ContentEncoding: "",
				Body:            b,
				DeliveryMode:    amqp.Transient, // 1=non-persistent, 2=persistent
				Priority:        0,              // 0-
			},
		); err != nil {
			log.Errorf("Failed to publish %s %s: %s", exchange, "", err)
		}

	}
}
