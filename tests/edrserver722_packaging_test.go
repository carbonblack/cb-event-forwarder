package tests

import (
	"strings"
	"testing"
)

// EDRSERVER-722: systemd unit /etc/systemd/system/cb-event-forwarder.service must ship as mode 644 (not 755).

func TestEDRSERVER722_MakefileInstallsSystemdUnitAs644(t *testing.T) {
	makefile := readRepoFile(t, "Makefile")
	if !strings.Contains(makefile, "install -m 644 cb-event-forwarder.service") {
		t.Fatal("Makefile must use 'install -m 644 cb-event-forwarder.service' for the systemd unit (EDRSERVER-722)")
	}
	if strings.Contains(makefile, "cp -p cb-event-forwarder.service ${RPM_BUILD_ROOT}/etc/systemd/system/") {
		t.Fatal("Makefile must not use cp -p for systemd unit (preserves wrong mode); use install -m 644 (EDRSERVER-722)")
	}
}

func TestEDRSERVER722_RpmSpecDeclares644SystemdUnit(t *testing.T) {
	spec := readRepoFile(t, "cb-event-forwarder.rpm.spec")
	if !strings.Contains(spec, "%attr(0644,root,root) /etc/systemd/system/cb-event-forwarder.service") {
		t.Fatalf("RPM spec must declare systemd unit with %%attr(0644,root,root) (EDRSERVER-722)")
	}
	if !strings.Contains(spec, `"%{dist}" != ".el6"`) {
		t.Fatalf(`RPM spec must gate systemd %%files entry on dist != ".el6" so EL6 builds still work (EDRSERVER-722)`)
	}
}

func TestEDRSERVER722_ManifestsDoNotDuplicateSystemdUnit(t *testing.T) {
	unitPath := "/etc/systemd/system/cb-event-forwarder.service"
	for _, m := range []string{"MANIFEST7", "MANIFEST8", "MANIFEST9"} {
		body := readRepoFile(t, m)
		if strings.Contains(body, unitPath) {
			t.Fatalf("%s must not list %s; unit is owned by %%files line in spec to set %%attr (EDRSERVER-722)", m, unitPath)
		}
	}
}

func TestEDRSERVER722_FixPermissionsScriptChmodsUnit(t *testing.T) {
	sh := readRepoFile(t, "cb-edr-fix-permissions.sh")
	if !strings.Contains(sh, "/etc/systemd/system/cb-event-forwarder.service") {
		t.Fatal("cb-edr-fix-permissions.sh must reference the systemd unit path (EDRSERVER-722)")
	}
	if !strings.Contains(sh, "chmod 644") {
		t.Fatal("cb-edr-fix-permissions.sh must chmod 644 the systemd unit when present (EDRSERVER-722)")
	}
}
