package rabbitmq

import (
	"crypto/tls"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

/*
 * AMQP bookkeeping
 */
/*
 ZE 2019 - Improved Consumer struct
*/
type Consumer struct {
	conn              AMQPConnection
	channel           AMQPChannel
	bindToRawExchange bool
	tag               string
	queueName         string
	amqpURI           string
	tlsCfg            *tls.Config
	routingKeys       []string
	dialer            AMQPDialer
	ConnectionErrors  chan *amqp.Error
}

type AMQPConnection interface {
	Close() error
	NotifyClose(receiver chan *amqp.Error) chan *amqp.Error
	Channel() (AMQPChannel, error)
}

type AMQPChannel interface {
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
	Cancel(consumer string, noWait bool) error
	QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	Publish(exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
	Close() error
}

type WrappedAMQPConnection struct {
	*amqp.Connection
}

type WrappedAMQPChannel struct {
	*amqp.Channel
}

func (wrappedcon WrappedAMQPConnection) Channel() (AMQPChannel, error) {
	channel, err := wrappedcon.Connection.Channel()
	return WrappedAMQPChannel{channel}, err
}

func NewConsumer(amqpURI, queueName, ctag string, bindToRawExchange bool,
	routingKeys []string, dialer AMQPDialer, tlsCfg * tls.Config) *Consumer {
	return NewConsumerWithTlsCfg(amqpURI, queueName, ctag, bindToRawExchange, routingKeys, dialer, tlsCfg)
}

func NewConsumerWithTlsCfg(amqpURI, queueName, ctag string, bindToRawExchange bool,
	routingKeys []string, dialer AMQPDialer, tlsCfg *tls.Config) *Consumer {
	c := &Consumer{
		conn:              nil,
		channel:           nil,
		tag:               ctag,
		bindToRawExchange: bindToRawExchange,
		routingKeys:       routingKeys,
		dialer:            dialer,
		amqpURI:           amqpURI,
		queueName:         queueName,
		ConnectionErrors:  make(chan *amqp.Error),
		tlsCfg:            tlsCfg}

	return c
}

func (c *Consumer) DialAMQP() error {
	var err error = nil
	if c.tlsCfg != nil {
		log.Debugf("Connecting to message bus at %s via TLS...", c.amqpURI)
		c.conn, err = c.dialer.DialTLS(c.amqpURI, c.tlsCfg)
		if err != nil {
			return err
		}
	} else {
		log.Debugf("Connecting to message bus at %s....", c.amqpURI)
		c.conn, err = c.dialer.Dial(c.amqpURI)
		if err != nil {
			return err
		}

	}
	return err
}

func (c *Consumer) Connect() (deliveries <-chan amqp.Delivery, err error) {

	err = c.DialAMQP()
	if err != nil {
		return deliveries, err
	}
	c.channel, err = c.conn.Channel()
	if err != nil {
		return deliveries, err
	}

	queue, err := c.channel.QueueDeclare(
		c.queueName,
		false, // durable,
		true,  // delete when unused
		false, // exclusive
		false, // nowait
		nil,   // arguments
	)
	if err != nil {
		return deliveries, err
	}

	if c.bindToRawExchange {
		err = c.channel.QueueBind(c.queueName, "", "api.rawsensordata", false, nil)
		if err != nil {
			return deliveries, err
		}
		log.Infof("Subscribed to bulk raw sensor event exchange on queue %s", c.queueName)
	}

	for _, key := range c.routingKeys {
		err = c.channel.QueueBind(c.queueName, key, "api.events", false, nil)
		if err != nil {
			return deliveries, err
		}
		log.Infof("Subscribed to %s on %s", key, c.queueName)
	}

	deliveries, err = c.channel.Consume(
		queue.Name,
		c.tag,
		true,  // automatic or manual acking
		false, // exclusive
		false, // noLocal
		false, // noWait
		nil,   // arguments
	)

	if err != nil {
		return deliveries, err
	}

	c.conn.NotifyClose(c.ConnectionErrors)

	return deliveries, nil
}

func (c *Consumer) Shutdown() error {
	defer close(c.ConnectionErrors)
	if err := c.channel.Cancel(c.tag, true); err != nil {
		return fmt.Errorf("Consumer cancel failed: %s", err)
	}

	c.channel.Close()

	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("AMQP connection close error: %s", err)
	}

	log.Infof("AMQP shutdown OK")

	return nil
}

type AMQPDialer interface {
	Dial(string) (AMQPConnection, error)
	DialTLS(string, *tls.Config) (AMQPConnection, error)
}

type StreadwayAMQPDialer struct {
}

func (sdial StreadwayAMQPDialer) Dial(s string) (AMQPConnection, error) {
	conn, err := amqp.Dial(s)
	return WrappedAMQPConnection{conn}, err
}

func (sdial StreadwayAMQPDialer) DialTLS(s string, tlscfg *tls.Config) (AMQPConnection, error) {
	conn, err := amqp.DialTLS(s, tlscfg)
	return WrappedAMQPConnection{conn}, err
}
