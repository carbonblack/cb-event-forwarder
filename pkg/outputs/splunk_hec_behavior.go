package outputs

import (
	"fmt"
	. "github.com/carbonblack/cb-event-forwarder/pkg/config"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"text/template"
	"time"
)

/* This is the Splunk HTTP Event Collector (HEC) implementation of the OutputHandler interface defined in main.go */
type SplunkBehavior struct {
	Config  *Configuration
	dest    string
	headers map[string]string

	client *http.Client

	HTTPPostTemplate        *template.Template
	firstEventTemplate      *template.Template
	subsequentEventTemplate *template.Template
}

func NewSplunkOutputFromConfig(cfg *Configuration) *BundledOutput {
	return &BundledOutput{Config: cfg, Behavior: &SplunkBehavior{Config: cfg}}
}

type SplunkStatistics struct {
	Destination string `json:"destination"`
}

/* Construct the syslog_output.go object */
func (this *SplunkBehavior) Initialize(dest string) error {
	this.HTTPPostTemplate = this.Config.HTTPPostTemplate
	this.firstEventTemplate = template.Must(template.New("first_event").Parse("{{.}}"))
	this.subsequentEventTemplate = template.Must(template.New("subsequent_event").Parse("{{.}}"))
	this.headers = make(map[string]string)

	this.dest = dest

	/* add authorization token, if applicable */
	if this.Config.SplunkToken != nil {
		this.headers["Authorization"] = fmt.Sprintf("Splunk %s", *this.Config.SplunkToken)
	}

	this.headers["Content-Type"] = *this.Config.HTTPContentType

	transport := &http.Transport{
		TLSClientConfig:     this.Config.TLSConfig,
		Dial:                (&net.Dialer{Timeout: 5 * time.Second}).Dial,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	this.client = &http.Client{
		Transport: transport,
		Timeout:   120 * time.Second, // default timeout is 2 minutes for the entire exchange
	}

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

	request, err := http.NewRequest("POST", this.dest, reader)

	go func() {
		defer fp.Close()
		defer writer.Close()

		// spawn goroutine to read from the file
		go convertFileIntoTemplate(this.Config, fp, uploadData.Events, this.firstEventTemplate, this.subsequentEventTemplate)
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
