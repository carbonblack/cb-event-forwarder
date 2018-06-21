package output

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"github.com/carbonblack/cb-event-forwarder/internal/util"
	log "github.com/sirupsen/logrus"
	"io"
	"os"
	"text/template"
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

func convertFileIntoTemplate(commaSeparateEvents, debugFlag bool, debugStore string, fp *os.File, events chan<- UploadEvent, firstEventTemplate *template.Template, subsequentEventTemplate *template.Template) {
	defer close(events)

	var fileReader io.ReadCloser
	var err error

	if util.IsGzip(fp) {
		fileReader, err = gzip.NewReader(fp)
		if err != nil {
			log.Debugf("Error reading file: %s", err.Error())
			util.MoveFileToDebug(debugFlag, debugStore, fp.Name())
			return
		}
		defer fileReader.Close()
	} else {
		fileReader = fp
	}

	scanner := bufio.NewScanner(fileReader)
	var i int64

	for scanner.Scan() {
		var b bytes.Buffer
		var err error
		eventText := scanner.Text()

		if len(eventText) == 0 {
			// skip empty lines
			continue
		}

		if commaSeparateEvents {
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

		events <- UploadEvent{EventText: eventText, EventSeq: i}
		i++

	}

	if err := scanner.Err(); err != nil {
		log.Debug(err)
	}

}
