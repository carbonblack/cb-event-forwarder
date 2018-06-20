package main

import (
	"encoding/json"
	"errors"
	"fmt"
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

type WrappedHDFSClient interface {
  	Create(name string) (* hdfs.FileWriter, error)
}

type HdfsOutput struct {
	hdfsServer        string
	HDFSClient        WrappedHDFSClient
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

func NewHDFSOutputFromCFg(cfg  map[interface{}] interface{}) (HdfsOutput , error) {

	ho := HdfsOutput{}

	hdfsServer, ok := cfg["hdfs_server"].(string)
	if !ok {
		return ho,errors.New("Not hdsf_server speficied")
	} else {
		ho.hdfsServer = hdfsServer
	}

	hdfsPath, ok := cfg["hdfs_path"].(string)
	if !ok {
		log.Warnf("Failed to get HDFS path")
		ho.hdfsPath = "/"
	} else {
		ho.hdfsPath = hdfsPath
	}

	hdfsClient, err := hdfs.New(ho.hdfsServer)
	if err == nil {
		ho.HDFSClient = hdfsClient
	} else {
		log.Infof("Failed to create HDFS client %v", err)
		return ho,err
	}

	ho.deliveryChan = make(chan DeliveryMessage)

	log.Infof("Created HDFS client %v\n", ho.HDFSClient)

	return ho,nil
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
	writer, err := o.HDFSClient.Create(path.Join(o.hdfsPath, fn))
	if err == nil && writer != nil {
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

func GetOutputHandler(cfg map[interface{}] interface{}) (output.OutputHandler, error) {
	ho,err := GetOutputHandler(cfg)
	return ho,err
}
