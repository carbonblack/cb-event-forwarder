package output

import (
	"github.com/carbonblack/cb-event-forwarder/internal/encoder"
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
	NetConn        string
	RemoteHostname string
	ProtocolName   string
	outputSocket   net.Conn
	AddNewline     bool

	connectTime                 time.Time
	reconnectTime               time.Time
	connected                   bool
	droppedEventCount           int64
	droppedEventSinceConnection int64
	sync.RWMutex
	Encoder encoder.Encoder
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
func NewNetOutputHandler(netConn string, e encoder.Encoder) (NetOutput, error) {
	temp := NetOutput{Encoder: e, NetConn: netConn}

	connSpecification := strings.SplitN(netConn, ":", 2)

	temp.ProtocolName = connSpecification[0]
	temp.RemoteHostname = connSpecification[1]

	if strings.HasPrefix(temp.ProtocolName, "tcp") {
		temp.AddNewline = true
	}

	return temp, nil
}

func (o *NetOutput) markConnected() {
	o.connectTime = time.Now()
	log.Infof("Connected to %s at %s.", o.NetConn, o.connectTime)
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

	log.Infof("Lost connection to %s. Will try to reconnect at %s.", o.NetConn, o.reconnectTime)
}

func (o *NetOutput) close() {
	o.Lock()
	defer o.Unlock()

	if o.connected {
		o.outputSocket.Close()
		o.connected = false
	}

	log.Infof("Lost connection to %s. Closing", o.NetConn)
}

func (o *NetOutput) Key() string {
	o.RLock()
	defer o.RUnlock()

	return o.NetConn
}

func (o *NetOutput) String() string {
	o.RLock()
	defer o.RUnlock()

	return o.NetConn
}

func (o *NetOutput) Statistics() interface{} {
	o.RLock()
	defer o.RUnlock()

	return NetStatistics{
		LastOpenTime:      o.connectTime,
		Protocol:          o.ProtocolName,
		RemoteHostname:    o.RemoteHostname,
		DroppedEventCount: o.droppedEventCount,
		Connected:         o.connected,
	}
}

func (o *NetOutput) output(m string) error {
	if o.AddNewline {
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
	o.outputSocket, err = net.Dial(o.ProtocolName, o.RemoteHostname)
	if err != nil {
		return err
	}
	o.markConnected()
	return nil
}

func (o *NetOutput) Go(messages <-chan map[string]interface{}, errorChan chan<- error, conchan <-chan os.Signal) error {

	o.Connect()
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()
		for {
			if o.connected {
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
						log.Infof("close and reschedule due to not being connected and after reconenct time %T %s ", o.connected, o.reconnectTime)
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
					}
				}
			}
		}
	}()

	return nil
}
