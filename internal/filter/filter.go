package filter
//package main

import (
	"bytes"
	"text/template"
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"encoding/json"
	log "github.com/sirupsen/logrus"
	"os"
	"strings"
)

//boolean returns false if the message should be discarded by the event forwarder
//FILTER RETURN VALUES: "KEEP" to keep a msg, "DROP" to drop a message all other returns get DROPPED
func FilterWithTemplate(msg map[string]interface{}, template *template.Template) bool {
	var doc bytes.Buffer
	err := template.Execute(&doc, msg)
	if err == nil {
		msg_str := strings.TrimSpace(doc.String())
		keep := msg_str == "KEEP"
		drop := msg_str == "DROP"
		if keep {
			return true
		}
		if drop {
			return false
		}
		log.Warnf("Filter template failed to generate expected output properly %s", msg_str)
		return false
	} else {
		log.Warnf("Filter template failed to execute properly %v", err)
		return false
	}
}

func main() {
	log.Infof("Starting filter test")
	var msgDict map[string]interface{}
	config, err := conf.ParseConfig(os.Args[1])
	if err == nil {
		m := "{\"k\": {\"k\":\"v\"}}"
		json.Unmarshal([] byte (m), &msgDict)
		keepEvent := true
		if config.FilterEnabled {
			keepEvent = FilterWithTemplate(msgDict, config.FilterTemplate)
			log.Infof("Filter result :  %t ", keepEvent)
		}
	} else {
		log.Warn("%v",err)
	}
}
