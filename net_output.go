package main

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

type NetOutput struct {
	netConn        string
	remoteHostname string
	protocolName   string
	outputSocket   net.Conn
	addNewline     bool

	connectTime time.Time
	sync.RWMutex
}

type NetStatistics struct {
	LastOpenTime   time.Time `json:"last_open_time"`
	Protocol       string    `json:"connection_protocol"`
	RemoteHostname string    `json:"remote_hostname"`
}

// Initialize() expects a connection string in the following format:
// (protocol):(hostname/IP):(port)
// for example: tcp:destination.server.example.com:512
func (o *NetOutput) Initialize(netConn string) error {
	o.Lock()
	defer o.Unlock()

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
		return errors.New(fmt.Sprintf("Error connecting to '%s': %s", netConn, err))
	}

	o.connectTime = time.Now()

	return nil
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

	return NetStatistics{LastOpenTime: o.connectTime, Protocol: o.protocolName, RemoteHostname: o.remoteHostname}
}

func (o *NetOutput) output(m string) error {
	if o.addNewline {
		m = m + "\r\n"
	}

	_, err := o.outputSocket.Write([]byte(m))
	if err != nil {
		// try to reconnect and send again
		err = o.Initialize(o.netConn)
		if err != nil {
			return err
		}
		_, err = o.outputSocket.Write([]byte(m))
		return err
	}
	return err
}

func (o *NetOutput) Go(messages <-chan string, errorChan chan<- error) error {
	if o.outputSocket == nil {
		return errors.New("Output socket not open")
	}

	go func() {
		for {
			select {
			case message := <-messages:
				if err := o.output(message); err != nil {
					errorChan <- err
					return
				}
			}
		}

	}()

	return nil
}
