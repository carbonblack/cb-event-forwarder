package forwarder

import (
	"encoding/json"
)

type AuditLogEvent struct {
	Message  string `json:"message"`
	Type     string `json:"type"`
	CbServer string `json:"cb_server"`
}

func (auditLogEvent AuditLogEvent) asJson() ([]byte, error) {
	return json.Marshal(auditLogEvent)
}

func NewAuditLogEvent(message, logType, serverName string) AuditLogEvent {
	return AuditLogEvent{Message: message, Type: logType, CbServer: serverName}
}
