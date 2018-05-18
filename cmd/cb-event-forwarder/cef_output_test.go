package main

import (
	cef "github.com/carbonblack/cb-event-forwarder/internal/cef"
	"testing"
)

func marshalCef(msgs []map[string]interface{}) (string, error) {
	var ret string

	for _, msg := range msgs {
		msg["cb_server"] = "cbserver"
		marshaled, err := cef.Encode(msg)
		if err != nil {
			return "", err
		}
		ret += marshaled + "\n"
	}

	return ret, nil
}

func TestCefOutput(t *testing.T) {
	t.Log("Generating CEF output to cef_output...")
	processTestEvents(t, "cef_output", marshalCef)
}
