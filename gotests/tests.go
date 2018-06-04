package tests

import (
	"github.com/carbonblack/cb-event-forwarder/internal/cbapi"
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/pbmessageprocessor"
)

var config conf.Configuration = conf.Configuration{CbServerURL: "https://cbtests/", EventMap: make(map[string]bool)}
var cbapihandler cbapi.CbAPIHandler = cbapi.CbAPIHandler{Config: &config}
var pbmp pbmessageprocessor.PbMessageProcessor = pbmessageprocessor.PbMessageProcessor{Config: &config}
var jsmp jsonmessageprocessor.JsonMessageProcessor = jsonmessageprocessor.JsonMessageProcessor{Config: &config, CbAPI: &cbapihandler}
