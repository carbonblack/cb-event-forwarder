package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"io/ioutil"
)

/*
 * AMQP bookkeeping
 */

func NewConsumer(amqpURI, queueName, ctag string, bindToRawExchange bool,
	routingKeys []string) (*Consumer, <-chan amqp.Delivery, error) {
	c := &Consumer{
		conn:    nil,
		channel: nil,
		tag:     ctag,
	}

	var err error

	if config.AMQPTLSEnabled == true {
		log.Info("Connecting to message bus via TLS...")

		cfg := new(tls.Config)

		caCert, err := ioutil.ReadFile(config.AMQPTLSCACert)
		if err != nil {
			log.Fatal(err)
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		cfg.RootCAs = caCertPool

		cert, err := tls.LoadX509KeyPair(config.AMQPTLSClientCert, config.AMQPTLSClientKey)
		if err != nil {
			log.Fatal(err)
		}
		cfg.Certificates = []tls.Certificate{cert}
		cfg.InsecureSkipVerify = true

		c.conn, err = amqp.DialTLS(amqpURI, cfg)

		if err != nil {
			return nil, nil, fmt.Errorf("Dial: %s", err)
		}
	} else {
		log.Info("Connecting to message bus...")
		c.conn, err = amqp.Dial(amqpURI)

		if err != nil {
			return nil, nil, fmt.Errorf("Dial: %s", err)
		}
	}

	c.channel, err = c.conn.Channel()
	if err != nil {
		return nil, nil, fmt.Errorf("Channel: %s", err)
	}

	durable := config.AMQPDurableQueues
	queue, err := c.channel.QueueDeclare(
		queueName,
		durable, // durable,
		true,    // delete when unused
		false,   // exclusive
		false,   // nowait
		nil,     // arguments
	)
	if err != nil {
		return nil, nil, fmt.Errorf("Queue declare: %s", err)
	}

	if bindToRawExchange {
		err = c.channel.QueueBind(queueName, "", "api.rawsensordata", false, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("QueueBind: %s", err)
		}
		log.Info("Subscribed to bulk raw sensor event exchange")
	}

	for _, key := range routingKeys {
		err = c.channel.QueueBind(queueName, key, "api.events", false, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("QueueBind: %s", err)
		}
		log.Infof("Subscribed to %s", key)
	}

	deliveries, err := c.channel.Consume(
		queue.Name,
		c.tag,
		config.AMQPAutomaticAcking, // automatic or manual acking
		false, // exclusive
		false, // noLocal
		false, // noWait
		nil,   // arguments
	)

	if err != nil {
		return nil, nil, fmt.Errorf("Queue consume: %s", err)
	}

	return c, deliveries, nil
}

func (c *Consumer) Shutdown() error {
	if err := c.channel.Cancel(c.tag, true); err != nil {
		return fmt.Errorf("Consumer cancel failed: %s", err)
	}

	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("AMQP connection close error: %s", err)
	}

	defer log.Infof("AMQP shutdown OK")

	return nil
}
