package tests

import (
	"github.com/carbonblack/cb-event-forwarder/pkg/leefencoder"
	"testing"
)

func marshalLeef(msgs []map[string]interface{}) (string, error) {
	var ret string

	for _, msg := range msgs {
		msg["cb_server"] = "cbserver"
		marshaled, err := leefencoder.Encode(msg)
		if err != nil {
			return "", err
		}
		ret += marshaled + "\n"
	}

	return ret, nil
}

func TestLeefOutput(t *testing.T) {
	t.Log("Generating LEEF output to leef_output...")
	processTestEvents(t, ALLFORMATS, "leef_output", marshalLeef)
}
