package config

import (
	"errors"
	"io"
	"testing"

	log "github.com/sirupsen/logrus"
)

func silenceLogs(t *testing.T) {
	t.Helper()
	prev := log.StandardLogger().Out
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.StandardLogger().Out = prev })
}

func TestDeriveAutomatedCbServerURL_FromFQDN(t *testing.T) {
	silenceLogs(t)
	u, err := deriveAutomatedCbServerURL("myserver.example.com", nil, func() (string, error) {
		t.Fatal("hostFn should not be called when FQDN is usable")
		return "", nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if want := "https://myserver.example.com/"; u != want {
		t.Fatalf("CbServerURL = %q, want %q", u, want)
	}
}

func TestDeriveAutomatedCbServerURL_FallbackLocalhostLocaldomain(t *testing.T) {
	silenceLogs(t)
	u, err := deriveAutomatedCbServerURL("localhost", nil, func() (string, error) {
		return "localhost.localdomain", nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if want := "https://localhost.localdomain/"; u != want {
		t.Fatalf("CbServerURL = %q, want %q", u, want)
	}
}

func TestDeriveAutomatedCbServerURL_FallbackAfterFQDNError(t *testing.T) {
	silenceLogs(t)
	u, err := deriveAutomatedCbServerURL("", errors.New("no fqdn"), func() (string, error) {
		return "edr-lab-01", nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if want := "https://edr-lab-01/"; u != want {
		t.Fatalf("CbServerURL = %q, want %q", u, want)
	}
}

func TestDeriveAutomatedCbServerURL_FallbackTrimsHostname(t *testing.T) {
	silenceLogs(t)
	u, err := deriveAutomatedCbServerURL("localhost", nil, func() (string, error) {
		return "  myhost  ", nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if want := "https://myhost/"; u != want {
		t.Fatalf("CbServerURL = %q, want %q", u, want)
	}
}

func TestDeriveAutomatedCbServerURL_FailsBareLocalhostHostname(t *testing.T) {
	silenceLogs(t)
	_, err := deriveAutomatedCbServerURL("localhost", nil, func() (string, error) {
		return "localhost", nil
	})
	if err == nil {
		t.Fatal("expected error when only localhost is available")
	}
}

func TestDeriveAutomatedCbServerURL_FailsEmptyHostname(t *testing.T) {
	silenceLogs(t)
	_, err := deriveAutomatedCbServerURL("", errors.New("fqdn fail"), func() (string, error) {
		return "", nil
	})
	if err == nil {
		t.Fatal("expected error for empty hostname")
	}
}

func TestDeriveAutomatedCbServerURL_FailsHostnameError(t *testing.T) {
	silenceLogs(t)
	_, err := deriveAutomatedCbServerURL("", errors.New("fqdn fail"), func() (string, error) {
		return "", errors.New("no hostname")
	})
	if err == nil {
		t.Fatal("expected error when os.Hostname fails")
	}
}

func TestParseCbServerURL_ExplicitWins(t *testing.T) {
	silenceLogs(t)
	iniBody := []byte("[bridge]\ncb_server_url=https://explicit.example/\n")
	f, err := ini.Load(iniBody)
	if err != nil {
		t.Fatal(err)
	}
	var c Configuration
	if err := c.parseCbServerURL(f); err != nil {
		t.Fatal(err)
	}
	if c.CbServerURL != "https://explicit.example/" {
		t.Fatalf("got %q", c.CbServerURL)
	}
}
