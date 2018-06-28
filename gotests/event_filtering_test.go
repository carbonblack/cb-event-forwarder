package tests

import (
	"bytes"
	"encoding/json"
	"github.com/carbonblack/cb-event-forwarder/internal/filter"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"io/ioutil"
	"os"
	"path"
	"testing"
)

var testFilter *filter.Filter = filter.NewFilter("{{if (eq .type \"alert.watchlist.hit.query.binary\")}}KEEP{{else}}DROP{{end}}", "", "")

func filterMessages(msgs []map[string]interface{}) []map[string]interface{} {
	var ret []map[string]interface{} = make([]map[string]interface{}, 0)
	for _, msg := range msgs {
		msg["cb_server"] = "cbserver"
		if keep := testFilter.FilterEvent(msg); keep {
			ret = append(ret, msg)
		}
	}
	return ret
}

func generateFilteredOutput(exampleJSONInput string) error {

	var msg map[string]interface{}

	decoder := json.NewDecoder(bytes.NewReader([]byte(exampleJSONInput)))

	// Ensure that we decode numbers in the JSON as integers and *not* float64s
	decoder.UseNumber()

	if err := decoder.Decode(&msg); err != nil {
		return err
	}

	msgs, err := jsmp.ProcessJSONMessage(msg, "watchlist.hit.test")
	if err != nil {
		return err
	}

	for _, msg := range msgs {
		if ok := testFilter.FilterEvent(msg); ok {

		}
	}

	return nil
}

func BenchmarkFilter(b *testing.B) {
	fn := path.Join("../test/raw_data/json/watchlist.hit.process/0.json")
	fp, _ := os.Open(fn)
	d, _ := ioutil.ReadAll(fp)
	s := string(d)
	for i := 0; i < b.N; i++ {
		generateFilteredOutput(s)
	}
}

func TestFilterOutput(t *testing.T) {
	t.Log("Generated filtered_output to filtered_output")
	filterTestEvents(t, "filtered_output", filterMessages)
}

type FilterFunc func([]map[string]interface{}) []map[string]interface{}

func filterTestEvents(t *testing.T, outputDir string, filterFunc FilterFunc) {
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

				msgs = filterFunc(msgs)
				if msgs != nil && len(msgs) > 0 {
					out, err := jsonmessageprocessor.MarshalJSON(msgs)
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
}
