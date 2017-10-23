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
	outputFileExtension string
	outputFile     io.WriteCloser
	outputGzWriter *gzip.Writer
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

	// pull "extension" from file (assuming it's json or txt), adding '.gz' if we are gzipping the output file.
	o.outputFileExtension = filepath.Ext(fileName)
	o.outputFileName = strings.TrimSuffix(fileName, o.outputFileExtension)

	if config.FileHandlerCompressData != false && strings.Contains(o.outputFileExtension,".gz") == false {
		o.outputFileExtension = o.outputFileExtension + ".gz"
	}

	o.outputFileName = fileName
	o.closeFile()
	o.fileOpenedAt = time.Time{}
	o.lastRolledOver = time.Time{}

	// if the output file already exists, let's roll it over to start from scratch
	fp, err := os.OpenFile(o.outputFileName + o.outputFileExtension, os.O_RDWR|os.O_EXCL|os.O_CREATE, 0644)
	if err != nil {
		return err
	}

	if config.FileHandlerCompressData != false {
		log.Println("File handler configured to compress data")
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

		log.Printf("Writing to bufferoutput %d", o.bufferOutput.buffer.Len())

		if config.FileHandlerCompressData != false {

			_, err := o.outputGzWriter.Write(o.bufferOutput.buffer.Bytes())
			o.outputGzWriter.Flush()

			if err != nil {
				log.Println("COMPRESSED Writing to bufferoutput failed")
				return err
			}

			log.Println("COMPRESSED Writing to buffer output did not fail")
			o.bufferOutput.buffer.Reset()
			o.bufferOutput.lastFlush = time.Now()
			return nil

		} else {
			_, err := o.outputFile.Write(o.bufferOutput.buffer.Bytes())

			if err != nil {
				log.Println("Writing to bufferoutput failed")
				return err
			}

			log.Println("Writing to buffer output did not fail")
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
	log.Printf("WRiting to buffer: %s ",s)
	o.bufferOutput.buffer.WriteString(s + "\n")
	err := o.flushOutput(false)
	return err
}

func (o *FileOutput) rollOverFile(tf string) (string, error) {
	basename := filepath.Dir(o.outputFileName)

	var newName string

	if (config.FileHandlerCompressData != false){
		newName = fmt.Sprintf("%s.%s.%s", filepath.Base(o.outputFileName),
			o.lastRolledOver.Format(tf), o.outputFileExtension)
	} else {
		newName = fmt.Sprintf("%s.%s.%s", filepath.Base(o.outputFileName),
			o.lastRolledOver.Format(tf), o.outputFileExtension)
	}
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
	log.Printf("Closing FILE!")
	if o.outputFile != nil {
		o.outputFile.Close()
		o.outputFile = nil
	}

}
