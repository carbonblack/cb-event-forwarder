package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type FileOutput struct {
	outputFileName string
	outputFile     *os.File
	fileOpenedAt   time.Time

	lastRolledOver time.Time
	sync.RWMutex
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
	o.close()
	o.fileOpenedAt = time.Time{}
	o.lastRolledOver = time.Time{}

	fp, err := os.OpenFile(fileName, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		return err
	}

	o.outputFile = fp
	o.fileOpenedAt = time.Now()
	o.lastRolledOver = time.Now()

	return nil
}

func (o *FileOutput) Go(messages <-chan string, errorChan chan<- error) error {
	if o.outputFile == nil {
		return errors.New("No output file specified")
	}

	_, err := o.outputFile.Stat()
	if err != nil {
		return err
	}

	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()

		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)

		defer signal.Stop(hup)
		defer o.close()

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

func (o *FileOutput) output(s string) error {
	_, err := o.outputFile.WriteString(s + "\n")
	// is the error temporary? reopen the file and see...
	if err != nil {
		return err
	}
	return nil
}

func (o *FileOutput) rollOverFile(tf string) (string, error) {
	basename := filepath.Dir(o.outputFileName)
	newName := fmt.Sprintf("%s.%s", filepath.Base(o.outputFileName),
		o.lastRolledOver.Format(tf))
	newName = filepath.Join(basename, newName)

	o.close()

	log.Printf("Rolling file %s to %s", o.outputFileName, newName)
	err := os.Rename(o.outputFileName, newName)
	if err != nil {
		return "", err
	}

	return newName, o.Initialize(o.outputFileName)
}

func (o *FileOutput) close() {
	if o.outputFile != nil {
		o.outputFile.Close()
		o.outputFile = nil
	}
}
