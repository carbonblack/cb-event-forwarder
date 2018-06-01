package tests

import (
	"github.com/carbonblack/cb-event-forwarder/internal/cbapi"
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/pbmessageprocessor"
	"text/template"
	"time"
)

var configHTTPTemplate *template.Template = template.Must(template.New("testtemplateconfig").Parse(
	`{"filename": "{{.FileName}}", "service": "carbonblack", "alerts":[{{range .Events}}{{.EventText}}{{end}}]}`))
var config conf.Configuration = conf.Configuration{UploadEmptyFiles: false, BundleSizeMax: 1024 * 1024 * 1024, BundleSendTimeout: time.Duration(30) * time.Second, CbServerURL: "https://cbtests/", HTTPPostTemplate: configHTTPTemplate, DebugStore: ".", DebugFlag: true, EventMap: make(map[string]bool)}

var cbapihandler cbapi.CbAPIHandler = cbapi.CbAPIHandler{Config: &config}
var pbmp pbmessageprocessor.PbMessageProcessor = pbmessageprocessor.PbMessageProcessor{Config: &config}
var jsmp jsonmessageprocessor.JsonMessageProcessor = jsonmessageprocessor.JsonMessageProcessor{Config: &config, CbAPI: &cbapihandler}
