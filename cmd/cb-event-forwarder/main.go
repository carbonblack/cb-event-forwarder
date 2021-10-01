package main

import (
	"encoding/json"
	"expvar"
	"flag"
	"fmt"
	graphite "github.com/cyberdelia/go-metrics-graphite"
	"github.com/facebookgo/pidfile"
	"github.com/rcrowley/go-metrics/exp"
	log "github.com/sirupsen/logrus"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	. "github.com/carbonblack/cb-event-forwarder/pkg/config"
	. "github.com/carbonblack/cb-event-forwarder/pkg/forwarder"
	. "github.com/carbonblack/cb-event-forwarder/pkg/logging"
	"github.com/rcrowley/go-metrics"
	_ "net/http/pprof"
)

var (
	pidFileLocation    = flag.String("pid-file", "", "PID file location")
	checkConfiguration = flag.Bool("check", false, "Check the configuration file and exit")
	debug              = flag.Bool("debug", false, "Enable debugging mode")
)

var version = "3.7.5"

var signals = make(chan os.Signal, 2)
var config Configuration

func main() {
	flag.Parse()

	hostname := handleGetHostname()

	config = handleConfigurationLoading()

	forwarder, err := NewEventForwarderFromConfig(signals, &config)
	if err != nil {
		log.Fatalf("%s", err)
	}

	if *checkConfiguration {
		checkConfig(&forwarder)
	}

	lh := handleStartup(hostname, &forwarder)

	handleExit(&forwarder, lh)

}

func handleStartup(hostname string, forwarder *EventForwarder) * LogFileHandler {
	log.Infof("cb-event-forwarder version %s starting", version)

	handlePidFile()

	showNetworkInterfaces()

	logHandler := handleDebugLoggingAndMetrics(hostname, forwarder)

	showNetworkInterfaces()

	handleStart(forwarder, hostname)

	startDebugServer(forwarder)

	handleMetricsToGraphite()

	return logHandler
}

func checkConfig(forwarder *EventForwarder) {
	if err := forwarder.StartOutput(); err != nil {
		log.Fatal(err)
	}
	os.Exit(0)
}

func handlePidFile() {
	defaultPidFileLocation := "/run/cb/integrations/cb-event-forwarder/cb-event-forwarder.pid"
	if *pidFileLocation == "" {
		*pidFileLocation = defaultPidFileLocation
	}
	log.Infof("PID file will be written to %s\n", *pidFileLocation)
	pidfile.SetPidfilePath(*pidFileLocation)
	err := pidfile.Write()
	if err != nil {
		log.Warnf("Could not write PID file: %v\n", err)
	}
}

func showNetworkInterfaces() {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Fatal("Could not get IP addresses")
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			log.Infof("Interface address %s", ipnet.IP.String())
		}
	}
}

func handleMetricsToGraphite() {
	if config.CarbonMetricsEndpoint != nil && config.RunMetrics {
		addr, err := net.ResolveTCPAddr("tcp4", *config.CarbonMetricsEndpoint)
		if err != nil {
			log.Panicf("Failing resolving carbon endpoint %v", err)
		}

		go graphite.Graphite(metrics.DefaultRegistry, 1*time.Second, config.MetricTag, addr)
		log.Infof("Sending metrics to graphite")
	}

	exp.Exp(metrics.DefaultRegistry)
}

func startDebugServer(forwarder *EventForwarder) {

	http.HandleFunc("/debug/healthcheck", func(w http.ResponseWriter, r *http.Request) {
		if !forwarder.Status.IsConnected {
			payload, _ := json.Marshal(map[string]interface{}{"status": "FORWARDER IS NOT CONNECTED", "error": forwarder.Status.LastConnectError})
			http.Error(w, string(payload), http.StatusNetworkAuthenticationRequired)
		} else {
			payload, _ := json.Marshal(map[string]interface{}{"status": "FORWARDER IS CONNECTED"})
			w.Write(payload)
		}
	})

	go http.ListenAndServe(fmt.Sprintf(":%d", config.HTTPServerPort), nil)

}

func handleDebugLoggingAndMetrics(hostname string, forwarder *EventForwarder) * LogFileHandler {
	exportedVersion := &expvar.String{}
	metrics.Register("version", exportedVersion)
	if *debug {
		exportedVersion.Set(version + " (debugging on)")
		log.Debugf("*** Debugging enabled: messages may be sent via http://%s:%d/debug/sendmessage ***",
			hostname, config.HTTPServerPort)
		log.SetLevel(log.DebugLevel)

	} else {
		exportedVersion.Set(version)
	}

	metrics.Register("debug", expvar.Func(func() interface{} { return *debug }))

	return SetUpLogger(forwarder.Configuration)

}

func SetUpLogger(config * Configuration) * LogFileHandler{
	lh := NewLogFileHandler(config.LogDir, config.LogLevel, config.LogSizeMB, config.LogMaxAge, config.LogBackups)
	lh.InitializeLogging()
	return &lh
}

func handleConfigurationLoading() Configuration {
	configLocation := "/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf"
	if flag.NArg() > 0 {
		configLocation = flag.Arg(0)
	}
	log.Infof("Using config file %s\n", configLocation)
	config, err := ParseConfig(configLocation)
	if err != nil {
		log.Fatal(err)
	}
	return config
}

func handleGetHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}
	return hostname
}

func handleStart(forwarder *EventForwarder, hostname string) {
	log.Infof("Configured to capture events: %v", forwarder.EventTypes)
	if err := forwarder.Startup(hostname); err != nil {
		log.Fatalf("Could not startup: %s", err)
	}
}

func handleExit(forwarder *EventForwarder, logHandler * LogFileHandler) {
	hookSignals()
	defer log.Infof("EF Shutdown complete...")
	defer logHandler.Rotate()
	defer signal.Stop(signals)
	forwarder.RunUntilExit()
}

func hookSignals() {
	signal.Notify(signals, syscall.SIGHUP)
	signal.Notify(signals, syscall.SIGTERM)
	signal.Notify(signals, syscall.SIGINT)
}
