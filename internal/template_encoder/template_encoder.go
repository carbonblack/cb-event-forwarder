package templateencoder

import (
	"bytes"
	"text/template"
)

func EncodeWithTemplate(msg map[string]interface{}, template *template.Template) (string, error) {
	var doc bytes.Buffer
	err := template.Execute(&doc, msg)
	if err == nil {
		return doc.String(), nil
	} else {
		return "", err
	}
}
