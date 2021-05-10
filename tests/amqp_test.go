package tests

import (
	rabbitmq "github.com/carbonblack/cb-event-forwarder/pkg/rabbitmq"
	"testing"
)

func TestNewConsumer(t *testing.T) {
	c := rabbitmq.NewConsumerWithTlsCfg("amqp://lol:lol@cb/", "aqueuename", "consumertag", true, []string{"aroutingkey", "anotherroutingkey"}, rabbitmq.MockAMQPDialer{}, nil)
	if c.ConnectionErrors == nil {
		t.Errorf("ConnectionError channel not constructed properly")
	}
}
