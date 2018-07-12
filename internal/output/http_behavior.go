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

/* This is the HTTP implementation of the OutputHandler interface defined in main.go */
type HTTPBehavior struct {
	dest    string
	headers map[string]string

	client *http.Client

	HTTPPostTemplate        *template.Template
	firstEventTemplate      *template.Template
	subsequentEventTemplate *template.Template
	DebugFlag               bool
	CommaSeperateEvents     bool
	DebugStore              string
}

type HTTPStatistics struct {
	Destination string `json:"destination"`
}

func HTTPBehaviorFromCfg(cfg map[interface{}]interface{}, debugFlag bool, debugStore string, tlsConfig *tls.Config) (*HTTPBehavior, error) {
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
	headers := make(map[string]string)
	if headerTemp, ok := cfg["headers"]; ok {
		headerTempMap, _ := headerTemp.(map[interface{}]interface{})
		for k, v := range headerTempMap {
			headers[k.(string)] = v.(string)
		}
	}
	httpb, err := NewHTTPBehavior(http_post_template, dest, headers, commaSeparate, debugFlag, debugStore, tlsConfig)
	return &httpb, err
}

/* Construct the HTTPBehavior object */
func NewHTTPBehavior(httpPostTemplate, dest string, headers map[string]string, jsonFormat, debugFlag bool, debugStore string, tlsConfig *tls.Config) (HTTPBehavior, error) {
	temp := HTTPBehavior{DebugFlag: debugFlag, DebugStore: debugStore, headers: headers, dest: dest}
	temp.firstEventTemplate = template.Must(template.New("first_event").Parse(`{{.}}`))
	temp.subsequentEventTemplate = template.Must(template.New("subsequent_event").Parse("\n, {{.}}"))
	HTTPPostTemplate := template.New("http_post_output")
	if httpPostTemplate != "" {
		HTTPPostTemplate = template.Must(HTTPPostTemplate.Parse(httpPostTemplate))
	} else {
		if jsonFormat {
			HTTPPostTemplate = template.Must(HTTPPostTemplate.Parse(
				`{"filename": "{{.FileName}}", "service": "carbonblack", "alerts":[{{range .Events}}{{.EventText}}{{end}}]}`))
		} else {
			HTTPPostTemplate = template.Must(HTTPPostTemplate.Parse(`{{range .Events}}{{.EventText}}{{end}}`))
		}
	}
	temp.HTTPPostTemplate = HTTPPostTemplate

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	temp.client = &http.Client{Transport: transport}

	return temp, nil
}

func (this *HTTPBehavior) String() string {
	return "HTTP POST " + this.Key()
}

func (this *HTTPBehavior) Statistics() interface{} {
	return HTTPStatistics{
		Destination: this.dest,
	}
}

func (this *HTTPBehavior) Key() string {
	return this.dest
}

/* This function does a POST of the given event to this.dest. UploadBehavior is called from within its own
   goroutine so we can do some expensive work here. */
func (this *HTTPBehavior) Upload(fileName string, fp *os.File) UploadStatus {
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
		go convertFileIntoTemplate(this.CommaSeperateEvents, this.DebugFlag, this.DebugStore, fp, uploadData.Events, this.firstEventTemplate, this.subsequentEventTemplate)

		this.HTTPPostTemplate.Execute(writer, uploadData)
	}()

	/* Set the header values of the post */
	for key, value := range this.headers {
		request.Header.Set(key, value)
	}

	/* Execute the POST */
	resp, err := this.client.Do(request)
	if err != nil {
		return UploadStatus{FileName: fileName, Result: err}
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
