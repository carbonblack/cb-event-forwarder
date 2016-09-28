package main

import (
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"time"
	"text/template"
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"fmt"
	"strings"
)

/* This is the HTTP implementation of the OutputHandler interface defined in main.go */
type HttpBehavior struct {
	dest              string
	eventSentAt       time.Time
	headers           map[string]string
	ssl               bool

	lastHttpError     string
	lastHttpErrorTime time.Time
	httpErrors        int64

	client            *http.Client

	httpPostTemplate  *template.Template
	firstEventTemplate *template.Template
	subsequentEventTemplate *template.Template
}

type HttpStatistics struct {
	LastSendTime  time.Time `json:"last_send_time"`
	Destination   string    `json:"destination"`
	HttpErrors    int64     `json:"http_errors"`
	LastErrorTime time.Time `json:"last_error_time"`
	LastErrorText string    `json:"last_error_text"`
}

/* Construct the HttpBehavior object */
func (this *HttpBehavior) Initialize(dest string) error {
	this.httpPostTemplate = config.HttpPostTemplate
	this.firstEventTemplate = template.Must(template.New("first_event").Parse(`{{.}}`))
	this.subsequentEventTemplate = template.Must(template.New("subsequent_event").Parse("\n, {{.}}"))

	this.headers = make(map[string]string)

	// TODO: fix
	parts := strings.SplitN(dest, ":", 2)
	this.dest = parts[1]

	/* add authorization token, if applicable */
	if config.HttpAuthorizationToken != nil {
		this.headers["Authorization"] = *config.HttpAuthorizationToken
	}

	this.headers["Content-Type"] = *config.HttpContentType

	/* ssl verification is enabled by default */
	// TODO: merge this with the Syslog TLS configuration in syslog_output.go
	if this.ssl {
		this.client = &http.Client{}
	} else { /* disable ssl verification */
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		this.client = &http.Client{Transport: transport}
	}

	this.eventSentAt = time.Time{}
	return nil
}

func (this *HttpBehavior) String() string {
	return "HTTP POST " + this.Key()
}

func (this *HttpBehavior) Statistics() interface{} {
	return HttpStatistics{
		LastSendTime:  this.eventSentAt,
		Destination:   this.dest,
		LastErrorTime: this.lastHttpErrorTime,
		LastErrorText: this.lastHttpError,
		HttpErrors:    this.httpErrors,
	}
}

func (this *HttpBehavior) Key() string {
	return this.dest
}

type UploadData struct {
	FileName string
	FileSize int64
	Events chan UploadEvent
}

type UploadEvent struct {
	EventSeq int64
	EventText string
}

func (this *HttpBehavior) readFromFile(fp *os.File, events chan <- UploadEvent) {
	defer close(events)

	scanner := bufio.NewScanner(fp)
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
func (this *HttpBehavior) UploadBehavior(fileName string, fp *os.File) UploadStatus {
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

	request, err := http.NewRequest("POST", this.dest, reader)

	go func() {
		// spawn goroutine to read from the file
		go this.readFromFile(fp, uploadData.Events)

		this.httpPostTemplate.Execute(writer, uploadData)
		writer.Close()
	}()

	/* Set the header values of the post */
	for key, value := range this.headers {
		request.Header.Set(key, value)
	}

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

		this.httpErrors += 1
		this.lastHttpError = errorData
		this.lastHttpErrorTime = time.Now()

		log.Printf("%s\n", errorData)

		return UploadStatus{fileName: fileName,
			result: fmt.Errorf("Error: HTTP request failed with status: %d", resp.StatusCode)}
	}

	fmt.Println("response Status:", resp.Status)
	fmt.Println("response Headers:", resp.Header)
	body, _ := ioutil.ReadAll(resp.Body)
	fmt.Println("response Body:", string(body))

	this.eventSentAt = time.Now()
	return UploadStatus{fileName: fileName, result: err}
}
