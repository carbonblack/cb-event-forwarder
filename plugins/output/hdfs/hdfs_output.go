package main

import (
	"encoding/json"
	"errors"
	"fmt"
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"github.com/carbonblack/cb-event-forwarder/internal/output"
	"github.com/colinmarc/hdfs"
	log "github.com/sirupsen/logrus"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type HdfsOutput struct {
	hdfsServer        string
	hdfsClient        *hdfs.Client
	hdfsPath          string
	droppedEventCount int64
	eventSentCount    int64
	deliveryChan      chan DeliveryMessage
	sync.RWMutex
}

type HdfsStatistics struct {
	DroppedEventCount int64 `json:"dropped_event_count"`
	EventSentCount    int64 `json:"event_sent_count"`
}

type DeliveryMessage struct {
	Error          error
	SuccessMessage string
}

func (o *HdfsOutput) Initialize(unused string, config *conf.Configuration) error {

	o.Lock()
	defer o.Unlock()

	hdfsServer, err := config.GetString("plugin", "hdfs_server")
	if err != nil {
		log.Infof("Failed to create HDFS client : %s\n", err)
		return err
	} else {
		o.hdfsServer = hdfsServer
	}
	hdfsPath, err := config.GetString("plugin", "hdfs_path")
	if err != nil {
		log.Infof("Failed to get HDFS path : %v", err)
		o.hdfsPath = "/"
	} else {
		o.hdfsPath = hdfsPath
	}

	hdfsClient, err := hdfs.New(o.hdfsServer)
	if err == nil {
		o.hdfsClient = hdfsClient
	} else {
		log.Infof("Failed to create HDFS client %v", err)
		return err
	}

	o.deliveryChan = make(chan DeliveryMessage)

	log.Infof("Created HDFS client %v\n", o.hdfsClient)

	return nil
}

func (o *HdfsOutput) Go(messages <-chan string, errorChan chan<- error, controlchan <-chan os.Signal) error {
	stoppubchan := make(chan struct{}, 1)
	go func() {

		for {
			select {
			case message := <-messages:
				var parsedMsg map[string]interface{}
				json.Unmarshal([]byte(message), &parsedMsg)
				t := parsedMsg["type"]
				if typeString, ok := t.(string); ok {
					o.output(typeString, message)
				}
			case <-stoppubchan:
				log.Infof("Got stop message, exiting hdfs output goroutine")
				return
			}
		}
	}()

	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()

		for {
			select {
			case m := <-o.deliveryChan:
				if m.Error != nil {
					log.Infof("Delivery failed: %v\n", m.Error)
					atomic.AddInt64(&o.droppedEventCount, 1)
					errorChan <- m.Error
				} else {
					log.Infof("Delivered message to HDFS: %s", m.SuccessMessage)
					atomic.AddInt64(&o.eventSentCount, 1)
				}
			case cmsg := <-controlchan:
				switch cmsg {
				case syscall.SIGTERM, syscall.SIGINT:
					// handle exit gracefully
					log.Info("Received SIGTERM. Exiting")
					stoppubchan <- struct{}{}
					return
				}

			}
		}
	}()

	return nil
}

func (o *HdfsOutput) Statistics() interface{} {
	o.RLock()
	defer o.RUnlock()
	return HdfsStatistics{DroppedEventCount: o.droppedEventCount, EventSentCount: o.eventSentCount}
}

func (o *HdfsOutput) String() string {
	o.RLock()
	defer o.RUnlock()

	return fmt.Sprintf("HDFS Server %s", o.hdfsServer)
}

func (o *HdfsOutput) Key() string {
	o.RLock()
	defer o.RUnlock()
	return fmt.Sprintf("HDFS Server:%s", o.hdfsServer)
}

func (o *HdfsOutput) output(fn, m string) {
	writeto := path.Join(o.hdfsPath, fn)
	writer, err := o.hdfsClient.Create(path.Join(o.hdfsPath, fn))
	if err == nil {
		_, err := writer.Write([]byte(m))
		if err == nil {
			o.deliveryChan <- DeliveryMessage{Error: nil, SuccessMessage: fmt.Sprintf("Succesfully delivered to %s", writeto)}
		} else {
			log.Warnf("Error writing to HDFS %v", err)
			o.deliveryChan <- DeliveryMessage{Error: errors.New(fmt.Sprintf("Error writing to %s %v", writeto, err)), SuccessMessage: ""}
		}
	} else {
		log.Warnf("Error creating creating new writer for HDFS output %v", err)
	}

}

func GetOutputHandler() output.OutputHandler {
	return &HdfsOutput{}
}
