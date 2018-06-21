package output

import (
	"crypto/tls"
	"fmt"
	syslog "github.com/RackSec/srslog"
	"github.com/carbonblack/cb-event-forwarder/internal/encoder"
	log "github.com/sirupsen/logrus"
	"os"
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
	tls                         *tls.Config

	Encoder encoder.Encoder

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
func NewSyslogOutput(netConn string, tls *tls.Config, e encoder.Encoder) (SyslogOutput, error) {
	temp := SyslogOutput{tls: tls, Encoder: e}

	connSpecification := strings.SplitN(netConn, ":", 2)

	temp.protocol = connSpecification[0]
	temp.hostnamePort = connSpecification[1]

	return temp, nil
}

func (o *SyslogOutput) Connect() error {
	var err error
	o.outputSocket, err = syslog.DialWithTLSConfig(o.protocol, o.hostnamePort, syslog.LOG_INFO, o.tag, o.tls)
	if err != nil {
		return err
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

func (o *SyslogOutput) close() {
	o.Lock()
	defer o.Unlock()

	if o.connected {
		o.outputSocket.Close()
		o.connected = false
	}
	log.Infof("Closing connection to %s..", o.hostnamePort)
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
		atomic.AddInt64(&o.droppedEventCount, 1)
	}

	return err
}

func (o *SyslogOutput) Go(messages <-chan map[string]interface{}, errorChan chan<- error, controlchan <-chan os.Signal, wg sync.WaitGroup) error {
	o.Connect()
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()
		defer wg.Done()
		for {
			select {
			case message := <-messages:
				if encodedMsg, err := o.Encoder.Encode(message); err == nil {
					if err := o.output(encodedMsg); err != nil {
						errorChan <- err
						return
					}
				} else {
					errorChan <- err
				}
			case <-refreshTicker.C:
				if !o.connected && time.Now().After(o.reconnectTime) {
					err := o.Connect()
					if err != nil {
						o.closeAndScheduleReconnection()
					}
				}
			case cmsg := <-controlchan:
				switch cmsg {
				case syscall.SIGTERM:
					log.Info("Term signal received...exiting gracefully")
					o.close()
					return
				}
			}
		}
	}()

	return nil
}
