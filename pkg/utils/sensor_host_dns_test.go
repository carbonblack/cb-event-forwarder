package utils

import (
	"testing"

	"github.com/streadway/amqp"
)

func TestSensorHostDnsNameFromHeaders(t *testing.T) {
	if SensorHostDnsNameFromHeaders(nil) != "" {
		t.Fatal("expected empty for nil table")
	}
	if SensorHostDnsNameFromHeaders(amqp.Table{}) != "" {
		t.Fatal("expected empty for empty table")
	}
	if got := SensorHostDnsNameFromHeaders(amqp.Table{"sensorHostDnsName": "host.example.com"}); got != "host.example.com" {
		t.Fatalf("got %q", got)
	}
	if got := SensorHostDnsNameFromHeaders(amqp.Table{"sensorHostDnsName": "  x  "}); got != "x" {
		t.Fatalf("trim: got %q", got)
	}
	if got := SensorHostDnsNameFromHeaders(amqp.Table{"sensorHostDnsName": ""}); got != "" {
		t.Fatalf("empty string: got %q", got)
	}
	if got := SensorHostDnsNameFromHeaders(amqp.Table{"sensorHostDnsName": []byte("b.example.com")}); got != "b.example.com" {
		t.Fatalf("bytes: got %q", got)
	}
	if got := SensorHostDnsNameFromHeaders(amqp.Table{"sensorHostDnsName": 123}); got != "" {
		t.Fatalf("wrong type: got %q", got)
	}
}
