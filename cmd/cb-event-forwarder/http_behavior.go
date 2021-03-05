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

	gzip "github.com/klauspost/pgzip"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"
)

/* This is the HTTP implementation of the OutputHandler interface defined in main.go */
type HTTPBehavior struct {
	dest    string
	headers map[string]string

	client *http.Client

	HTTPPostTemplate        *template.Template
	firstEventTemplate      *template.Template
	subsequentEventTemplate *template.Template
}

type HTTPStatistics struct {
	Destination string `json:"destination"`
}

/* Construct the HTTPBehavior object */
func (this *HTTPBehavior) Initialize(dest string) error {
	this.HTTPPostTemplate = config.HTTPPostTemplate
	this.firstEventTemplate = template.Must(template.New("first_event").Parse(`{{.}}`))
	this.subsequentEventTemplate = template.Must(template.New("subsequent_event").Parse("\n, {{.}}"))

	this.headers = make(map[string]string)

	this.dest = dest

	/* add authorization token, if applicable */
	if config.HTTPAuthorizationToken != nil {
		this.headers["Authorization"] = *config.HTTPAuthorizationToken
	}

	this.headers["Content-Type"] = *config.HTTPContentType
	if config.CompressHTTPPayload {
		this.headers["Content-Encoding"] = "gzip"
	}

	this.client = &http.Client{
		Transport: createTransport(&config),
		Timeout:   120 * time.Second, // default timeout is 2 minutes for the entire exchange
	}

	return nil
}

// createTransport returns Transport which will be used in http.Client.
func createTransport(config *Configuration) http.RoundTripper {
	baseTransport := &http.Transport{
		TLSClientConfig:     config.TLSConfig,
		Dial:                (&net.Dialer{Timeout: 5 * time.Second}).Dial,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	// If OAuth is configured, wrap baseTransport in oauth2.Transport.
	// We identify that OAuth is configured when all required fields for OAuth are configured.
	// These fields include client email, private key, and token url.
	if len(config.OAuthJwtClientEmail) > 0 &&
		len(config.OAuthJwtPrivateKey) > 0 &&
		len(config.OAuthJwtTokenUrl) > 0 {

		jwtConfig := &jwt.Config{
			Email:        config.OAuthJwtClientEmail,
			PrivateKey:   config.OAuthJwtPrivateKey,
			PrivateKeyID: config.OAuthJwtPrivateKeyId,
			Scopes:       config.OAuthJwtScopes,
			TokenURL:     config.OAuthJwtTokenUrl,
		}

		return &oauth2.Transport{
			Base:   baseTransport,
			Source: oauth2.ReuseTokenSource(nil, jwtConfig.TokenSource(nil)),
		}
	}

	return baseTransport
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

	if err == nil {
		/*This has to happen in another context because of the pipe used to read-write the outgoing message*/
		go func() {
			defer fp.Close()
			defer writer.Close()

			var httpWriter io.Writer = writer

			// if we are using compression, chain the GzipWriter inline
			if config.CompressHTTPPayload {
				gzw := gzip.NewWriter(writer)
				defer gzw.Close()
				httpWriter = gzw
			}

			go convertFileIntoTemplate(fp, uploadData.Events, this.firstEventTemplate, this.subsequentEventTemplate)

			this.HTTPPostTemplate.Execute(httpWriter, uploadData)
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

			return UploadStatus{fileName: fileName,
				result: fmt.Errorf("HTTP request failed: Error code %s", errorData), status: resp.StatusCode}
		}
		return UploadStatus{fileName: fileName, result: err, status: 200}

	}
	fp.Close()
	return UploadStatus{fileName: fileName, result: err, status: 500}
}
