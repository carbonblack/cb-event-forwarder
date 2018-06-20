package filter

//package main

import (
	"bytes"
	log "github.com/sirupsen/logrus"
	"strings"
	"text/template"
	"github.com/carbonblack/cb-event-forwarder/internal/util"
)

type Filter struct {
	FilterTemplate * template.Template
}

func NewFilter(filterString, filterPlugin, pluginPath string) (*Filter ,error){
	var filterTemplate * template.Template = nil
	var err error = nil
	if filterPlugin != "" {
		log.Info("!!!LOADING FILTER PLUGIN!!!")
		filterTemplate, err = template.New("TemplatFilter").Funcs(util.LoadFuncMapFromPlugin(pluginPath, filterPlugin)).Parse(filterString)
	} else {
		filterTemplate, err = template.New("TemplateFilter").Parse(filterString)
	}
	f := Filter{FilterTemplate:filterTemplate}
	return &f, err
}

//boolean returns false if the message should be discarded by the event forwarder
//FILTER RETURN VALUES: "KEEP" to keep a msg, "DROP" to drop a message all other returns get DROPPED
func (f * Filter) FilterEvent(msg map[string]interface{}) bool {
	var doc bytes.Buffer
	err := f.FilterTemplate.Execute(&doc, msg)
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

func GetFilterFromCfg(cfg map[interface{}] interface{}) (*Filter, error) {
	log.Infof("Trying to load filter from cfg")
	if filter, ok := cfg["template"]; ok {
		if filterPlugin , ok := cfg["plugin"]; ok {
			if pluginPath, ok := cfg["plugin_path"]; ok {
				return NewFilter(filter.(string),filterPlugin.(string),pluginPath.(string))
			}
			//default plugin path
			return NewFilter(filter.(string),filterPlugin.(string),".")
		}
		return NewFilter(filter.(string),"",".")
	}
	return nil, nil //template section is optional
}
