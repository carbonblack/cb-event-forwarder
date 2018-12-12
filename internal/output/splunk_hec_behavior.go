package output

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"text/template"
)

/* This is the Splunk HTTP Event Collector (HEC) implementation of the OutputHandler interface defined in main.go */
type SplunkBehavior struct {
	dest    string
	headers map[string]string

	client *http.Client

	HTTPPostTemplate        *template.Template
	firstEventTemplate      *template.Template
	subsequentEventTemplate *template.Template
	DebugFlag               bool
	CommaSeperateEvents     bool
	OutputAsBytes     bool
	DebugStore              string
}

type SplunkStatistics struct {
	Destination string `json:"destination"`
}

/* Construct the syslog_output.go object */
func NewSplunkBehavior(httpPostTemplate, dest string, headers map[string]string, jsonFormat,eventAsBytes, debugFlag bool, debugStore string, tlsConfig *tls.Config) (SplunkBehavior, error) {
    newSplunkBehavior := SplunkBehavior{dest: dest, headers: headers,CommaSeperateEvents: jsonFormat,OutputAsBytes: eventAsBytes, DebugStore: debugStore, DebugFlag: debugFlag}
	newSplunkBehavior.firstEventTemplate = template.Must(template.New("first_event").Parse("{{.}}"))
	newSplunkBehavior.subsequentEventTemplate = template.Must(template.New("subsequent_event").Parse("{{.}}"))
	newSplunkBehavior.headers = headers
	HTTPPostTemplate := template.New("http_post_output")
	if httpPostTemplate != "" {
		HTTPPostTemplate = template.Must(HTTPPostTemplate.Parse(httpPostTemplate))
	} else {
		if jsonFormat {
			HTTPPostTemplate = template.Must(HTTPPostTemplate.Parse(
				`{{range .Events}}{"sourcetype":"bit9:carbonblack:json","event":{{.EventText}}}{{end}}`))
		} else {
			HTTPPostTemplate = template.Must(HTTPPostTemplate.Parse(`{{range .Events}}{{.EventText}}{{end}}`))
		}
	}
	newSplunkBehavior.HTTPPostTemplate = HTTPPostTemplate

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}
	newSplunkBehavior.client = &http.Client{Transport: transport}

	return newSplunkBehavior, nil
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
		go convertFileIntoTemplate(this.CommaSeperateEvents,this.OutputAsBytes, this.DebugFlag, this.DebugStore, fp, uploadData.Events, this.firstEventTemplate, this.subsequentEventTemplate)
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

func SplunkBehaviorFromCfg(cfg map[interface{}]interface{}, debugFlag bool, debugStore string, tlsConfig *tls.Config) (*SplunkBehavior, error) {
	http_post_template := ""
	if temp, ok := cfg["http_post_template"]; ok {
		http_post_template, _ = temp.(string)
	}

	dest := ""
	if dtemp, ok := cfg["destination"]; ok {
		dest, _ = dtemp.(string)
	} else {
		return nil, errors.New("No destination provided in HTTP section")
	}

	commaSeparate := false
	if btemp, ok := cfg["comma_seperate_events"]; ok {
		commaSeparate = btemp.(bool)
	}

	eventAsBytes := false
	if btemp, ok := cfg["event_as_bytes"]; ok {
		eventAsBytes = btemp.(bool)
	}

	headers := make(map[string]string)
	if headerTemp, ok := cfg["headers"]; ok {
		headerTempMap, _ := headerTemp.(map[interface{}]interface{})
		for k, v := range headerTempMap {
			headers[k.(string)] = v.(string)
		}
	}
	httpb, err := NewSplunkBehavior(http_post_template, dest, headers, commaSeparate,eventAsBytes, debugFlag, debugStore, tlsConfig)
	return &httpb, err
}
