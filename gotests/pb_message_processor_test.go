package tests

import (
	"encoding/json"
	"github.com/carbonblack/cb-event-forwarder/internal/pbmessageprocessor"
	"io/ioutil"
	"os"
	"path"
	"testing"
)

func TestPbProcessing(t *testing.T) {
	pathname := path.Join("../test/raw_data", "protobuf")
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

		routingDir := path.Join(pathname, routingKey)
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
			d, err := ioutil.ReadAll(fp)
			if err != nil {
				t.Errorf("Could not read %s", path.Join(routingDir, fn.Name()))
				continue
			}
			eventMap := map[string]interface{}{
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
            pbmp := pbmessageprocessor.PbMessageProcessor{EventMap: eventMap, UseTimeFloat: false}
			msgs, err := pbmp.ProcessProtobuf(routingKey, d)
			if err == nil && len(msgs) > 0 {
				//t.Logf("%s",msgs)

				for _, msg := range msgs {
					_, _ = json.Marshal(msg)
					//t.Logf("%s",s)
				}
			} else {
				t.Errorf("Error marshaling %s to JSON: %s", d, err)
			}
			fp.Close()
		}
	}
}
