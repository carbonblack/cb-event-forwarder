package encoder

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/internal/util"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"text/template"
	"time"
)

type Encoder interface {
	Encode(msg map[string]interface{}) (string, error)
	String() string
}

type TemplateEncoder struct {
	EncoderTemplate *template.Template
}

func (e *TemplateEncoder) String() string {
	return "template"
}

func (e *TemplateEncoder) Encode(msg map[string]interface{}) (string, error) {
	var doc bytes.Buffer
	err := e.EncoderTemplate.Execute(&doc, msg)
	if err == nil {
		return doc.String(), nil
	} else {
		return "", err
	}
}

func NewTemplateEncoder(templateString, templatePlugin, pluginPath string) (TemplateEncoder, error) {
	var encoderTemplate *template.Template = nil
	var err error = nil
	if templatePlugin != "" {
		log.Info("!!!LOADING ENCODER PLUGIN!!!")
		tmpl, e := template.New("TemplateEncoder").Funcs(util.LoadFuncMapFromPlugin(pluginPath, templatePlugin)).Parse(templateString)
		encoderTemplate = tmpl
		err = e
	} else {
		tmpl, e := template.New("TemplateEncoder").Funcs(GetUtilFuncMap()).Parse(templateString)
		encoderTemplate = tmpl
		err = e
	}
	temp := TemplateEncoder{EncoderTemplate: encoderTemplate}
	return temp, err
}

func Leef(raw_input map[string]interface{}) (string, error) {
	return (&LEEFEncoder{}).Encode(raw_input)
}

func Cef(raw_input map[string]interface{}, cef_severity int) (string, error) {
	return (&CEFEncoder{eventSeverity: cef_severity}).Encode(raw_input)
}

func Json(raw_input map[string]interface{}) (string, error) {
	ret, err := json.Marshal(raw_input)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s", ret), nil
}

func Yaml(raw_input map[string]interface{}) (string, error) {
	ret, err := yaml.Marshal(raw_input)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s", ret), nil
}
func GetUtilFuncMap() template.FuncMap {
	funcMap := template.FuncMap{"LeefFormat": Leef, "CefFormat": Cef, "JsonFormat": Json, "YamlFormat": Yaml, "GetCurrentTimeFormat": GetCurrentTimeFormat, "GetCurrentTimeRFC3339": GetCurrentTimeRFC3339, "GetCurrentTimeUnix": GetCurrentTimeUnix, "GetCurrentTimeUTC": GetCurrentTimeUTC, "ParseTime": ParseTime}
	return funcMap
}

func GetCurrentTimeRFC3339() string {
	t := time.Now()
	return t.Format(time.RFC3339)
}

func GetCurrentTimeFormat(format string) string {
	t := time.Now()
	return t.Format(format)
}

func GetCurrentTimeUnix() string {
	t := time.Now()
	return fmt.Sprintf("%v", t.Unix())
}

func GetCurrentTimeUTC() string {
	t := time.Now()
	return fmt.Sprintf("%v", t.UTC())
}

func ParseTime(t string, format string) (string, error) {
	parsed, err := time.Parse(format, t)
	if err == nil {
		return fmt.Sprintf("%v", parsed), nil
	} else {
		return "", err
	}
}

func GetEncoderFromCfg(cfg map[interface{}]interface{}) (Encoder, error) {
	if t, ok := cfg["type"]; ok {
		switch t.(string) {
		case "leef":
			le := NewLEEFEncoder()
			return &le, nil
		case "cef":
			ce := NewCEFEncoder(cfg["severity"].(int))
			return &ce, nil
		case "json":
			je := NewJSONEncoder()
			return &je, nil
		case "template":
			if encoderTemplate, ok := cfg["template"]; ok {
				if encoderPlugin, ok := cfg["plugin"]; ok {
					if pluginPath, ok := cfg["plugin_path"]; ok {
						te, err := NewTemplateEncoder(encoderTemplate.(string), encoderPlugin.(string), pluginPath.(string))
						return &te, err
					}
					//use default plugin path '.'
					te, err := NewTemplateEncoder(encoderTemplate.(string), encoderPlugin.(string), ".")
					return &te, err
				} //no plugin specified

				te, err := NewTemplateEncoder(encoderTemplate.(string), "", "")
				return &te, err
			} else {
				return nil, errors.New("Enter a valid template")
			}
		default:
			return nil, errors.New("Enter a valid encoder.format format: leef,cef,json or template")
		}
	}
	return nil, errors.New("enter a valid encoder format: leef,cef,json, or template ")
}
