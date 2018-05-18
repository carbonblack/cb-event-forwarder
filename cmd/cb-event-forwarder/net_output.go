package main

import (
	"errors"
	"fmt"
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	log "github.com/sirupsen/logrus"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type NetOutput struct {
	netConn        string
	remoteHostname string
	protocolName   string
	outputSocket   net.Conn
	addNewline     bool

	connectTime                 time.Time
	reconnectTime               time.Time
	connected                   bool
	droppedEventCount           int64
	droppedEventSinceConnection int64

	sync.RWMutex

	config conf.Configuration
}

type NetStatistics struct {
	LastOpenTime      time.Time `json:"last_open_time"`
	Protocol          string    `json:"connection_protocol"`
	RemoteHostname    string    `json:"remote_hostname"`
	DroppedEventCount int64     `json:"dropped_event_count"`
	Connected         bool      `json:"connected"`
}

// Initialize() expects a connection string in the following format:
// (protocol):(hostname/IP):(port)
// for example: tcp:destination.server.example.com:512
func (o *NetOutput) Initialize(netConn string, config conf.Configuration) error {
	o.Lock()
	defer o.Unlock()

	o.config = config
	if o.connected {
		o.outputSocket.Close()
	}

	o.netConn = netConn

	connSpecification := strings.SplitN(netConn, ":", 2)

	o.protocolName = connSpecification[0]
	o.remoteHostname = connSpecification[1]

	if strings.HasPrefix(o.protocolName, "tcp") {
		o.addNewline = true
	}

	var err error
	o.outputSocket, err = net.Dial(o.protocolName, o.remoteHostname)

	if err != nil {
		return fmt.Errorf("Error connecting to '%s': %s", netConn, err)
	}

	o.markConnected()

	return nil
}

func (o *NetOutput) markConnected() {
	o.connectTime = time.Now()
	log.Infof("Connected to %s at %s.", o.netConn, o.connectTime)
	o.connected = true
	if o.droppedEventCount != o.droppedEventSinceConnection {
		log.Infof("Dropped %d events since the last reconnection.",
			o.droppedEventCount-o.droppedEventSinceConnection)
		o.droppedEventSinceConnection = o.droppedEventCount
	}
}

func (o *NetOutput) closeAndScheduleReconnection() {
	o.Lock()
	defer o.Unlock()

	if o.connected {
		o.outputSocket.Close()
		o.connected = false
	}

	// try reconnecting in 5 seconds
	o.reconnectTime = time.Now().Add(time.Duration(5 * time.Second))

	log.Infof("Lost connection to %s. Will try to reconnect at %s.", o.netConn, o.reconnectTime)
}

func (o *NetOutput) Key() string {
	o.RLock()
	defer o.RUnlock()

	return o.netConn
}

func (o *NetOutput) String() string {
	o.RLock()
	defer o.RUnlock()

	return o.netConn
}

func (o *NetOutput) Statistics() interface{} {
	o.RLock()
	defer o.RUnlock()

	return NetStatistics{
		LastOpenTime:      o.connectTime,
		Protocol:          o.protocolName,
		RemoteHostname:    o.remoteHostname,
		DroppedEventCount: o.droppedEventCount,
		Connected:         o.connected,
	}
}

func (o *NetOutput) output(m string) error {
	if o.addNewline {
		m = m + "\r\n"
	}

	if !o.connected {
		// drop this event on the floor...
		atomic.AddInt64(&o.droppedEventCount, 1)
		return nil
	}

	_, err := o.outputSocket.Write([]byte(m))
	if err != nil {
		o.closeAndScheduleReconnection()
	}
	return err
}

func (o *NetOutput) Go(messages <-chan string, errorChan chan<- error) error {
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
					err := o.Initialize(o.netConn, o.config)
					if err != nil {
						o.closeAndScheduleReconnection()
					}
				}
			}
		}

	}()

	return nil
}
