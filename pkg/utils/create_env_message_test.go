package utils

import (
	"testing"

	"github.com/streadway/amqp"
)

func TestCreateEnvMessageSetsDnsFromHeaderWhenPresent(t *testing.T) {
	h := amqp.Table{"sensorHostDnsName": "d.example.com"}
	env, err := CreateEnvMessage(h, true)
	if err != nil {
		t.Fatal(err)
	}
	if env.Endpoint.GetSensorHostDnsName() != "d.example.com" {
		t.Fatalf("expected DNS from header, got %q", env.Endpoint.GetSensorHostDnsName())
	}
}

func TestCreateEnvMessageOmitsDnsWhenDisabled(t *testing.T) {
	h := amqp.Table{"sensorHostDnsName": "d.example.com"}
	env, err := CreateEnvMessage(h, false)
	if err != nil {
		t.Fatal(err)
	}
	if env.Endpoint.GetSensorHostDnsName() != "" {
		t.Fatalf("expected no DNS when includeSensorHostDns is false, got %q", env.Endpoint.GetSensorHostDnsName())
	}
}
