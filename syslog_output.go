package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	syslog "github.com/RackSec/srslog"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type SyslogOutput struct {
	protocol     string
	hostnamePort string
	tag          string
	outputSocket *syslog.Writer

	connectTime                 time.Time
	reconnectTime               time.Time
	connected                   bool
	droppedEventCount           int64
	droppedEventSinceConnection int64

	sync.RWMutex
}

type SyslogStatistics struct {
	LastOpenTime       time.Time `json:"last_open_time"`
	Protocol           string    `json:"protocol"`
	RemoteHostnamePort string    `json:"remote_hostname_port"`
	DroppedEventCount  int64     `json:"dropped_event_count"`
	Connected          bool      `json:"connected"`
}

// Initialize() expects a connection string in the following format:
// (protocol):(hostname/IP):(port)
// for example: tcp+tls:destination.server.example.com:512
func (o *SyslogOutput) Initialize(netConn string) error {
	o.Lock()
	defer o.Unlock()

	if o.connected {
		o.outputSocket.Close()
	}

	connSpecification := strings.SplitN(netConn, ":", 2)

	o.protocol = connSpecification[0]
	o.hostnamePort = connSpecification[1]

	tlsConfig := &tls.Config{}

	if config.SyslogTLSVerify == false {
		log.Printf("Disabling TLS verification for syslog output at %s", netConn)
		tlsConfig.InsecureSkipVerify = true
	}

	if config.SyslogTLSClientCert != nil && config.SyslogTLSClientKey != nil && len(*config.SyslogTLSClientCert) > 0 &&
		len(*config.SyslogTLSClientKey) > 0 {
		log.Printf("Loading client cert/key from %s & %s for syslog output at %s", *config.SyslogTLSClientCert,
			*config.SyslogTLSClientKey, netConn)
		cert, err := tls.LoadX509KeyPair(*config.SyslogTLSClientCert, *config.SyslogTLSClientKey)
		if err != nil {
			log.Fatal(err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if config.SyslogTLSCACert != nil && len(*config.SyslogTLSCACert) > 0 {
		// Load CA cert
		log.Printf("Loading valid CAs from file %s for syslog output at %s", *config.SyslogTLSCACert, netConn)
		caCert, err := ioutil.ReadFile(*config.SyslogTLSCACert)
		if err != nil {
			log.Fatal(err)
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		tlsConfig.RootCAs = caCertPool
	}

	var err error
	o.outputSocket, err = syslog.DialWithTLSConfig(o.protocol, o.hostnamePort, syslog.LOG_INFO, o.tag, tlsConfig)

	if err != nil {
		return errors.New(fmt.Sprintf("Error connecting to '%s': %s", netConn, err))
	}

	o.markConnected()

	return nil
}

func (o *SyslogOutput) Key() string {
	return o.String()
}

func (o *SyslogOutput) String() string {
	o.RLock()
	defer o.RUnlock()

	return fmt.Sprintf("%s:%s", o.protocol, o.hostnamePort)
}

func (o *SyslogOutput) Statistics() interface{} {
	o.RLock()
	defer o.RUnlock()

	return SyslogStatistics{
		LastOpenTime:       o.connectTime,
		Protocol:           o.protocol,
		RemoteHostnamePort: o.hostnamePort,
		DroppedEventCount:  o.droppedEventCount,
		Connected:          o.connected,
	}
}

func (o *SyslogOutput) markConnected() {
	o.connectTime = time.Now()
	log.Printf("Connected to %s at %s.", o.hostnamePort, o.connectTime)
	o.connected = true
	if o.droppedEventCount != o.droppedEventSinceConnection {
		log.Printf("Dropped %d events since the last reconnection.",
			o.droppedEventCount-o.droppedEventSinceConnection)
		o.droppedEventSinceConnection = o.droppedEventCount
	}
}

func (o *SyslogOutput) closeAndScheduleReconnection() {
	o.Lock()
	defer o.Unlock()

	if o.connected {
		o.outputSocket.Close()
		o.connected = false
	}
	// try reconnecting in 5 seconds
	o.reconnectTime = time.Now().Add(time.Duration(5 * time.Second))

	log.Printf("Lost connection to %s. Will try to reconnect at %s.", o.hostnamePort, o.reconnectTime)
}

func (o *SyslogOutput) output(m string) error {
	if !o.connected {
		// drop this event on the floor...
		atomic.AddInt64(&o.droppedEventCount, 1)
		return nil
	}

	err := o.outputSocket.Info(m)
	if err != nil {
		o.closeAndScheduleReconnection()
	}

	return err
}

func (o *SyslogOutput) Go(messages <-chan string, errorChan chan<- error) error {
	if o.outputSocket == nil {
		return errors.New("Output socket not open")
	}

	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()

		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)

		defer signal.Stop(hup)

		for {
			select {
			case message := <-messages:
				if err := o.output(message); err != nil {
					errorChan <- err
				}

			case <-refreshTicker.C:
				if !o.connected && time.Now().After(o.reconnectTime) {
					err := o.Initialize(o.String())
					if err != nil {
						o.closeAndScheduleReconnection()
					}
				}
			}
		}

	}()

	return nil
}
