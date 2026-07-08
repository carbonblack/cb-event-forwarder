package tests

import (
	"strings"
	"testing"
)

// EDRSERVER-150: audit log tailer must use the same JSON/LEEF pipeline as AMQP events
// (previously always emitted raw JSON from auditLogEvent.asJson()).

func TestEDRSERVER150_ForwarderAuditLogUsesMessageProcessor(t *testing.T) {
	fwd := readRepoFile(t, "pkg/forwarder/forwarder.go")
	if !strings.Contains(fwd, "jsonmessageprocessor.NewJsonMessageProcessor") {
		t.Fatal("logFileProcessingLoop should build JsonMessageProcessor for output format (EDRSERVER-150)")
	}
	if !strings.Contains(fwd, "ProcessJSONMessageWithFormat") {
		t.Fatal("audit log path should call ProcessJSONMessageWithFormat so LEEF applies (EDRSERVER-150)")
	}
	if strings.Contains(fwd, "rawLogEvent, _ := auditLogEvent.asJson()\n\t\t\toutputMessage(rawLogEvent") {
		t.Fatal("must not short-circuit audit logs to outputMessage as raw JSON only (EDRSERVER-150)")
	}
}
