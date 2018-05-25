package filter

import (
	"bytes"
	log "github.com/sirupsen/logrus"
	"text/template"
)

//boolean returns false if the message should be discarded by the event forwarder
//FILTER RETURN VALUES: "KEEP" to keep a msg, "DROP" to drop a message all other returns get DROPPED
func FilterWithTemplate(msg map[string]interface{}, template *template.Template) bool {
	var doc bytes.Buffer
	err := template.Execute(&doc, msg)
	if err == nil {
		msg_str := doc.String()
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
