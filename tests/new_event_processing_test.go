package tests

import (
	cfg "github.com/carbonblack/cb-event-forwarder/pkg/config"
	"github.com/carbonblack/cb-event-forwarder/pkg/protobufmessageprocessor"
	"github.com/streadway/amqp"
	"io/ioutil"
	"os"
	"path"
	"testing"
)

var theConfig = cfg.Configuration{CbServerURL: "https://cbtests/", EventMap: ALLRAWEVENTS}

var processor = protobufmessageprocessor.NewProtobufMessageProcessor(&theConfig)

var ALLRAWEVENTS = map[string]bool{
	"ingress.event.process":            true,
	"ingress.event.procstart":          true,
	"ingress.event.netconn":            true,
	"ingress.event.procend":            true,
	"ingress.event.childproc":          true,
	"ingress.event.moduleload":         true,
	"ingress.event.module":             true,
	"ingress.event.filemod":            true,
	"ingress.event.regmod":             true,
	"ingress.event.tamper":             true,
	"ingress.event.crossprocopen":      true,
	"ingress.event.remotethread":       true,
	"ingress.event.processblock":       true,
	"ingress.event.emetmitigation":     true,
	"ingress.event.filelessscriptload": true,
}

func marshalJSONNG(msgs [][]byte) (string, error) {
	var ret string

	for _, msg := range msgs {
		ret += string(msg) + "\n"
	}

	return ret, nil
}

func processProtobufNG(routingKey string, indata []byte) ([][]byte, error) {
	config.UseTimeFloat = false
	emptyHeaders := new(amqp.Table)

	msg, err := processor.ProcessProtobufMessage(routingKey, indata, *emptyHeaders)
	if err != nil {
		return nil, err
	}
	msgs := make([][]byte, 0, 1)
	msgs = append(msgs, msg)
	return msgs, nil
}

func BenchmarkNGProtobufEventProcessing(b *testing.B) {
	fn := path.Join("../test/raw_data/protobuf/ingress.event.process/0.protobuf")
	fp, _ := os.Open(fn)
	d, _ := ioutil.ReadAll(fp)

	for i := 0; i < b.N; i++ {
		processProtobufNG("ingress.event.process", d)
	}
}

func BenchmarkZipBundleProcessingNG(b *testing.B) {
	fn := path.Join("../test/stress_rabbit/zipbundles/1")
	fp, _ := os.Open(fn)
	d, _ := ioutil.ReadAll(fp)

	fakeHeaders := amqp.Table{}

	for i := 0; i < b.N; i++ {
		processor.ProcessRawZipBundle("", d, fakeHeaders)
	}
}

type outputMessageFuncNG func([][]byte) (string, error)

func TestEventProcessingNG(t *testing.T) {
	t.Log("Generating JSON output to go_output...")
	processTestEventsNG(t, "go_output", marshalJSONNG)
}

func processTestEventsNG(t *testing.T, outputDir string, outputFunc outputMessageFuncNG) {
	formats := [...]struct {
		formatType string
		process    func(string, []byte) ([][]byte, error)
	}{{"protobuf", processProtobufNG}}

	for _, format := range formats {
		pathname := path.Join("../test/raw_data", format.formatType)
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
			os.MkdirAll(path.Join("../test_output", outputDir, format.formatType, routingKey), 0755)

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

				outfp, err := os.Create(path.Join("../test_output", outputDir, format.formatType, routingKey, fn.Name()))
				if err != nil {
					t.Errorf("Error creating file: %s", err)
					continue
				}

				outfp.Write([]byte(out))
				outfp.Close()
			}
		}
	}
}
