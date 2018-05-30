package templateencoder
//package main
import (
	"bytes"
	"text/template"
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"github.com/carbonblack/cb-event-forwarder/internal/filter"
	"encoding/json"
	log "github.com/sirupsen/logrus"
	"os"
)

func EncodeWithTemplate(msg map[string]interface{}, template * template.Template) (string, error) {
	var doc bytes.Buffer
	err := template.Execute(&doc,msg)
	if err == nil {
		return doc.String(), nil
	} else {
		return "", err
	}
}

func main() {
	log.Infof("Starting")
	var msgDict map[string] interface{}

	config, err := conf.ParseConfig(os.Args[1])
	if err == nil {
		m := "{\"k\": {\"k\":\"v\"}}"
		json.Unmarshal([] byte (m),&msgDict)
		encodedMsg , err:= EncodeWithTemplate(msgDict,config.EncoderTemplate)
		log.Infof("Done encoding with: %s %v",encodedMsg,err)
		keepEvent := true
		if config.FilterEnabled {
			keepEvent = filter.FilterWithTemplate(msgDict, config.FilterTemplate)
			log.Infof("Filter result :  %t ", keepEvent)
		}
	} else {
		log.Warn("%v",err)
	}
}
