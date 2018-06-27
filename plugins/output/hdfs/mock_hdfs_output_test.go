package main

/**
* Copyright 2016 Confluent Inc.
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
* http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.


CARBON BLACK 2018 - Zachary Estep - Using this code as the basis for a producer interface that is mockable
*/

import (
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/output"
	"github.com/carbonblack/cb-event-forwarder/internal/pbmessageprocessor"
	"github.com/colinmarc/hdfs"
	"github.com/stretchr/testify/mock"
	"io/ioutil"
	"os"
	"path"
	"syscall"
	"testing"
	"time"
)

var m map[string]interface{} = map[string]interface{}{"plugin": map[string]interface{}{"hdfs_server": nil}}

var config conf.Configuration = conf.Configuration{UploadEmptyFiles: false, BundleSizeMax: 1024 * 1024 * 1024, BundleSendTimeout: time.Duration(30) * time.Second, CbServerURL: "https://cbtests/", DebugStore: ".", DebugFlag: true, ConfigMap: m, EventMap: make(map[string]bool)}

var pbmp pbmessageprocessor.PbMessageProcessor = pbmessageprocessor.PbMessageProcessor{Config: &config}
var jsmp jsonmessageprocessor.JsonMessageProcessor = jsonmessageprocessor.JsonMessageProcessor{Config: &config}

type MockedHDFSClient struct {
	mock.Mock
	outdir string
}

func (mHDFS *MockedHDFSClient) Create(name string) (*hdfs.FileWriter, error) {
	return nil, nil
}

type outputMessageFunc func([]map[string]interface{}) (string, error)

func TestHDFSOutput(t *testing.T) {
	// create an instance of our test object

	t.Logf("Starting hdfs output test")
	outputDir := "../../../test_output/real_output_hdfs"
	os.MkdirAll(outputDir, 0755)

	_, err := os.Create(path.Join(outputDir, "/hdfsoutput")) // For read access.
	if err != nil {
		t.Errorf("Coudln't open hdfsoutput file %v", err)
		t.FailNow()
		return
	}
	mockHdfs := new(MockedHDFSClient)
	mockHdfs.outdir = outputDir
	var outputHandler output.OutputHandler = &HdfsOutput{HDFSClient: mockHdfs}

	processTestEventsWithRealHandler(t, outputDir, jsonmessageprocessor.MarshalJSON, outputHandler, "mockhdfs")

}

func processTestEventsWithRealHandler(t *testing.T, outputDir string, outputFunc outputMessageFunc, oh output.OutputHandler, ohname string) {
	t.Logf("Tring to preform test with %v %s", oh, oh)
	t.Logf("Starting outputhandler %s ", ohname)
	err := oh.Initialize(ohname, &config)
	if err != nil {
		t.Errorf("%v", err)
		t.FailNow()
	} else {
		t.Logf("Output handler %v initialized succesfully, entering test", oh)
	}
	formats := [...]struct {
		formatType string
		process    func(string, []byte) ([]map[string]interface{}, error)
	}{{"json", jsmp.ProcessJSON}, {"protobuf", pbmp.ProcessProtobuf}}

	messages := make(chan string, 100)
	errors := make(chan error)
	controlchan := make(chan os.Signal, 5)

	oh.Go(messages, errors, controlchan)

	for _, format := range formats {
		pathname := path.Join("../../../test/raw_data", format.formatType)
		fp, err := os.Open(pathname)
		if err != nil {
			t.Logf("Could not open %s", pathname)
			t.FailNow()
		}

		infos, err := fp.Readdir(0)
		if err != nil {
			t.Logf("Could not enumerate directory %s", pathname)
			t.FailNow()
		}

		fp.Close()

		for _, info := range infos {
			if !info.IsDir() {
				continue
			}

			routingKey := info.Name()
			os.MkdirAll(outputDir, 0755)

			// add this routing key into the filtering map
			config.EventMap[routingKey] = true

			// process all files inside this directory
			routingDir := path.Join(pathname, info.Name())
			fp, err := os.Open(routingDir)
			if err != nil {
				t.Logf("Could not open directory %s", routingDir)
				t.FailNow()
			}

			files, err := fp.Readdir(0)
			if err != nil {
				t.Errorf("Could not enumerate directory %s; continuing", routingDir)
				continue
			}

			fp.Close()

			for _, fn := range files {
				if fn.IsDir() {
					continue
				}

				fp, err := os.Open(path.Join(routingDir, fn.Name()))
				if err != nil {
					t.Errorf("Could not open %s for reading", path.Join(routingDir, fn.Name()))
					continue
				}
				b, err := ioutil.ReadAll(fp)
				if err != nil {
					t.Errorf("Could not read %s", path.Join(routingDir, fn.Name()))
					continue
				}

				fp.Close()

				msgs, err := format.process(routingKey, b)
				if err != nil {
					t.Errorf("Error processing %s: %s", path.Join(routingDir, fn.Name()), err)
					continue
				}
				if len(msgs[0]) == 0 {
					t.Errorf("got zero messages out of: %s/%s", routingDir, fn.Name())
					continue
				}

				out, err := outputFunc(msgs)
				if err != nil {
					t.Errorf("Error serializing %s: %s", path.Join(routingDir, fn.Name()), err)
					continue
				}
				messages <- out
			}
		}
	}
	t.Logf("Done with test for %s ", oh)
	controlchan <- syscall.SIGTERM
}
