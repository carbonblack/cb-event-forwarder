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
	"os/signal"
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

func (o *HdfsOutput) Go(messages <-chan string, errorChan chan<- error) error {
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()

		hup := make(chan os.Signal, 1)

		signal.Notify(hup, syscall.SIGHUP)

		defer signal.Stop(hup)

		for {
			select {
			case message := <-messages:
				var parsedMsg map[string]interface{}

				json.Unmarshal([]byte(message), &parsedMsg)
				t := parsedMsg["type"]
				if typeString, ok := t.(string); ok {
					o.output(typeString, message)
				}
			case m := <-o.deliveryChan:
				if m.Error != nil {
					log.Infof("Delivery failed: %v\n", m.Error)
					atomic.AddInt64(&o.droppedEventCount, 1)
					errorChan <- m.Error
				} else {
					log.Infof("Delivered message to HDFS: %s", m.SuccessMessage)
					atomic.AddInt64(&o.eventSentCount, 1)
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

func main() {
	messages := make(chan string)
	errors := make(chan error)
	var hdfsOutput output.OutputHandler = &HdfsOutput{}
	c, _ := conf.ParseConfig(os.Args[1])
	hdfsOutput.Initialize("", c)
	go func() {
		hdfsOutput.Go(messages, errors)
	}()
	messages <- "{\"type\":\"Lol\"}"
	log.Infof("%v", <-errors)
}
