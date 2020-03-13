package main

import (
	"testing"
)

func TestNewConsumer(t *testing.T) {
	c := NewConsumerWithTlsCfg("amqp://lol:lol@cb/", "aqueuename", "consumertag", true, []string{"aroutingkey", "anotherroutingkey"}, MockAMQPDialer{}, nil)
	if c.connectionErrors == nil {
		t.Errorf("ConnectionError channel not constructed properly")
	}
}
