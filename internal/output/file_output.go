package output

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/internal/encoder"
	log "github.com/sirupsen/logrus"
	"io"
	"os"
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
	OutputFileName     string
	OutputFile         io.WriteCloser
	OutputFileGzWriter *gzip.Writer
	FileOpenedAt       time.Time
	LastRolledOver     time.Time
	BufferOutput       BufferOutput
	CompressData       bool
	sync.RWMutex
	Encoder encoder.Encoder
}

func NewFileOutputHandler(outputFileName string, e encoder.Encoder) FileOutput {
	f := FileOutput{Encoder: e, CompressData: strings.HasSuffix(outputFileName, ".gz"), OutputFileName: outputFileName}
	f.OpenFileForWriting()
	return f
}

type FileStatistics struct {
	LastOpenTime time.Time `json:"last_open_time"`
	FileName     string    `json:"file_name"`
}

func (o *FileOutput) Statistics() interface{} {
	o.RLock()
	defer o.RUnlock()

	return FileStatistics{LastOpenTime: o.FileOpenedAt, FileName: o.OutputFileName}
}

func (o *FileOutput) Key() string {
	o.RLock()
	defer o.RUnlock()

	return fmt.Sprintf("file:%s", o.OutputFileName)
}

func (o *FileOutput) OpenFileForWriting() error {
	o.Lock()
	defer o.Unlock()

	o.FileOpenedAt = time.Time{}
	o.LastRolledOver = time.Now()
	o.closeFile()

	// if the output file already exists, let's roll it over to start from scratch
	fp, err := os.OpenFile(o.OutputFileName, os.O_RDWR|os.O_EXCL|os.O_CREATE, 0644)
	if err != nil {
		if os.IsExist(err) {
			// the output file already exists, try to roll it over
			o.rollOverRename("2006-01-02T15:04:05.000.restart")

			// try again
			fp, err = os.OpenFile(o.OutputFileName, os.O_RDWR|os.O_EXCL|os.O_CREATE, 0644)
			if err != nil {
				// give up if we still have an error
				return err
			}
		} else {
			// the error is not EEXIST, error out instead
			return err
		}
	}

	if o.CompressData != false {
		log.Info("File handler configured to compress data")
		o.OutputFileGzWriter = gzip.NewWriter(fp)
	}
	o.OutputFile = fp

	o.FileOpenedAt = time.Now()
	o.LastRolledOver = time.Now()
	o.BufferOutput.lastFlush = time.Now()

	return nil
}

func (o *FileOutput) Go(messages <-chan map[string]interface{}, errorChan chan<- error, controlchan <-chan os.Signal, wg sync.WaitGroup) error {
	if o.OutputFile == nil {
		return errors.New("No output file specified")
	}

	go func() {
		wg.Add(1)
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()
		defer o.closeFile()
		defer o.flushOutput(true)
		defer wg.Done()
		for {
			//log.Infof("FILE HANDLER SELECT LOOP has control chan %s!", controlchan)
			select {
			case message := <-messages:
				if encodedMsg, err := o.Encoder.Encode(message); err == nil {
					if err := o.output(encodedMsg); err != nil {
						errorChan <- err
						return
					}
				} else {
					errorChan <- err
				}

			case <-refreshTicker.C:
				if o.LastRolledOver.Day() != time.Now().Day() {
					if _, err := o.rollOverFile("20060102"); err != nil {
						errorChan <- err
						return
					}
				}
				o.flushOutput(false)

			case cmsg := <-controlchan:
				log.Infof("Fileoutput got %s over controlchan", cmsg)
				switch cmsg {
				case syscall.SIGHUP:
					// reopen file
					log.Info("Received SIGHUP, Rolling over file now.")
					if _, err := o.rollOverFile("2006-01-02T15:04:05.000"); err != nil {
						errorChan <- err
						return
					}
				case syscall.SIGTERM, syscall.SIGINT:
					// handle exit gracefully
					log.Info("Received a signal to terminate. Exiting gracefully")
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

	return fmt.Sprintf("File %s", o.OutputFileName)
}

func (o *FileOutput) flushOutput(force bool) error {

	/*
	 * 1000000ns = 1ms
	 */

	if time.Since(o.BufferOutput.lastFlush).Nanoseconds() > 100000000 || force {

		if o.CompressData && o.OutputFileGzWriter != nil {

			_, err := o.OutputFileGzWriter.Write(o.BufferOutput.buffer.Bytes())
			o.OutputFileGzWriter.Flush()

			if err != nil {
				return err
			}

			o.BufferOutput.buffer.Reset()
			o.BufferOutput.lastFlush = time.Now()
			return nil

		} else if o.OutputFile != nil {
			_, err := o.OutputFile.Write(o.BufferOutput.buffer.Bytes())
			if err != nil {
				return err
			}
			o.BufferOutput.buffer.Reset()
			o.BufferOutput.lastFlush = time.Now()
			return nil
		}
	}
	return nil
}

func (o *FileOutput) output(s string) error {
	/*
	 * Write to our buffer first
	 */
	o.BufferOutput.buffer.WriteString(s + "\n")
	err := o.flushOutput(false)
	return err
}

func (o *FileOutput) rollOverFile(tf string) (string, error) {
	o.closeFile()

	log.Infof("Rolling over file with format string: %s", tf)

	newName, err := o.rollOverRename(tf)
	if err != nil {
		return "", err
	}

	return newName, o.OpenFileForWriting()
}

func (o *FileOutput) rollOverRename(tf string) (string, error) {
	var newName string
	if o.CompressData == true {
		fileNameWithoutExtension := strings.TrimSuffix(o.OutputFileName, ".gz")
		newName = fileNameWithoutExtension + "." + o.LastRolledOver.Format(tf) + ".gz"
	} else {
		newName = o.OutputFileName + "." + o.LastRolledOver.Format(tf)
	}

	log.Infof("Rolling file %s to %s", o.OutputFileName, newName)
	err := os.Rename(o.OutputFileName, newName)
	if err != nil {
		return "", err
	}
	return newName, nil

}

func (o *FileOutput) closeFile() {
	if o.OutputFileGzWriter != nil {
		o.flushOutput(true)
		o.OutputFileGzWriter.Close()
		o.OutputFileGzWriter = nil
	} else if o.OutputFile != nil {
		o.flushOutput(true)
		o.OutputFile.Close()
		o.OutputFile = nil
	}

}
