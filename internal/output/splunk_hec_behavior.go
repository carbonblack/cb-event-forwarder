package output

import (
	"fmt"
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"text/template"
)

/* This is the Splunk HTTP Event Collector (HEC) implementation of the OutputHandler interface defined in main.go */
type SplunkBehavior struct {
	config  *conf.Configuration
	dest    string
	headers map[string]string

	client *http.Client

	HTTPPostTemplate        *template.Template
	firstEventTemplate      *template.Template
	subsequentEventTemplate *template.Template
}

type SplunkStatistics struct {
	Destination string `json:"destination"`
}

/* Construct the syslog_output.go object */
func (this *SplunkBehavior) Initialize(dest string, config *conf.Configuration) error {
	this.config = config
	this.HTTPPostTemplate = config.HTTPPostTemplate
	this.firstEventTemplate = template.Must(template.New("first_event").Parse("{{.}}"))
	this.subsequentEventTemplate = template.Must(template.New("subsequent_event").Parse("{{.}}"))
	this.headers = make(map[string]string)

	this.dest = dest

	/* add authorization token, if applicable */
	if config.SplunkToken != nil {
		this.headers["Authorization"] = fmt.Sprintf("Splunk %s", config.SplunkToken)
	}

	this.headers["Content-Type"] = config.HTTPContentType

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
		defer writer.Close()

		// spawn goroutine to read from the file
		go convertFileIntoTemplate(this.config, fp, uploadData.Events, this.firstEventTemplate, this.subsequentEventTemplate)
		this.HTTPPostTemplate.Execute(writer, uploadData)
	}()

	/* Set the header values of the post */
	for key, value := range this.headers {
		request.Header.Set(key, value)
	}

	/* Execute the POST */
	resp, err := this.client.Do(request)
	if err != nil {
		return UploadStatus{FileName: fileName, Result: err, Status: 0}
	}
	defer resp.Body.Close()

	/* Some sort of issue with the POST */
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		errorData := resp.Status + "\n" + string(body)

		return UploadStatus{FileName: fileName,
			Result: fmt.Errorf("HTTP request failed: Error code %s", errorData), Status: resp.StatusCode}
	}
	return UploadStatus{FileName: fileName, Result: err, Status: 200}
}
