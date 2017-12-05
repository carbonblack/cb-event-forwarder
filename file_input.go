package main

import (
	log "github.com/sirupsen/logrus"
	"io"
	"os"
	"time"
)

type FileConsumer struct {
	fileName string
}

func (f FileConsumer) tailFile(fname string, c chan<- string) {
	file, err := os.Open(fname)
	if err != nil {
		log.Debugf("Failed to open file: %s", fname)
		return
	}
	log.Debugf("Opened file: %s", fname)
	defer file.Close()
	offset, err := file.Seek(0, io.SeekEnd)
	buffer := make([]byte, 1024, 1024)
	for {
		readBytes, err := file.ReadAt(buffer, offset)
		if err != nil {
			if err != io.EOF {
				log.Debugf("Error reading lines: %v", err)
				break
			}
		}
		offset += int64(readBytes)
		if readBytes != 0 {
			s := string(buffer[:readBytes])
			c <- s
		}
		time.Sleep(time.Second)
	}
}

func NewFileConsumer(fName string) (*FileConsumer, <-chan string, error) {

	consumer := &FileConsumer{fileName: fName}

	c := make(chan string)

	go consumer.tailFile(fName, c)

	return consumer, c, nil
}
