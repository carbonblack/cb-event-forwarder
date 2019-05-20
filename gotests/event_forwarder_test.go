package tests

import (
	"context"
	"github.com/carbonblack/cb-event-forwarder/internal/cbeventforwarder"
	"github.com/carbonblack/cb-event-forwarder/internal/consumer"
	"github.com/streadway/amqp"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"syscall"
	"testing"
	"time"
)

func TestFileOutputReal(t *testing.T) {
	outputDir := "../test_output/real_output_file_real/"
	os.MkdirAll(outputDir, 0755)
	conf := map[string]interface{}{"debug": false,
		"use_time_float": true,
		"input":          map[interface{}]interface{}{"cbresponse": map[interface{}]interface{}{"cb_server_url": "https://cbresponseserver", "bind_raw_exchange": true, "rabbit_mq_password": "lol", "rabbit_mq_port": 5672}},
		"output":         []interface{}{map[interface{}]interface{}{"file": map[interface{}]interface{}{"path": path.Join(outputDir, "realfileout"), "format": map[interface{}]interface{}{"type": "json"}}}},
	}

	processTestEventsWithRealForwarder(t, conf, outputDir, nil, nil)
}

func TestNetOutputTCPReal(t *testing.T) {
	t.Logf("Starting netoutput test")
	outputDir := "../test_output/real_output_net_real"
	os.MkdirAll(outputDir, 0755)
	conf := map[string]interface{}{"debug": false,
		"use_time_float": true,
		"input":          map[interface{}]interface{}{"cbresponse": map[interface{}]interface{}{"cb_server_url": "https://cbresponseserver", "bind_raw_exchange": true, "rabbit_mq_password": "lol", "rabbit_mq_port": 5672}},
		"output":         []interface{}{map[interface{}]interface{}{"socket": map[interface{}]interface{}{"connection": "tcp:127.0.0.1:41337", "format": map[interface{}]interface{}{"type": "json"}}}},
	}
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

	processTestEventsWithRealForwarder(t, conf, outputDir, &background, &shutdown)
}

func TestHttpOutputReal(t *testing.T) {

	t.Logf("Starting httpoutput test")
	outputDir := "../test_output/real_output_http_real"

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

	conf := map[string]interface{}{"debug": false,
		"use_time_float": true,
		"input":          map[interface{}]interface{}{"cbresponse": map[interface{}]interface{}{"cb_server_url": "https://cbresponseserver", "bind_raw_exchange": true, "rabbit_mq_password": "lol", "rabbit_mq_port": 5672}},
		"output":         []interface{}{map[interface{}]interface{}{"http": map[interface{}]interface{}{"bundle_directory": "/tmp", "destination": "http://127.0.0.1:51337/", "format": map[interface{}]interface{}{"type": "json"}}}},
	}
	processTestEventsWithRealForwarder(t, conf, outputDir, &background, &shutdown)
}
/* Need to expose way to DI the mocks3 to s3 behavior
func TestS3OutputReal(t * testing.T) {
	mocks3 := new(MockS3)
	outputDir := "../test_output/real_output_s3_real"
	os.MkdirAll(outputDir, 0755)
	outFile, err := os.Create(path.Join(outputDir, "/s3output")) // For read access.
	if err != nil {
		t.Errorf("Coudln't open s3output file %v", err)
		t.FailNow()
		return
	}
	mocks3.outfile = outFile
	t.Logf("Starting s3output test")
	conf := map[string]interface{}{"debug": false,
		"use_time_float": true,
		"input":          map[interface{}]interface{}{"cbresponse": map[interface{}]interface{}{"cb_server_url": "https://cbresponseserver", "bind_raw_exchange": true, "rabbit_mq_password": "lol", "rabbit_mq_port": 5672}},
		"output":         []interface{}{map[interface{}]interface{}{"s3": map[interface{}]interface{}{"bundle_directory": "/tmp", "bucket_name":"bucket", "region":"east","format": map[interface{}]interface{}{"type": "json"}}}},
	}
	processTestEventsWithRealForwarder(t, conf, outputDir,nil,nil)
} */

func processTestEventsWithRealForwarder(t *testing.T, conf map[string]interface{}, outputDir string, backgroundfunc *func(), shutdown *func()) {

	t.Logf("Tring to preform test")

	if backgroundfunc != nil {
		t.Logf("Executing background func")
		go (*backgroundfunc)()
	}

	t.Logf("Background running...continue to test...")

	formats := [2]string{"json", "protobuf"}

	sigs := make(chan os.Signal)

	mockConn := consumer.MockAMQPConnection{AMQPURL: "amqp://cb:lol@localhost:5672"}

	mockChan, _ := mockConn.Channel()

	mockDialer := consumer.MockAMQPDialer{Connection: mockConn}

	cbef := cbeventforwarder.GetCbEventForwarderFromCfg(conf, mockDialer)

	go cbef.Go(sigs, nil)

	for _, format := range formats {

		pathname := path.Join("../test/raw_data", format)
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

				exchange := "api.events"
				contentType := "application/json"
				if format == "json" {
					exchange = "api.events"
				}
				if format == "protobuf" {
					exchange = "api.rawsensordata"
					contentType = "application/protobuf"
				}
				if err = mockChan.Publish(
					exchange,   // publish to an exchange
					routingKey, // routing to 0 or more queues
					false,      // mandatory
					false,      // immediate
					amqp.Publishing{
						Headers:         amqp.Table{},
						ContentType:     contentType,
						ContentEncoding: "",
						Body:            b,
						DeliveryMode:    amqp.Transient, // 1=non-persistent, 2=persistent
						Priority:        0,              // 0-
					},
				); err != nil {
					t.Errorf("Failed to publish %s %s: %s", exchange, routingKey, err)
				}
			}
		}
	}
	t.Logf("Done with test  ")

	if shutdown != nil {
		(*shutdown)()
	}

	sigs <- syscall.SIGTERM
}
