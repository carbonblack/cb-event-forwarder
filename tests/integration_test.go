//go:build integration

// Package tests contains integration tests that exercise multiple components
// working together without external dependencies (no live RabbitMQ, S3, etc.).
//
// Run with:
//
//	go test -tags integration -v ./tests -run Integration
package tests

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"text/template"
	"time"

	. "github.com/carbonblack/cb-event-forwarder/pkg/config"
	"github.com/carbonblack/cb-event-forwarder/pkg/forwarder"
	"github.com/carbonblack/cb-event-forwarder/pkg/outputs"
)

// awaitExitCond waits for exitCond to be signalled or fails the test after the
// given timeout. It must be called BEFORE the goroutine that signals the cond.
func awaitExitCond(t *testing.T, exitCond *sync.Cond, timeout time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		exitCond.L.Lock()
		exitCond.Wait()
		exitCond.L.Unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for output to stop")
	}
}

// TestIntegrationFileOutputPipeline verifies that FileOutput correctly writes
// messages to disk and exits cleanly when it receives SIGTERM.
func TestIntegrationFileOutputPipeline(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outFile := dir + "/events.log"

	cfg := &Configuration{OutputFormat: JSONOutputFormat}
	fo := outputs.NewFileOutputFromConfig(cfg)
	if err := fo.Initialize(outFile); err != nil {
		t.Fatalf("FileOutput.Initialize: %v", err)
	}

	messages := make(chan string, 10)
	signals := make(chan os.Signal, 1)
	exitCond := sync.NewCond(&sync.RWMutex{})

	if err := fo.Go(messages, signals, exitCond); err != nil {
		t.Fatalf("FileOutput.Go: %v", err)
	}

	events := []string{
		`{"type":"watchlist.hit.process","pid":1234}`,
		`{"type":"ingress.event.process","cmdline":"cmd.exe /c whoami"}`,
	}
	for _, ev := range events {
		messages <- ev
	}

	// Start waiting on the cond before signalling so the broadcast is not lost.
	go func() {
		time.Sleep(300 * time.Millisecond)
		signals <- syscall.SIGTERM
	}()
	awaitExitCond(t, exitCond, 10*time.Second)

	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	for _, ev := range events {
		if !strings.Contains(string(content), ev) {
			t.Errorf("output file missing expected event:\n  want substring: %s\n  got file:        %s", ev, content)
		}
	}
}

// TestIntegrationNetOutputTCPDelivery starts a local TCP listener, sends events
// through NetOutput, and verifies the listener receives the full payloads.
func TestIntegrationNetOutputTCPDelivery(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	// Collect lines received on the TCP side.
	var (
		mu       sync.Mutex
		received []string
		srvDone  sync.WaitGroup
	)
	srvDone.Add(1)
	go func() {
		defer srvDone.Done()
		conn, connErr := ln.Accept()
		if connErr != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			mu.Lock()
			received = append(received, sc.Text())
			mu.Unlock()
		}
	}()

	cfg := &Configuration{}
	no := outputs.NewNetOutputfromConfig(cfg)
	if err := no.Initialize("tcp:" + addr); err != nil {
		t.Fatalf("NetOutput.Initialize: %v", err)
	}

	messages := make(chan string, 10)
	signals := make(chan os.Signal, 1)
	exitCond := sync.NewCond(&sync.RWMutex{})

	if err := no.Go(messages, signals, exitCond); err != nil {
		t.Fatalf("NetOutput.Go: %v", err)
	}

	events := []string{
		`{"type":"watchlist.hit.process","pid":1234}`,
		`{"type":"ingress.event.netconn","remote_ip":"10.0.0.1"}`,
	}
	for _, ev := range events {
		messages <- ev
	}

	go func() {
		time.Sleep(300 * time.Millisecond)
		signals <- syscall.SIGTERM
	}()
	awaitExitCond(t, exitCond, 10*time.Second)

	// Close the listener so the scanner goroutine unblocks.
	ln.Close()
	srvDone.Wait()

	mu.Lock()
	defer mu.Unlock()
	for _, ev := range events {
		found := false
		for _, line := range received {
			if strings.Contains(line, ev) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("TCP listener did not receive expected event: %s\n  received lines: %v", ev, received)
		}
	}
}

// TestIntegrationHTTPBehaviorPost verifies that HTTPBehavior correctly POSTs
// event data to an HTTP endpoint with the right content and headers.
func TestIntegrationHTTPBehaviorPost(t *testing.T) {
	t.Parallel()

	var (
		mu           sync.Mutex
		receivedBody []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedBody = body
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	contentType := "application/json"
	tmpl := template.Must(template.New("post").Parse(`{{range .Events}}{{.EventText}}{{end}}`))
	cfg := &Configuration{
		OutputFormat:    JSONOutputFormat,
		HTTPContentType: &contentType,
		HTTPPostTemplate: tmpl,
	}

	behavior := &outputs.HTTPBehavior{Config: cfg}
	if err := behavior.Initialize(srv.URL); err != nil {
		t.Fatalf("HTTPBehavior.Initialize: %v", err)
	}

	event := `{"type":"watchlist.hit.process","pid":9999,"process_name":"notepad.exe"}`
	tmp, err := os.CreateTemp(t.TempDir(), "events-*.json")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := tmp.WriteString(event + "\n"); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}

	// Upload is synchronous: returns after the HTTP response is received.
	behavior.Upload(tmp.Name(), tmp)

	mu.Lock()
	body := string(receivedBody)
	mu.Unlock()

	if !strings.Contains(body, "watchlist.hit.process") {
		t.Errorf("HTTP POST body missing expected event type\n  body: %q", body)
	}
	if !strings.Contains(body, "notepad.exe") {
		t.Errorf("HTTP POST body missing expected process name\n  body: %q", body)
	}
}

