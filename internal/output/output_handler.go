package output

import conf "github.com/carbonblack/cb-event-forwarder/internal/config"

type OutputHandler interface {
	Initialize(string, *conf.Configuration) error
	Go(messages <-chan string, errorChan chan<- error, stopchan <-chan struct{}) error
	String() string
	Statistics() interface{}
	Key() string
}
