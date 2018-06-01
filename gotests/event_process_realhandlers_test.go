package tests

import (
	"context"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/output"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"testing"
	"time"
)

func TestFileOutput(t *testing.T) {
	var outputHandler output.OutputHandler = &output.FileOutput{Config: &config}
	outputDir := "../test_output/real_output_file/"
	os.MkdirAll(outputDir, 0755)
	processTestEventsWithRealHandler(t, outputDir, jsonmessageprocessor.MarshalJSON, outputHandler, path.Join(outputDir, "/filehandlerout"), nil, nil)
}

func TestNetOutputTCP(t *testing.T) {
	var outputHandler output.OutputHandler = &output.NetOutput{Config: &config}
	t.Logf("Starting netoutput test")
	outputDir := "../test_output/real_output_net"
	os.MkdirAll(outputDir, 0755)
	listener, err := net.Listen("tcp", "127.0.0.1:41337")
	if err != nil {
		t.Errorf("Failed to open listening socket... %v ", err)
		t.FailNow()
		return
	}

	outfile, err := os.Create(path.Join(outputDir, "/netoutputtcp")) // For read access.
	if err != nil {
		t.Errorf("Coudln't open httpoutput file %v", err)
		t.FailNow()
		return
	}

	stopchan := make(chan struct{}, 1)

	//spawn a TCP server
	background := func() {
		defer outfile.Close()
		conn, err := listener.Accept()
		if err != nil {
			t.Errorf("Could not accept connection on listening socket %v ", err)
			t.FailNow()
			return
		} else {
			t.Logf("Accepted conection on listening socket :) ")
		}
		for {
			select {
			case <-stopchan:
				t.Logf("got stop chan event in listner")
				return
			default:
			}
			buf := make([]byte, 1024)
			_, err := conn.Read(buf)
			if err == io.EOF {
				t.Logf("GOT EOF ON LISTENING SOCKET!")
				return
			} else if err != nil {
				t.Logf("error at listening socket %v", err)
			}
			_, err = outfile.Write(buf)
		}
	}

	shutdown := func() {
		stopchan <- struct{}{}
	}

	processTestEventsWithRealHandler(t, outputDir, jsonmessageprocessor.MarshalJSON, outputHandler, "tcp:127.0.0.1:41337", &background, &shutdown)
}

//Doesn't work yet
func testNetOutputUDP(t *testing.T) {
	var outputHandler output.OutputHandler = &output.NetOutput{Config: &config}
	t.Logf("Starting netoutput test")
	outputDir := "../test_output/real_output_net"
	os.MkdirAll(outputDir, 0755)

	listener, err := net.ListenPacket("udp", "127.0.0.1:41337")
	if err != nil {
		t.Errorf("Could not accept connection on listening socket %v ", err)
		t.FailNow()
	}
	defer listener.Close()

	outfile, err := os.Create(path.Join(outputDir, "/netoutputudp")) // For read access.
	if err != nil {
		t.Errorf("Coudln't open httpoutput file %v", err)
		t.FailNow()
		return
	}

	stopchan := make(chan struct{}, 1)

	//spawn a TCP server
	background := func() {
		defer outfile.Close()
		for {
			select {
			case <-stopchan:
				t.Logf("got stop chan event in listner")
				return
			default:
			}
			buffer := make([]byte, 1024*4)
			_, addr, err := listener.ReadFrom(buffer)
			n, err := outfile.Write(buffer)
			t.Logf("Wrote %d bytes to logfile %v recv'd from %s", n, err, addr)
		}
	}

	shutdown := func() {
		stopchan <- struct{}{}
	}

	processTestEventsWithRealHandler(t, outputDir, jsonmessageprocessor.MarshalJSON, outputHandler, "udp:127.0.0.1:41337", &background, &shutdown)
}

type handler struct {
	outf *os.File
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "OK")
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "can't read body", http.StatusBadRequest)
		return
	}
	h.outf.Write(body)
}

func TestHttpOutput(t *testing.T) {
	var outputHandler output.OutputHandler = &output.BundledOutput{TempFileDirectory: ".", Behavior: &output.HTTPBehavior{}}
	t.Logf("Starting httpoutput test")
	outputDir := "../test_output/real_output_http"
	os.MkdirAll(outputDir, 0755)

	outfile, err := os.Create(path.Join(outputDir, "/httpoutput")) // For read access.
	if err != nil {
		t.Errorf("Coudln't open httpoutput file %v", err)
		t.FailNow()
		return
	}

	h := handler{outf: outfile}

	s := &http.Server{
		Addr:           "127.0.0.1:51337",
		Handler:        h,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:51337")
	if err != nil {
		t.Errorf("Couldn't listen on http port %v", err)
		t.FailNow()
		return
	}

	background := func() {
		s.Serve(listener)
	}

	shutdown := func() {
		ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
		s.Shutdown(ctx)
		h.outf.Close()
	}

	processTestEventsWithRealHandler(t, outputDir, jsonmessageprocessor.MarshalJSON, outputHandler, "http://127.0.0.1:51337/", &background, &shutdown)
}

func TestS3Output(t *testing.T) {
	mocks3 := new(MockS3)
	outputDir := "../test_output/real_output_s3"
	os.MkdirAll(outputDir, 0755)
	outFile, err := os.Create(path.Join(outputDir, "/s3output")) // For read access.
	if err != nil {
		t.Errorf("Coudln't open s3output file %v", err)
		t.FailNow()
		return
	}
	mocks3.outfile = outFile
	var outputHandler output.OutputHandler = &output.BundledOutput{TempFileDirectory: ".", Behavior: &output.S3Behavior{Out: mocks3}}
	t.Logf("Starting s3output test")
	processTestEventsWithRealHandler(t, outputDir, jsonmessageprocessor.MarshalJSON, outputHandler, "http://127.0.0.1:51337/", nil, nil)
}

func processTestEventsWithRealHandler(t *testing.T, outputDir string, outputFunc outputMessageFunc, oh output.OutputHandler, ohname string, backgroundfunc *func(), shutdown *func()) {
	t.Logf("Tring to preform test with %v %s", oh, oh)
	if backgroundfunc != nil {
		t.Logf("Executing background func")
		go (*backgroundfunc)()

	}
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
	stopchan := make(chan struct{}, 1)

	go oh.Go(messages, errors, stopchan)

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
	if shutdown != nil {
		(*shutdown)()
	}
	stopchan <- struct{}{}
}
