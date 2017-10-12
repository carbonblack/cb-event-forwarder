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
	"crypto/sha1"
	"encoding/hex"
	"gopkg.in/h2non/filetype.v1"
	"hash"
)

// The following UUID code is from https://github.com/satori/go.uuid:
// Copyright (C) 2013-2015 by Maxim Bublis <b@codemonkey.ru>
//
// Permission is hereby granted, free of charge, to any person obtaining
// a copy of this software and associated documentation files (the
// "Software"), to deal in the Software without restriction, including
// without limitation the rights to use, copy, modify, merge, publish,
// distribute, sublicense, and/or sell copies of the Software, and to
// permit persons to whom the Software is furnished to do so, subject to
// the following conditions:
//
// The above copyright notice and this permission notice shall be
// included in all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
// EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
// MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
// NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
// LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
// OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
// WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

// UUID representation compliant with specification
// described in RFC 4122.
type UUID [16]byte

// Used in string method conversion
const dash byte = '-'

// SetVersion sets version bits.
func (u *UUID) SetVersion(v byte) {
	u[6] = (u[6] & 0x0f) | (v << 4)
}

// SetVariant sets variant bits as described in RFC 4122.
func (u *UUID) SetVariant() {
	u[8] = (u[8] & 0xbf) | 0x80
}

// Returns canonical string representation of UUID:
// xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.
func (u UUID) String() string {
	buf := make([]byte, 36)

	hex.Encode(buf[0:8], u[0:4])
	buf[8] = dash
	hex.Encode(buf[9:13], u[4:6])
	buf[13] = dash
	hex.Encode(buf[14:18], u[6:8])
	buf[18] = dash
	hex.Encode(buf[19:23], u[8:10])
	buf[23] = dash
	hex.Encode(buf[24:], u[10:])

	return string(buf)
}

// NewV5 returns UUID based on SHA-1 hash of namespace UUID and name.
func NewV5(ns UUID, name string) UUID {
	u := newFromHash(sha1.New(), ns, name)
	u.SetVersion(5)
	u.SetVariant()

	return u
}

// Returns UUID based on hashing of namespace UUID and name.
func newFromHash(h hash.Hash, ns UUID, name string) UUID {
	u := UUID{}
	h.Write(ns[:])
	h.Write([]byte(name))
	copy(u[:], h.Sum(nil))

	return u
}

/* This is the Splunk HTTP Event Collector (HEC) implementation of the OutputHandler interface defined in main.go */
type SplunkBehavior struct {
	dest    string
	headers map[string]string

	client *http.Client

	hecToken string
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
    this.subsequentEventTemplate = template.Must(template.New("subsequent_event").Parse("\n, {{.}}"))
	this.headers = make(map[string]string)

	this.dest = dest

	/* add authorization token, if applicable */
	if config.SplunkToken != "" {
		this.headers["Authorization"] = fmt.Sprintf("Splunk %s", *config.HttpAuthorizationToken)
	}

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


func (this *SplunkBehavior) readFromFile(fp *os.File) {
	var fileReader io.Reader

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

	request, err := http.NewRequest("POST", this.dest, reader)

	go func() {
		defer writer.Close()

		// spawn goroutine to read from the file
		go this.readFromFile(fp)

		this.httpPostTemplate.Execute(writer, uploadData)
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
			result: fmt.Errorf("HTTP request failed: Error code %s", errorData)}
	}
	return UploadStatus{fileName: fileName, result: err}
}
