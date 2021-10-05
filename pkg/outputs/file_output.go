package outputs

import (
	"bytes"
	"errors"
	"fmt"
	. "github.com/carbonblack/cb-event-forwarder/pkg/config"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
)

type BufferOutput struct {
	buffer    bytes.Buffer
	lastFlush time.Time
}

type FileOutput struct {
	Config              *Configuration
	outputFileName      string
	outputFileExtension string
	outputFile          io.WriteCloser
	outputGzWriter      FlushableWriteCloser
	fileOpenedAt        time.Time
	lastRolledOver      time.Time
	sync.RWMutex
	bufferOutput BufferOutput
}

func NewFileOutputFromConfig(cfg *Configuration) *FileOutput {
	return &FileOutput{Config: cfg}
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
	o.outputFileExtension = o.Config.FileExtensionForCompressionType()
	// if the output file already exists, let's roll it over to start from scratch
	fp, err := os.OpenFile(o.outputFileName, os.O_RDWR|os.O_EXCL|os.O_CREATE, 0644)
	if err != nil {
		if os.IsExist(err) {
			// the output file already exists, try to roll it over
			o.rollOverRename("2006-01-02T15:04:05.000.restart")

			// try again
			fp, err = os.OpenFile(o.outputFileName, os.O_RDWR|os.O_EXCL|os.O_CREATE, 0644)
			if err != nil {
				// give up if we still have an error
				return err
			}
		} else {
			// the error is not EEXIST, error out instead
			return err
		}
	}

	if o.Config.FileHandlerCompressData != false {
		log.Info("File handler configured to compress data")
		o.outputGzWriter, _ = o.Config.WrapWriterWithCompressionSettings(fp)
	}
	o.outputFile = fp

	o.fileOpenedAt = time.Now()
	o.lastRolledOver = time.Now()
	o.bufferOutput.lastFlush = time.Now()

	return nil
}

func (o *FileOutput) Go(messages <-chan string, signalChan <-chan os.Signal, exitCond *sync.Cond) error {
	if o.outputFile == nil {
		return errors.New("No output file specified")
	}

	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)

		defer SignalExitCond(exitCond)
		defer o.closeFile()
		defer o.flushOutput(true)
		defer refreshTicker.Stop()

		for {

			select {
			case message := <-messages:
				if err := o.output(message); err != nil && !o.Config.DryRun {
					log.Errorf("Fatal error %s", err)
					return
				}

			case <-refreshTicker.C:
				if o.lastRolledOver.Day() != time.Now().Day() {
					if _, err := o.rollOverFile("20060102"); err != nil {
						log.Errorf("Error rolling file %s", err)
						return
					}
				}
				o.flushOutput(false)

			case signal := <-signalChan:
				switch signal {
				case syscall.SIGHUP:
					// reopen file
					log.Info("Received SIGHUP, Rolling over file now.")
					if _, err := o.rollOverFile("2006-01-02T15:04:05.000"); err != nil {
						log.Errorf("Error rolling file %s", err)
						return
					}

				case syscall.SIGTERM, syscall.SIGINT:
					// handle exit gracefully
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

		if o.Config.FileHandlerCompressData && o.outputGzWriter != nil {

			_, err := o.outputGzWriter.Write(o.bufferOutput.buffer.Bytes())
			o.outputGzWriter.Flush()

			if err != nil {
				return err
			}

			o.bufferOutput.buffer.Reset()
			o.bufferOutput.lastFlush = time.Now()
			return nil

		} else if o.outputFile != nil {
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

func (o *FileOutput) rollOverRename(tf string) (string, error) {
	var newName string
	if o.Config.FileHandlerCompressData == true {
		fileNameWithoutExtension := strings.TrimSuffix(o.outputFileName, o.outputFileExtension)
		newName = fileNameWithoutExtension + "." + o.lastRolledOver.Format(tf) + o.outputFileExtension
	} else {
		newName = o.outputFileName + "." + o.lastRolledOver.Format(tf)
	}

	log.Infof("Rolling file %s to %s", o.outputFileName, newName)
	err := os.Rename(o.outputFileName, newName)
	if err != nil {
		return "", err
	}
	return newName, nil

}

func (o *FileOutput) closeFile() {
	if o.outputGzWriter != nil {
		o.flushOutput(true)
		o.outputGzWriter.Close()
		o.outputGzWriter = nil
	}
	if o.outputFile != nil {
		o.flushOutput(true)
		log.Debugf("Closing file %s", o.outputFileName)
		o.outputFile.Close()
		o.outputFile = nil
	}

}
