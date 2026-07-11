package tests

import (
	"regexp"
	"strings"
	"testing"
)

// EDRSERVER-461: boot/restart race — EF exits when syslog sink (e.g. tcp:localhost:514) is not
// listening yet; rapid RestartSec exhausts systemd start limits. Unit must order after syslog and
// use a sane RestartSec.

func TestEDRSERVER461_SystemdUnitOrdersAfterSyslogSocket(t *testing.T) {
	unit := readRepoFile(t, "cb-event-forwarder.service")
	matched, _ := regexp.MatchString(`(?m)^After=.*syslog\.socket`, unit)
	if !matched {
		t.Fatal("cb-event-forwarder.service must include After=... syslog.socket on the same line for startup ordering (EDRSERVER-461)")
	}
	if !strings.Contains(unit, "Wants=syslog.socket") {
		t.Fatal("cb-event-forwarder.service must include Wants=syslog.socket (soft dep) (EDRSERVER-461)")
	}
}

func TestEDRSERVER461_SystemdUnitUsesBackoffRestartSec(t *testing.T) {
	unit := readRepoFile(t, "cb-event-forwarder.service")
	if !strings.Contains(unit, "RestartSec=10") {
		t.Fatal("cb-event-forwarder.service must set RestartSec=10 to avoid default 100ms burst vs StartLimitBurst (EDRSERVER-461)")
	}
}
