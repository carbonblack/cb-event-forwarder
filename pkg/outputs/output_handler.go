package outputs

import (
	log "github.com/sirupsen/logrus"
	"os"
	"sync"
	"syscall"
	"time"
)

type Output interface {
	Go(messages <-chan string, signalChan <-chan os.Signal, exitCond *sync.Cond) error
	OutputKeys
	OutputInitializer
}

type OutputInitializer interface {
	Initialize(string) error
}

type OutputKeys interface {
	String() string
	Statistics() interface{}
	Key() string
}

type OutputHandler interface {
	Start() error
	HandleMessage(message string) error
	HandleHup() error
	HandleTick() error
	ExitCleanup()
	HandleTerm()
	OutputKeys
	OutputInitializer
}

type BaseOutput struct {
	OutputHandler
}

func (baseOutputHandler *BaseOutput) Go(messages <-chan string, signalChan <-chan os.Signal, exitCond *sync.Cond) error {
	err := baseOutputHandler.Start()
	if err != nil {
		return err
	}
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)

		defer exitCond.Signal()
		defer baseOutputHandler.ExitCleanup()
		defer refreshTicker.Stop()

		for {
			select {
			case message := <-messages:
				err := baseOutputHandler.HandleMessage(message)
				if err != nil {
					log.Errorf("%s", err)
				}
			case signal := <-signalChan:
				switch signal {
				case syscall.SIGHUP:
					// flush to S3 immediately
					log.Infof("Received SIGHUP, sending data to %s immediately.", baseOutputHandler.String())
					err := baseOutputHandler.HandleHup()
					if err != nil {
						log.Errorf("%s", err)
					}
				case syscall.SIGTERM, syscall.SIGINT:
					// handle exit gracefully
					baseOutputHandler.HandleTerm()
					log.Info("Received SIGTERM. Exiting")
					return
				}
			case <-refreshTicker.C:
				err := baseOutputHandler.HandleTick()
				if err != nil {
					log.Errorf("%s", err)
				}
			}
		}
	}()
	return nil
}
