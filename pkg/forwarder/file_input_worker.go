package forwarder

import (
	"github.com/hpcloud/tail"
	log "github.com/sirupsen/logrus"
	"io"
)

type FileConsumer struct {
	fileName string
}

func (f FileConsumer) tailFile(fName string, c chan<- string) error {
	seekInfo := tail.SeekInfo{Offset: 0, Whence: io.SeekEnd}
	t, err := tail.TailFile(fName, tail.Config{Follow: true, ReOpen: true, Poll: true, Location: &seekInfo})
	if err == nil {
		for line := range t.Lines {
			log.Debug(line.Text)
			c <- line.Text
		}
		return nil
	}
	log.Debugf("Error tailing file %s : %v", fName, err)
	return err
}

func NewFileConsumer(fName string) (*FileConsumer, <-chan string, error) {

	consumer := &FileConsumer{fileName: fName}

	c := make(chan string)

	go consumer.tailFile(fName, c)

	return consumer, c, nil
}
