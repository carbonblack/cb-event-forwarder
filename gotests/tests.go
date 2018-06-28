package tests

import (
	"github.com/carbonblack/cb-event-forwarder/internal/cbapi"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/pbmessageprocessor"
)

//var configHTTPTemplate *template.Template = template.Must(template.New("testtemplateconfig").Parse(
//`{"filename": "{{.FileName}}", "service": "carbonblack", "alerts":[{{range .Events}}{{.EventText}}{{end}}]}`))
//var config conf.Configuration = conf.Configuration{UploadEmptyFiles: false, BundleSizeMax: 1024 * 1024 * 1024, BundleSendTimeout: time.Duration(30) * time.Second, CbServerURL: "https://cbtests/", HTTPPostTemplate: configHTTPTemplate, DebugStore: ".", DebugFlag: true, EventMap: make(map[string]bool)}

var cbapihandler cbapi.CbAPIHandler = cbapi.CbAPIHandler{}
var jsmp jsonmessageprocessor.JsonMessageProcessor = jsonmessageprocessor.JsonMessageProcessor{}
var eventMap map[string]interface{} = map[string]interface{}{
	"ingress.event.process":        true,
	"ingress.event.procstart":      true,
	"ingress.event.netconn":        true,
	"ingress.event.procend":        true,
	"ingress.event.childproc":      true,
	"ingress.event.moduleload":     true,
	"ingress.event.module":         true,
	"ingress.event.filemod":        true,
	"ingress.event.regmod":         true,
	"ingress.event.tamper":         true,
	"ingress.event.crossprocopen":  true,
	"ingress.event.remotethread":   true,
	"ingress.event.processblock":   true,
	"ingress.event.emetmitigation": true,
	"binaryinfo.#":                 true,
	"binarystore.#":                true,
	"events.partition.#":           true,
}
var pbmp pbmessageprocessor.PbMessageProcessor = pbmessageprocessor.PbMessageProcessor{EventMap: eventMap}
