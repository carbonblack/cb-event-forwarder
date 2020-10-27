package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"os"
	"text/template"

	log "github.com/sirupsen/logrus"
)

type UploadData struct {
	FileName string
	FileSize int64
	Events   chan UploadEvent
}

type UploadEvent struct {
	EventSeq  int64
	EventText string
}

func convertFileIntoTemplate(fp *os.File, events chan<- UploadEvent, firstEventTemplate, subsequentEventTemplate *template.Template) {
	defer close(events)

	var fileReader io.ReadCloser
	var err error

	if IsGzip(fp) {
		fileReader, err = gzip.NewReader(fp)
		if err != nil {
			log.Debugf("Error reading file: %s", err.Error())
			MoveFileToDebug(fp.Name())
			return
		}
		defer fileReader.Close()
	} else {
		fileReader = fp
	}

	scanner := bufio.NewScanner(fileReader)
	scanner.Buffer(make([]byte, bufio.MaxScanTokenSize), 10*bufio.MaxScanTokenSize)
	var i int64

	for scanner.Scan() {
		var b bytes.Buffer
		var err error
		eventText := scanner.Text()

		if len(eventText) == 0 {
			// skip empty lines
			continue
		}

		if config.CommaSeparateEvents {
			if i == 0 {
				err = firstEventTemplate.Execute(&b, eventText)
			} else {
				err = subsequentEventTemplate.Execute(&b, eventText)
			}
			eventText = b.String()
		} else {
			eventText = eventText + "\n"
		}
		if err != nil {
			log.Debug(err)
		}

		events <- newUploadEvent(i, eventText, config.EventTextAsJsonByteArray)
		i++

	}

	if err := scanner.Err(); err != nil {
		log.Debug(err)
	}

}

// newUploadEvent creates an instance of UploadEvent.
func newUploadEvent(eventSeq int64, eventText string, eventTextAsJsonByteArray bool) UploadEvent {
	// If eventTextAsJsonByteArray is true, eventText will be encoded as a base64-encoded string.
	if eventTextAsJsonByteArray {
		eventText = base64.StdEncoding.EncodeToString([]byte(eventText))
	}
	return UploadEvent{
		EventSeq:  eventSeq,
		EventText: eventText,
	}
}
