package tests

import (
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/streadway/amqp"
	"io/ioutil"
	"os"
	"path"
	"testing"
)

func BenchmarkProtobufEventProcessing(b *testing.B) {
	fn := path.Join("../test/raw_data/protobuf/ingress.event.process/0.protobuf")
	fp, _ := os.Open(fn)
	d, _ := ioutil.ReadAll(fp)

	for i := 0; i < b.N; i++ {
		pbmp.ProcessProtobuf("ingress.event.process", d)
	}
}

func BenchmarkJsonEventProcessing(b *testing.B) {
	fn := path.Join("../test/raw_data/json/watchlist.hit.process/0.json")
	fp, _ := os.Open(fn)
	d, _ := ioutil.ReadAll(fp)

	for i := 0; i < b.N; i++ {
		jsmp.ProcessJSON("watchlist.hit.process", d)
	}
}

func BenchmarkZipBundleProcessing(b *testing.B) {
	fn := path.Join("../test/stress_rabbit/zipbundles/1")
	fp, _ := os.Open(fn)
	d, _ := ioutil.ReadAll(fp)

	fakeHeaders := amqp.Table{}

	for i := 0; i < b.N; i++ {
		pbmp.ProcessRawZipBundle("", d, fakeHeaders)
	}
}

type outputMessageFunc func([]map[string]interface{}) (string, error)

func TestEventProcessing(t *testing.T) {
	t.Log("Generating JSON output to go_output...")
	processTestEvents(t, "go_output", jsonmessageprocessor.MarshalJSON)
}

func processTestEvents(t *testing.T, outputDir string, outputFunc outputMessageFunc) {

	formats := [...]struct {
		formatType string
		process    func(string, []byte) ([]map[string]interface{}, error)
	}{{"json", jsmp.ProcessJSON}, {"protobuf", pbmp.ProcessProtobuf}}

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
