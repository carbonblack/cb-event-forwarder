package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
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
	outputFileName string
	outputFile     io.WriteCloser
	gzipOSFilePtr  *os.File
	fileOpenedAt   time.Time

	lastRolledOver time.Time
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

	if config.FileHandlerCompressData != false && !strings.HasSuffix(fileName, ".gz") {
		fileName += ".gz"
	}

	o.outputFileName = fileName
	o.closeFile()
	o.fileOpenedAt = time.Time{}
	o.lastRolledOver = time.Time{}

	fp, err := os.OpenFile(fileName, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		return err
	}

	if config.FileHandlerCompressData != false {
		o.outputFile = gzip.NewWriter(fp)
		o.gzipOSFilePtr = fp
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

		defer o.flushOutput(true)
		defer signal.Stop(hup)
		defer o.closeFile()

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
				log.Println("Received SIGHUP, Rolling over file now.")
				if _, err := o.rollOverFile("2006-01-02T15:04:05"); err != nil {
					errorChan <- err
					return
				}

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
		_, err := o.outputFile.Write(o.bufferOutput.buffer.Bytes())
		// is the error temporary? reopen the file and see...
		if err != nil {
			return err
		}
		o.bufferOutput.buffer.Reset()
		o.bufferOutput.lastFlush = time.Now()
		return nil
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
	basename := filepath.Dir(o.outputFileName)
	newName := fmt.Sprintf("%s.%s", filepath.Base(o.outputFileName),
		o.lastRolledOver.Format(tf))
	newName = filepath.Join(basename, newName)

	o.closeFile()

	log.Printf("Rolling file %s to %s", o.outputFileName, newName)
	err := os.Rename(o.outputFileName, newName)
	if err != nil {
		return "", err
	}

	return newName, o.Initialize(o.outputFileName)
}

func (o *FileOutput) closeFile() {
	if o.outputFile != nil {
		o.outputFile.Close()
		o.outputFile = nil
	}
	if o.gzipOSFilePtr != nil {
		o.gzipOSFilePtr.Close()
		o.gzipOSFilePtr = nil
	}
}
