package output

import (
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"os"
)

type OutputHandler interface {
	Initialize(string, *conf.Configuration) error
	Go(messages <-chan string, errorChan chan<- error, controlchan <-chan os.Signal) error
	String() string
	Statistics() interface{}
	Key() string
}
