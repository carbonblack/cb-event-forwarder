package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"text/template"
	"gopkg.in/h2non/filetype.v1"
	/*"github.com/satori/go.uuid"*/
)


/* This is the Splunk HTTP Event Collector (HEC) implementation of the OutputHandler interface defined in main.go */
type SplunkBehavior struct {
	dest    string
	headers map[string]string

	client *http.Client

    httpPostTemplate  *template.Template
    firstEventTemplate *template.Template
    subsequentEventTemplate *template.Template
}

type SplunkStatistics struct {
	Destination string `json:"destination"`
}

/* Construct the SplunkBehavior object */
func (this *SplunkBehavior) Initialize(dest string) error {
    this.httpPostTemplate = config.HttpPostTemplate
    this.firstEventTemplate = template.Must(template.New("first_event").Parse("{{.}}"))
    this.subsequentEventTemplate = template.Must(template.New("subsequent_event").Parse("{{.}}"))
	this.headers = make(map[string]string)

	this.dest = dest

	/* add authorization token, if applicable */
	if config.SplunkToken != nil{
		this.headers["Authorization"] = fmt.Sprintf("Splunk %s", *config.SplunkToken)
	}

	this.headers["Content-Type"] = *config.HttpContentType

	transport := &http.Transport{
		TLSClientConfig: config.TLSConfig,
	}
	this.client = &http.Client{Transport: transport}

	return nil
}

func (this *SplunkBehavior) String() string {
	return "Splunk HTTP Event Collector " + this.Key()
}

func (this *SplunkBehavior) Statistics() interface{} {
	return SplunkStatistics{
		Destination: this.dest,
	}
}

func (this *SplunkBehavior) Key() string {
	return this.dest
}


func (this *SplunkBehavior) readFromFile(fp *os.File, events chan<- UploadEvent) {

        defer close(events)

        var fileReader io.ReadCloser;

        // decompress file from disk if it's compressed
        header := make([]byte, 261)

        _, err := fp.Read(header)
        if err != nil {
            log.Fatalf("Could not read header information for file: %s", err.Error())
            return
        }
        fp.Seek(0, os.SEEK_SET)

        if filetype.IsMIME(header, "application/gzip") {
            fileReader, err := gzip.NewReader(fp)
            if err != nil {
                // TODO: find a better way to bubble this error up
                log.Fatalf("Error reading file: %s", err.Error())
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

            if config.CommaSeparateEvents {
                if i == 0 {
                    err = this.firstEventTemplate.Execute(&b, eventText)
                } else {
                    err = this.subsequentEventTemplate.Execute(&b, eventText)
                }
                eventText = b.String()
            } else {
                eventText = eventText + "\n"
            }

            if err != nil {
                log.Fatal(err)
            }

            events <- UploadEvent{EventText: eventText, EventSeq: i}
		    i += 1

        }

        if err := scanner.Err(); err != nil {
            log.Fatal(err)
        }

    }

/* This function does a POST of the given event to this.dest. UploadBehavior is called from within its own
   goroutine so we can do some expensive work here. */
func (this *SplunkBehavior) Upload(fileName string, fp *os.File) UploadStatus {
	var err error = nil
	var uploadData UploadData

	/* Initialize the POST */
	reader, writer := io.Pipe()

	uploadData.FileName = fileName
	fileInfo, err := fp.Stat()
	if err == nil {
		uploadData.FileSize = fileInfo.Size()
	}
	uploadData.Events = make(chan UploadEvent)

	request, err := http.NewRequest("POST", this.dest , reader)

	go func() {
		defer writer.Close()

		// spawn goroutine to read from the file
		go this.readFromFile(fp, uploadData.Events)
		this.httpPostTemplate.Execute(writer, uploadData)
	}()

	/* Set the header values of the post */
	for key, value := range this.headers {
		request.Header.Set(key, value)
	}

	/*request.Header.Set("X-Splunk-Request-Channel", uuid.NewV4().String()) */

	/* Execute the POST */
	resp, err := this.client.Do(request)
	if err != nil {
		return UploadStatus{fileName: fileName, result: err}
	}
	defer resp.Body.Close()

	/* Some sort of issue with the POST */
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		errorData := resp.Status + "\n" + string(body)

		return UploadStatus{fileName: fileName,
			result: fmt.Errorf("HTTP request failed: Error code %s", errorData)}
	}
	return UploadStatus{fileName: fileName, result: err}
}
