package tests

import (
	"archive/zip"
	"encoding/binary"
	cfg "github.com/carbonblack/cb-event-forwarder/pkg/config"
	"github.com/carbonblack/cb-event-forwarder/pkg/protobufmessageprocessor"
	"github.com/streadway/amqp"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
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

func createProtobufBundles(sourceDir string, outputDir string, bundleWriter bundlerWriterFunc) string {
	dir, err := os.Open(sourceDir)
	if err != nil {
		return "Could not open " + sourceDir
	}
	eventTypeDirs, err := dir.Readdir(0)
	if err != nil {
		return "Could not enumerate directory " + sourceDir
	}
	_ = dir.Close()

	for _, eventTypeDir := range eventTypeDirs {
		if !eventTypeDir.IsDir() {
			continue
		}
		failure := createProtobufBundle(path.Join(sourceDir, eventTypeDir.Name()), outputDir, bundleWriter)
		if failure != "" {
			return failure
		}
	}

	return ""
}

func createProtobufBundle(protobufMessageDir string, outputDir string, bundleWriter bundlerWriterFunc) string {
	dir, err := os.Open(protobufMessageDir)
	if err != nil {
		return "Could not open " + protobufMessageDir
	}
	protobufFiles, err := dir.Readdir(0)
	if err != nil {
		return "Could not enumerate directory " + protobufMessageDir
	}
	_ = dir.Close()

	bundleData := []byte("")

	for _, protobufFile := range protobufFiles {
		if protobufFile.IsDir() {
			continue
		}

		protobufFp, err := os.Open(path.Join(protobufMessageDir, protobufFile.Name()))
		if err != nil {
			return "Unable to create Protobuf bundle - Could not open protobuf data from: " + protobufFile.Name()
		}
		protobufData, err := ioutil.ReadAll(protobufFp)
		if err != nil {
			return "Unable to create Protobuf bundle - Could not read protobuf data from: " + protobufFile.Name()
		}
		_ = protobufFp.Close()

		lenAsBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(lenAsBytes, (uint32)(len(protobufData)))
		bundleData = append(bundleData, lenAsBytes...)
		bundleData = append(bundleData, protobufData...)
	}

	_, protobufName := path.Split(protobufMessageDir)
	return bundleWriter(bundleData, path.Join(outputDir, protobufName, "bundle.zip"))
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
		_, _ = processProtobufNG("ingress.event.process", d)
	}
}

func prepareZipInputData(outputDir string) string {
	// The zip input data is taken from the protobuf data. For each group of
	// protobuf messages we will combine them and then zip them.
	protobufDir := path.Join(outputDir, "../../../test/raw_data/protobuf")
	_, err := os.Stat(protobufDir)
	if os.IsNotExist(err) {
		return "Unable to find input protobuf data; cannot generate zipped input files: " + protobufDir
	}

	if err := os.MkdirAll(outputDir, 0770); err != nil {
		return "Could not create path for zipped input files: " + outputDir
	}

	return createProtobufBundles(protobufDir, outputDir, zipBundleData)
}

type bundlerWriterFunc func(protobufBundleData []byte, outputFilename string) string

func zipBundleData(bundleData []byte, outputFilename string) string {
	if err := os.MkdirAll(filepath.Dir(outputFilename), 0770); err != nil {
		return "Could not create path for zip file: " + outputFilename
	}

	file, err := os.Create(outputFilename)
	if err != nil {
		return "Could not create zip file: " + outputFilename
	}

	zipWriter := zip.NewWriter(file)
	zipFile, err := zipWriter.Create("events")
	if err != nil {
		return "Could not create 'events' file in zip file: " + outputFilename
	}
	_, _ = zipFile.Write(bundleData)
	_ = zipWriter.Close()
	_ = file.Close()

	return ""
}

func processZipNG(routingKey string, indata []byte) ([][]byte, error) {
	config.UseTimeFloat = false
	emptyHeaders := new(amqp.Table)

	msg, err := processor.ProcessRawZipBundle(routingKey, indata, *emptyHeaders)
	if err != nil {
		return nil, err
	}
	return msg, nil
}

func BenchmarkZipBundleProcessingNG(b *testing.B) {
	fn := path.Join("../test/stress_rabbit/zipbundles/1")
	fp, _ := os.Open(fn)
	d, _ := ioutil.ReadAll(fp)

	fakeHeaders := amqp.Table{}

	for i := 0; i < b.N; i++ {
		_, _ = processor.ProcessRawZipBundle("", d, fakeHeaders)
	}
}

type outputMessageFuncNG func([][]byte) (string, error)

type testConfiguration struct {
	formatType      string
	rawDataLocation string
	process         func(string, []byte) ([][]byte, error)
	prepare         func(string) string
}

func TestEventProcessingNGProtobuf(t *testing.T) {
	t.Log("Generating protobuf output to go_output...")
	config := testConfiguration{"protobuf", "../test/raw_data/protobuf", processProtobufNG, nil}
	processTestEventsNG(t, "go_output", marshalJSONNG, config)
}

func TestEventProcessingNGZip(t *testing.T) {
	t.Log("Generating zip output to go_output...")
	config := testConfiguration{"zip", "../test_output/raw_data/zip", processZipNG, prepareZipInputData}
	processTestEventsNG(t, "go_output", marshalJSONNG, config)
}

func processTestEventsNG(t *testing.T, outputDir string, outputFunc outputMessageFuncNG, testConfig testConfiguration) {
	if testConfig.prepare != nil {
		failure := testConfig.prepare(testConfig.rawDataLocation)
		if failure != "" {
			t.Logf("Could not prepare input for %s: %s", testConfig.formatType, failure)
			t.FailNow()
		}
	}

	fp, err := os.Open(testConfig.rawDataLocation)
	if err != nil {
		t.Logf("Could not open %s", testConfig.rawDataLocation)
		t.FailNow()
	}

	infos, err := fp.Readdir(0)
	if err != nil {
		t.Logf("Could not enumerate directory %s", testConfig.rawDataLocation)
		t.FailNow()
	}

	_ = fp.Close()

	for _, info := range infos {
		if !info.IsDir() {
			continue
		}

		routingKey := info.Name()
		testOutputDir := path.Join("../test_output", outputDir, testConfig.formatType, routingKey)
		_ = os.MkdirAll(testOutputDir, 0755)

		// process all files inside this directory
		routingDir := path.Join(testConfig.rawDataLocation, info.Name())
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

		_ = fp.Close()

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

			_ = fp.Close()

			msgs, err := testConfig.process(routingKey, b)
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

			outfp, err := os.Create(path.Join(testOutputDir, fn.Name()))
			if err != nil {
				t.Errorf("Error creating file: %s", err)
				continue
			}

			_, _ = outfp.Write([]byte(out))
			_ = outfp.Close()
		}
	}
}
