package tests

import (
	"encoding/json"
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
	created := true
	process := sensorevents.CbProcessMsg{ParentCreateTime: &currentTime, Created: &created}
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

// TestProcessEventParentGuidNotNegative is a regression test for the case where
// the deprecated parent_guid protobuf field carries a uint64 value whose MSB is
// set, causing it to appear negative when interpreted as int64.  When the modern
// fields (ParentPid + ParentCreateTime) are absent the code falls back to the raw
// protobuf value, which must be rendered as an unsigned (non-negative) number.
func TestProcessEventParentGuidNotNegative(t *testing.T) {
	// int64 bit-pattern of a uint64 value with MSB set — exactly the class of
	// value observed in the bug report (parent_guid: -8020028680769161069).
	rawGuid := int64(-8020028680769161069)

	pb := pbm.NewProtobufMessageProcessor(&cfg.Configuration{OutputFormat: cfg.JSONOutputFormat})

	header := NewHeaderMessage()
	environment := NewEnvironmentMessage()
	created := true
	// ParentPid and ParentCreateTime are intentionally omitted so that MakeGUID()
	// cannot be used and the code falls back to msg.Process.GetParentGuid().
	process := sensorevents.CbProcessMsg{
		Created:    &created,
		ParentGuid: &rawGuid,
	}
	event := sensorevents.CbEventMsg{Env: &environment, Process: &process, Header: &header}

	output := pb.NewProcessEvent(&event, "ingress.event.procstart")
	if output == nil {
		t.Fatal("NewProcessEvent returned nil")
	}

	outputBytes, err := pb.GetMessageInOutputFormat(output)
	if err != nil {
		t.Fatalf("GetMessageInOutputFormat error: %v", err)
	}

	decoder := json.NewDecoder(strings.NewReader(string(outputBytes)))
	decoder.UseNumber()
	var result map[string]interface{}
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("failed to parse output JSON: %v\noutput: %s", err, outputBytes)
	}

	raw, ok := result["parent_guid"]
	if !ok {
		t.Fatal("parent_guid field is missing from output")
	}

	switch v := raw.(type) {
	case json.Number:
		val, err := v.Int64()
		if err == nil && val < 0 {
			t.Errorf("parent_guid is negative (%d); large uint64 GUIDs must not wrap to negative", val)
		}
	case string:
		// A formatted GUID string (e.g. "00000001-0000-...") is always non-negative — pass.
	default:
		t.Errorf("parent_guid has unexpected type %T: %v", raw, raw)
	}
}
