package main

import (
	"errors"
	"fmt"
	syslog "github.com/RackSec/srslog"
	log "github.com/sirupsen/logrus"
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
		o.connected = false
	}

	connSpecification := strings.SplitN(netConn, ":", 2)

	o.protocol = connSpecification[0]
	o.hostnamePort = connSpecification[1]

	var err error
	o.outputSocket, err = syslog.DialWithTLSConfig(o.protocol, o.hostnamePort, syslog.LOG_INFO, o.tag, config.TLSConfig)

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
	log.Infof("Connected to %s at %s.", o.hostnamePort, o.connectTime)
	o.connected = true
	if o.droppedEventCount != o.droppedEventSinceConnection {
		log.Infof("Dropped %d events since the last reconnection.",
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

	log.Infof("Lost connection to %s. Will try to reconnect at %s.", o.hostnamePort, o.reconnectTime)
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