// TestIntegrationHTTPBehaviorPostGzip verifies that HTTPBehavior sets
// Content-Encoding: gzip when CompressHTTPPayload is enabled.
func TestIntegrationHTTPBehaviorPostGzip(t *testing.T) {
	t.Parallel()

	var (
		mu              sync.Mutex
		receivedHeaders http.Header
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedHeaders = r.Header.Clone()
		mu.Unlock()
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	contentType := "application/json"
	tmpl := template.Must(template.New("post").Parse(`{{range .Events}}{{.EventText}}{{end}}`))
	cfg := &Configuration{
		OutputFormat:        JSONOutputFormat,
		HTTPContentType:     &contentType,
		HTTPPostTemplate:    tmpl,
		CompressHTTPPayload: true,
	}

	behavior := &outputs.HTTPBehavior{Config: cfg}
	if err := behavior.Initialize(srv.URL); err != nil {
		t.Fatalf("HTTPBehavior.Initialize: %v", err)
	}

	tmp, err := os.CreateTemp(t.TempDir(), "events-*.json")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	tmp.WriteString(`{"type":"ingress.event.process"}` + "\n")
	tmp.Seek(0, io.SeekStart)

	behavior.Upload(tmp.Name(), tmp)

	mu.Lock()
	encoding := receivedHeaders.Get("Content-Encoding")
	mu.Unlock()

	if encoding != "gzip" {
		t.Errorf("expected Content-Encoding: gzip, got %q", encoding)
	}
}

// TestIntegrationFileOutputRollover verifies that FileOutput rolls over the
// output file on SIGHUP and continues writing to a fresh file.
func TestIntegrationFileOutputRollover(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outFile := dir + "/events.log"

	cfg := &Configuration{OutputFormat: JSONOutputFormat}
	fo := outputs.NewFileOutputFromConfig(cfg)
	if err := fo.Initialize(outFile); err != nil {
		t.Fatalf("FileOutput.Initialize: %v", err)
	}

	messages := make(chan string, 10)
	signals := make(chan os.Signal, 1)
	exitCond := sync.NewCond(&sync.RWMutex{})

	if err := fo.Go(messages, signals, exitCond); err != nil {
		t.Fatalf("FileOutput.Go: %v", err)
	}

	// Write first batch, then request rollover, then write second batch.
	messages <- `{"batch":1,"type":"pre-rollover"}`
	time.Sleep(150 * time.Millisecond)
	signals <- syscall.SIGHUP
	time.Sleep(150 * time.Millisecond)
	messages <- `{"batch":2,"type":"post-rollover"}`

	go func() {
		time.Sleep(300 * time.Millisecond)
		signals <- syscall.SIGTERM
	}()
	awaitExitCond(t, exitCond, 10*time.Second)

	// After SIGHUP, the original file is renamed and a new one is created.
	// The current output file should contain only the post-rollover event.
	currentContent, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading current output file: %v", err)
	}
	if !strings.Contains(string(currentContent), "post-rollover") {
		t.Errorf("current file missing post-rollover event: %s", currentContent)
	}

	// At least one rolled-over file should exist and contain the pre-rollover event.
	entries, _ := os.ReadDir(dir)
	foundPreRollover := false
	for _, e := range entries {
		if e.Name() == "events.log" {
			continue
		}
		data, _ := os.ReadFile(dir + "/" + e.Name())
		if strings.Contains(string(data), "pre-rollover") {
			foundPreRollover = true
			break
		}
	}
	if !foundPreRollover {
		t.Error("no rolled-over file found containing the pre-rollover event")
	}
}

// TestIntegrationEventForwarderCannedInput runs the full EventForwarder
// pipeline using canned protobuf zip-bundle input and a FileOutput, verifying
// that processed events are written to disk.
func TestIntegrationEventForwarderCannedInput(t *testing.T) {
	cannedPath := "../test/stress_rabbit/zipbundles/bundleone"
	if _, err := os.Stat(cannedPath); err != nil {
		t.Skipf("canned input file not found (%s): skipping end-to-end test", cannedPath)
	}

	dir := t.TempDir()
	outFile := dir + "/forwarder-output.log"

	contentType := "application/json"
	tmpl := template.Must(template.New("http").Parse(`{{range .Events}}{{.EventText}}{{end}}`))

	cfg := &Configuration{
		ServerName:           "integration-test",
		CbServerURL:          "https://cbtests/",
		CannedInput:          true,
		CannedInputLocation:  &cannedPath,
		RunConsumer:          true,
		UseRawSensorExchange: true,
		EventMap:             ALLRAWEVENTS,
		NumProcessors:        1,
		OutputFormat:         JSONOutputFormat,
		OutputType:           FileOutputType,
		OutputParameters:     outFile,
		ExitTimeoutSeconds:   3 * time.Second,
		HTTPContentType:      &contentType,
		HTTPPostTemplate:     tmpl,
	}

	signals := make(chan os.Signal, 1)
	ef, err := forwarder.NewEventForwarderFromConfig(signals, cfg)
	if err != nil {
		t.Fatalf("NewEventForwarderFromConfig: %v", err)
	}

	if err := ef.Startup("integration-test-host"); err != nil {
		t.Fatalf("Startup: %v", err)
	}

	// Let the forwarder process canned events for a short period, then stop.
	go func() {
		time.Sleep(700 * time.Millisecond)
		signals <- syscall.SIGTERM
	}()

	ef.RunUntilExit()

	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading forwarder output file: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("forwarder output file is empty; expected forwarded events to be written")
	}
	t.Logf("TestIntegrationEventForwarderCannedInput: produced %d bytes of output", len(content))
}
