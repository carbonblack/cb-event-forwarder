package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"text/template"
	"time"
	log "github.com/sirupsen/logrus"
)

// HTTPPostTemplate for Humio HEC format, defined in config.go
type HumioBehavior struct {
	dest    string
	headers map[string]string

	client *http.Client

	HTTPPostTemplate        *template.Template
	firstEventTemplate      *template.Template
	subsequentEventTemplate *template.Template
}

type HumioStatistics struct {
	Destination string `json:"destination"`
}

/* Construct the syslog_output.go object */
func (this *HumioBehavior) Initialize(dest string) error {
	this.HTTPPostTemplate = config.HTTPPostTemplate
	this.firstEventTemplate = template.Must(template.New("first_event").Parse("{{.}}"))
	this.subsequentEventTemplate = template.Must(template.New("subsequent_event").Parse("{{.}}"))
	this.headers = make(map[string]string)

	this.dest = dest

	/* Humio auth token, must always be set */
	log.Info("Is humio auth none?")
	if config.HumioToken != nil {
		this.headers["Authorization"] = fmt.Sprintf("Bearer %s", *config.HumioToken)
		log.Info(this.headers)
	}

	this.headers["Content-Type"] = *config.HTTPContentType

	transport := &http.Transport{
		TLSClientConfig:     config.TLSConfig,
		Dial:                (&net.Dialer{Timeout: 5 * time.Second}).Dial,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	this.client = &http.Client{
		Transport: transport,
		Timeout:   120 * time.Second, // This should not that long, ever. Configurable?
	}

	return nil
}

func (this *HumioBehavior) String() string {
	return "Humio HEC " + this.Key()
}

func (this *HumioBehavior) Statistics() interface{} {
	return HumioStatistics{
		Destination: this.dest,
	}
}

func (this *HumioBehavior) Key() string {
	return this.dest
}

/* This function does a POST of the given event to this.dest. UploadBehavior is called from within its own
   goroutine so we can do some expensive work here. */
func (this *HumioBehavior) Upload(fileName string, fp *os.File) UploadStatus {
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
		defer fp.Close()
		defer writer.Close()

		// spawn goroutine to read from the file
		go convertFileIntoTemplate(fp, uploadData.Events, this.firstEventTemplate, this.subsequentEventTemplate)
		this.HTTPPostTemplate.Execute(writer, uploadData)
	}()

	/* Set the header values of the post */
	for key, value := range this.headers {
		request.Header.Set(key, value)
	}

	/* Execute the POST */
	resp, err := this.client.Do(request)
	if err != nil {
		return UploadStatus{fileName: fileName, result: err, status: 0}
	}
	defer resp.Body.Close()

	/* Some sort of issue with the POST */
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		errorData := resp.Status + "\n" + string(body)

		return UploadStatus{fileName: fileName,
			result: fmt.Errorf("HTTP request failed: Error code %s", errorData), status: resp.StatusCode}
	}
	return UploadStatus{fileName: fileName, result: err, status: 200}
}
