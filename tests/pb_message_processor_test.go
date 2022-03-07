package tests

import (
	cfg "github.com/carbonblack/cb-event-forwarder/pkg/config"
	pbm "github.com/carbonblack/cb-event-forwarder/pkg/protobufmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/pkg/sensorevents"
	"strings"
	"testing"
	"time"
)

func NewEnvironmentMessage() sensorevents.CbEnvironmentMsg {
	sensorId := int32(1)
	sensorHostname := "bob"
	endpoint := sensorevents.CbEndpointEnvironmentMsg{SensorId: &sensorId, SensorHostName: &sensorHostname}
	envMsg := sensorevents.CbEnvironmentMsg{Endpoint: &endpoint}
	return envMsg
}

func NewHeaderMessage() sensorevents.CbHeaderMsg {
	currentTime := time.Now().Unix()
	header := sensorevents.CbHeaderMsg{Timestamp: &currentTime}
	return header
}

func NewProcessMessage() sensorevents.CbEventMsg {
	header := NewHeaderMessage()
	currentTime := time.Now().Unix()
	environment := NewEnvironmentMessage()
	process := sensorevents.CbProcessMsg{ParentCreateTime: &currentTime}
	event := sensorevents.CbEventMsg{Env: &environment, Process: &process, Header: &header}
	return event
}

func TestProtobufProcessEventHasTimestamp(t *testing.T) {
	pb := pbm.NewProtobufMessageProcessor(&cfg.Configuration{OutputFormat: cfg.JSONOutputFormat})
	event := NewProcessMessage()
	output := pb.NewProcessEvent(&event, "")
	outputBytes, _ := pb.GetMessageInOutputFormat(output)
	outputString := string(outputBytes)
	foundTimestamp := strings.Contains(outputString, "timestamp")
	foundCreateTimestamp := strings.Contains(outputString, "parent_create_time")
	foundBoth := foundTimestamp && foundCreateTimestamp
	if !foundBoth {
		t.Fatalf("Didn't find timestamps in output: %s", outputString)
	}
}
