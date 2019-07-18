package main

import (
	"crypto/tls"
	"errors"
	log "github.com/sirupsen/logrus"
	"fmt"
	"github.com/streadway/amqp"
	"path"
	"os"
	"io/ioutil"
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
	//log.Infof("MOCK AMQP Publish - %s %s", exchange, key)

	for _, queue := range mock.Queues {
		if _, ok := queue.BoundExchanges[exchange]; ok {
			//log.Infof("amqp.Publishing types: %s ", msg.ContentType)
			queue.Deliveries <- amqp.Delivery{Exchange: exchange, RoutingKey: key, Body: msg.Body, ContentType: msg.ContentType}
		} /* else {
			log.Debugf("Not bound to %s", exchange)
		} */
	}
	return nil
}

func (mock *MockAMQPChannel) QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
	mock.Queues = append(mock.Queues, MockAMQPQueue{Deliveries: make(chan amqp.Delivery), Name: name, BoundExchanges: make(map[string][]string, 0)})
	/*log.Infof("Created a mock queue")
	for _, q := range mock.Queues {
		log.Infof("Mock.queue has %s", q.String())
	}*/
	return amqp.Queue{Name: name, Messages: 0, Consumers: 0}, nil
}

func (mock *MockAMQPChannel) QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error {

	//log.Infof("Trying to bind mock queue - %s %s %s", name, key, exchange)
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
	//log.Infof("Looking for %s", queue)
	for _, q := range mock.Queues {
		if q.Name == queue {
			//log.Infof("FOUND - Looking for %s , on queue %s", q.Name, queue)
			return q.Deliveries, nil
		}
	}
	//log.Infof("Did not find queue by name, %s", queue)
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
	for {
		for _, format := range []string{"zip"} {

			pathname := path.Join("test/raw_data", format)

			fp, err := os.Open(pathname)

			if err != nil {
				log.Errorf("Could not open %s", pathname)
				continue
			}

			infos, err := fp.Readdir(0)
			if err != nil {
				log.Errorf("Could not enumerate directory %s", pathname)
				continue
			}

			fp.Close()

			for _, fn := range infos {

				if fn.IsDir() {
					continue
				}

				filename := path.Join(pathname,fn.Name())

				fp, err := os.Open(filename)
				if err != nil {
					log.Errorf("Could not open %s for reading", filename)
					continue
				}

				b, err := ioutil.ReadAll(fp)
				if err != nil {
					log.Errorf("Could not read %s", filename)
					continue
				}

				fp.Close()

				exchange := "api.events"
				contentType := "application/json"
				if format == "json" {
					exchange = "api.events"
					contentType = "application/json"
				}
				if format == "zip" {
					exchange = "api.rawsensordata"
					contentType = "application/protobuf"
				}

				if err = mockChan.Publish(
					exchange, // publish to an exchange
					"", // routing to 0 or more queues
					false, // mandatory
					false, // immediate
					amqp.Publishing{
						Headers:         amqp.Table{},
						ContentType:     contentType,
						ContentEncoding: "",
						Body:            b,
						DeliveryMode:    amqp.Transient, // 1=non-persistent, 2=persistent
						Priority:        0, // 0-
					},
				); err != nil {
					log.Errorf("Failed to publish %s %s: %s", exchange, "", err)
				}
			}
		}
	}
}
