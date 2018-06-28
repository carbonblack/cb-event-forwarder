package tests

import (
	"github.com/carbonblack/cb-event-forwarder/internal/encoder"
	"testing"
)

func marshalTemplate(msgs []map[string]interface{}) (string, error) {
	te, _ := encoder.NewTemplateEncoder("{{JsonFormat .}}", "", "")
	var ret string
	for _, msg := range msgs {
		msg["cb_server"] = "cbserver"
		marshaled, err := te.Encode(msg)
		if err != nil {
			return "", err
		}
		ret += marshaled + "\n"
	}

	return ret, nil
}

func TestTemplateOutput(t *testing.T) {
	t.Log("Generating Template output to template_output...")
	processTestEvents(t, "template_output", marshalTemplate)
}
