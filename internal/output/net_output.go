package output

import (
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	log "github.com/sirupsen/logrus"
	"net"
	"os"
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

	Config *conf.Configuration
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
func (o *NetOutput) Initialize(netConn string, config *conf.Configuration) error {
	o.Lock()
	defer o.Unlock()

	o.Config = config
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

func (o *NetOutput) close() {
	o.Lock()
	defer o.Unlock()

	if o.connected {
		o.outputSocket.Close()
		o.connected = false
	}

	log.Infof("Lost connection to %s. Closing", o.netConn)
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
		log.Infof("Error writing to netoutput socket: %v", err)
		o.closeAndScheduleReconnection()
	}
	return err
}

func (o *NetOutput) Connect() error {
	var err error
	o.outputSocket, err = net.Dial(o.protocolName, o.remoteHostname)
	if err != nil {
		return err
	}
	o.markConnected()
	return nil
}

func (o *NetOutput) Go(messages <-chan string, errorChan chan<- error, conchan <-chan os.Signal) error {

	o.Connect()
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()
		for {
			if o.connected {
				select {
				case message := <-messages:
					if err := o.output(message); err != nil {
						errorChan <- err
					}
				case <-refreshTicker.C:
					if !o.connected && time.Now().After(o.reconnectTime) {
						log.Infof("close and reschedule due to not being connected and after reconenct time %T %s ", o.connected, o.reconnectTime)
						o.Initialize(o.netConn, o.Config)
						err := o.Connect()
						if err != nil {
							o.closeAndScheduleReconnection()
						}
					}
				case cmsg := <-conchan:
					switch cmsg {
					case syscall.SIGTERM:
						o.close()
						log.Info("Got terminate signal, exiting gracefully")
						return
					case syscall.SIGHUP:
						log.Infof("close and reschedule due to not being connected and after reconenct time %T %s ", o.connected, o.reconnectTime)
						o.Initialize(o.netConn, o.Config)
						err := o.Connect()
						if err != nil {
							o.closeAndScheduleReconnection()
						}
					}
				}
			}
		}
	}()

	return nil
}
