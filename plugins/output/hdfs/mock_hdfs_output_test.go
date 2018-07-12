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
	"github.com/carbonblack/cb-event-forwarder/internal/encoder"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/output"
	"github.com/carbonblack/cb-event-forwarder/internal/pbmessageprocessor"
	"github.com/colinmarc/hdfs"
	"github.com/stretchr/testify/mock"
	"io/ioutil"
	"os"
	"path"
	"sync"
	"syscall"
	"testing"
)

var jsmp jsonmessageprocessor.JsonMessageProcessor = jsonmessageprocessor.JsonMessageProcessor{}
var eventMap map[string]interface{} = map[string]interface{}{
	"ingress.event.process":        true,
	"ingress.event.procstart":      true,
	"ingress.event.netconn":        true,
	"ingress.event.procend":        true,
	"ingress.event.childproc":      true,
	"ingress.event.moduleload":     true,
	"ingress.event.module":         true,
	"ingress.event.filemod":        true,
	"ingress.event.regmod":         true,
	"ingress.event.tamper":         true,
	"ingress.event.crossprocopen":  true,
	"ingress.event.remotethread":   true,
	"ingress.event.processblock":   true,
	"ingress.event.emetmitigation": true,
	"binaryinfo.#":                 true,
	"binarystore.#":                true,
	"events.partition.#":           true,
}
var pbmp pbmessageprocessor.PbMessageProcessor = pbmessageprocessor.PbMessageProcessor{EventMap: eventMap}

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
	testEncoder := encoder.NewJSONEncoder()
	outputHandler := HdfsOutput{HDFSClient: mockHdfs, Encoder: &testEncoder}
	processTestEventsWithRealHandler(t, outputDir, jsonmessageprocessor.MarshalJSON, &outputHandler)

}

func processTestEventsWithRealHandler(t *testing.T, outputDir string, outputFunc outputMessageFunc, oh output.OutputHandler) {
	t.Logf("Tring to preform test with %v %s", oh, oh)

	formats := [...]struct {
		formatType string
		process    func(string, []byte) ([]map[string]interface{}, error)
	}{{"json", jsmp.ProcessJSON}, {"protobuf", pbmp.ProcessProtobuf}}

	messages := make(chan map[string]interface{}, 100)
	errors := make(chan error)
	var wg sync.WaitGroup
	wg.Add(1)
	controlchan := make(chan os.Signal, 5)

	oh.Go(messages, errors, controlchan, wg)

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
				for _, msg := range msgs {
					messages <- msg
				}

				_, err = outputFunc(msgs)
				if err != nil {
					t.Errorf("Error serializing %s: %s", path.Join(routingDir, fn.Name()), err)
					continue
				}
			}
		}
	}
	t.Logf("Done with test for %s ", oh)
	controlchan <- syscall.SIGTERM
}
