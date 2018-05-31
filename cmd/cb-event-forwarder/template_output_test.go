package main

import (
	te "github.com/carbonblack/cb-event-forwarder/internal/template_encoder"
	"github.com/carbonblack/cb-event-forwarder/internal/util"
	"testing"
    	"text/template"
)

func marshalTemplate(msgs []map[string]interface{}) (string, error) {
    var EncoderTemplate * template.Template = template.New("testencoder").Funcs(template.FuncMap{"JsonFormat":util.Json})
    EncoderTemplate, _ = EncoderTemplate.Parse("{{JsonFormat .}}")
	var ret string

	for _, msg := range msgs {
		msg["cb_server"] = "cbserver"
		marshaled, err := te.EncodeWithTemplate(msg,EncoderTemplate)
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





