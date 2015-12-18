package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"path"
	"testing"
)

func processJson(routingKey string, indata []byte) (string, error) {
	var msg map[string]interface{}
	var ret string

	decoder := json.NewDecoder(bytes.NewReader(indata))

	// Ensure that we decode numbers in the JSON as integers and *not* float64s
	decoder.UseNumber()

	if err := decoder.Decode(&msg); err != nil {
		return "", err
	}

	msgs, err := ProcessJSONMessage(msg, routingKey)
	if err != nil {
		return "", err
	}

	for _, msg := range msgs {
		msg["cb_server"] = "cbserver"
		marshaled, err := json.Marshal(msg)
		if err != nil {
			return "", err
		}
		ret += string(marshaled) + "\n"
	}

	return ret, nil
}

func processProtobuf(routingKey string, indata []byte) (string, error) {
	msg, err := ProcessProtobufMessage(routingKey, indata)
	if err != nil {
		return "", err
	}
	msg["cb_server"] = "cbserver"
	marshaled, err := json.Marshal(msg)

	return string(marshaled), err
}

func BenchmarkProtobufEventProcessing(b *testing.B) {
	fn := path.Join("./tests/raw_data/protobuf/ingress.event.process/0.protobuf")
	fp, _ := os.Open(fn)
	d, _ := ioutil.ReadAll(fp)

	for i := 0; i < b.N; i++ {
		processProtobuf("ingress.event.process", d)
	}
}

func BenchmarkJsonEventProcessing(b *testing.B) {
	fn := path.Join("./tests/raw_data/json/watchlist.hit.process/0.json")
	fp, _ := os.Open(fn)
	d, _ := ioutil.ReadAll(fp)

	for i := 0; i < b.N; i++ {
		processJson("watchlist.hit.process", d)
	}
}

func TestEventProcessing(t *testing.T) {
	formats := [...]struct {
		formatType string
		process    func(string, []byte) (string, error)
	}{{"json", processJson}, {"protobuf", processProtobuf}}

	config.CbServerURL = "https://cbtests/"

	for _, format := range formats {
		pathname := path.Join("./tests/raw_data", format.formatType)
		fp, err := os.Open(pathname)
		if err != nil {
			log.Printf("Could not open %s", pathname)
			continue
		}

		infos, err := fp.Readdir(0)
		if err != nil {
			log.Printf("Could not enumerate directory %s", pathname)
			continue
		}

		fp.Close()

		for _, info := range infos {
			if !info.IsDir() {
				continue
			}

			routingKey := info.Name()
			os.MkdirAll(path.Join("./tests/go_output", format.formatType, routingKey), 0755)

			// process all files inside this directory
			routingDir := path.Join(pathname, info.Name())
			fp, err := os.Open(routingDir)
			if err != nil {
				log.Printf("Could not open directory %s", routingDir)
			}

			files, err := fp.Readdir(0)
			if err != nil {
				log.Printf("Could not enumerate directory %s", routingDir)
				continue
			}

			fp.Close()

			for _, fn := range files {
				if fn.IsDir() {
					continue
				}

				fp, err := os.Open(path.Join(routingDir, fn.Name()))
				if err != nil {
					log.Printf("Could not open %s for reading", path.Join(routingDir, fn.Name()))
					continue
				}
				b, err := ioutil.ReadAll(fp)
				if err != nil {
					log.Printf("Could not read %s", path.Join(routingDir, fn.Name()))
					continue
				}

				fp.Close()

				out, err := format.process(routingKey, b)
				if err != nil {
					log.Printf("Error processing %s: %s", path.Join(routingDir, fn.Name()), err)
					continue
				}

				outfp, err := os.Create(path.Join("./tests/go_output", format.formatType, routingKey, fn.Name()))
				if err != nil {
					log.Printf("Error creating file: %s", err)
					continue
				}

				outfp.Write([]byte(out))
				outfp.Close()
			}
		}
	}
}
