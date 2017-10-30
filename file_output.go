package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

type BufferOutput struct {
	buffer    bytes.Buffer
	lastFlush time.Time
}

type FileOutput struct {
	outputFileName      string
	outputFileExtension string
	outputFile          io.WriteCloser
	outputGzWriter      *gzip.Writer
	fileOpenedAt        time.Time
	lastRolledOver      time.Time
	sync.RWMutex
	bufferOutput BufferOutput
}

type FileStatistics struct {
	LastOpenTime time.Time `json:"last_open_time"`
	FileName     string    `json:"file_name"`
}

func (o *FileOutput) Statistics() interface{} {
	o.RLock()
	defer o.RUnlock()

	return FileStatistics{LastOpenTime: o.fileOpenedAt, FileName: o.outputFileName}
}

func (o *FileOutput) Key() string {
	o.RLock()
	defer o.RUnlock()

	return fmt.Sprintf("file:%s", o.outputFileName)
}

func (o *FileOutput) Initialize(fileName string) error {
	o.Lock()
	defer o.Unlock()

	o.outputFileName = fileName

	o.fileOpenedAt = time.Time{}
	o.lastRolledOver = time.Now()
	o.closeFile()

	// if the output file already exists, let's roll it over to start from scratch
	fp, err := os.OpenFile(o.outputFileName, os.O_RDWR|os.O_EXCL|os.O_CREATE, 0644)
	if err != nil {
		if os.IsExist(err) {
			// the output file already exists, try to roll it over
			//o.rollOverRename("2006-01-02T15:04:05.000")
			newname , err := o.restartRollOverRename("2006-01-02T15:04:05:000")
			if err == nil {
				o.outputFileName = newname
			}
			// try again
			fp, err = os.OpenFile(o.outputFileName, os.O_RDWR|os.O_EXCL|os.O_CREATE, 0644)
			if err != nil {
				// give up if we still have an error
				return err
			}
		}
	}

	if config.FileHandlerCompressData != false {
		log.Info("File handler configured to compress data")
		o.outputGzWriter = gzip.NewWriter(fp)
		o.outputFile = o.outputGzWriter
	} else {
		o.outputFile = fp
	}

	o.fileOpenedAt = time.Now()
	o.lastRolledOver = time.Now()
	o.bufferOutput.lastFlush = time.Now()

	return nil
}

func (o *FileOutput) Go(messages <-chan string, errorChan chan<- error) error {
	if o.outputFile == nil {
		return errors.New("No output file specified")
	}

	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()

		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)

		term := make(chan os.Signal, 1)
		signal.Notify(term, syscall.SIGTERM)
		signal.Notify(term, syscall.SIGINT)

		defer o.closeFile()
		defer o.flushOutput(true)
		defer signal.Stop(hup)
		defer signal.Stop(term)

		for {

			select {
			case message := <-messages:
				if err := o.output(message); err != nil {
					errorChan <- err
					return
				}

			case <-refreshTicker.C:
				if o.lastRolledOver.Day() != time.Now().Day() {
					if _, err := o.rollOverFile("20060102"); err != nil {
						errorChan <- err
						return
					}
				}
				o.flushOutput(false)

			case <-hup:
				// reopen file
				log.Info("Received SIGHUP, Rolling over file now.")
				if _, err := o.rollOverFile("2006-01-02T15:04:05.000"); err != nil {
					errorChan <- err
					return
				}

			case <-term:
				// handle exit gracefully
				log.Info("Received SIGTERM. Exiting")
				errorChan <- errors.New("SIGTERM received")
				return
			}
		}
	}()

	return nil
}

func (o *FileOutput) String() string {
	o.RLock()
	defer o.RUnlock()

	return fmt.Sprintf("File %s", o.outputFileName)
}

func (o *FileOutput) flushOutput(force bool) error {

	/*
	 * 1000000ns = 1ms
	 */

	if time.Since(o.bufferOutput.lastFlush).Nanoseconds() > 100000000 || force {

		if config.FileHandlerCompressData != false && o.outputGzWriter != nil {

			_, err := o.outputGzWriter.Write(o.bufferOutput.buffer.Bytes())
			o.outputGzWriter.Flush()

			if err != nil {
				return err
			}

			o.bufferOutput.buffer.Reset()
			o.bufferOutput.lastFlush = time.Now()
			return nil

		} else {
			_, err := o.outputFile.Write(o.bufferOutput.buffer.Bytes())

			if err != nil {
				return err
			}

			o.bufferOutput.buffer.Reset()
			o.bufferOutput.lastFlush = time.Now()
			return nil
		}

	}
	return nil
}

func (o *FileOutput) output(s string) error {
	/*
	 * Write to our buffer first
	 */
	o.bufferOutput.buffer.WriteString(s + "\n")
	err := o.flushOutput(false)
	return err
}

func (o *FileOutput) rollOverFile(tf string) (string, error) {
	o.closeFile()

	newName, err := o.rollOverRename(tf)
	if err != nil {
		return "", err
	}

	return newName, o.Initialize(o.outputFileName)
}

func (o *FileOutput) rollOverRename(tf string,) (string, error) {
	var newName string
	if config.FileHandlerCompressData == true {
		fileNameWithoutExtension := strings.TrimSuffix(o.outputFileName, ".gz")
		newName = fileNameWithoutExtension + "." + o.lastRolledOver.Format(tf) + ".gz"
	} else {
		newName = o.outputFileName + "." + o.lastRolledOver.Format(tf)
	}

	log.Infof("Rolling file %s to %s", o.outputFileName, newName)
	if (strings.Contains(newName,".restart")) {
		newName = strings.Replace(newName,".restart","",-1)
	}
	err := os.Rename(o.outputFileName, newName)
	if err != nil {
		return "", err
	} else {
		return newName, nil
	}
}

func (o *FileOutput) restartRollOverRename(tf string) (string, error) {
	var newName string
	if config.FileHandlerCompressData == true {
		fileNameWithoutExtension := strings.TrimSuffix(o.outputFileName, ".gz")
		newName = fileNameWithoutExtension + ".restart." + o.lastRolledOver.Format(tf) + ".gz"
	} else {
		newName = o.outputFileName +  ".restart." + o.lastRolledOver.Format(tf)
	}

	log.Infof("Rolling file %s to %s", o.outputFileName, newName)
	err := os.Rename(o.outputFileName, newName)
	if err != nil {
		return "", err
	} else {
		return newName, nil
	}
}

func (o *FileOutput) closeFile() {
	if o.outputFile != nil {
		o.outputFile.Close()
		o.outputFile = nil
	}

}
