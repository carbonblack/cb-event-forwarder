package tests

import (
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/output"
	"testing"
	"fmt"
	"path"
	"os"
	"io/ioutil"
	"net"
	"io"
)

func TestFileOutput(t * testing.T) {
	var outputHandler output.OutputHandler = &output.FileOutput{Config:&config}
	outputDir := fmt.Sprintf("real_output_%v",outputHandler)
	err := outputHandler.Initialize(path.Join("../test_output/",outputDir,"filehandlerout"),&config)
	if err != nil {
		t.Errorf("%v",err)
		t.FailNow()
	}
	processTestEventsWithRealHandler(t,outputDir,jsonmessageprocessor.MarshalJSON,outputHandler,nil)
}

func TestNetOutput(t * testing.T) {
	var outputHandler output.OutputHandler = &output.NetOutput{Config:&config}
	endchannel := make(chan bool)

	outputDir := fmt.Sprintf("real_output_%v",outputHandler)

		listener, err := net.Listen("tcp", "127.0.0.1:41337")
		if err != nil {
			t.Errorf("Failed to open listening socket... %v ",err)
			t.FailNow()
			return
	    }
	    conn, err := listener.Accept()
	    if err != nil {
		    t.Errorf("Could not accept connection on listening socket %v ",err)
		    t.FailNow()
		    return
	    }
            go func (termchannel * chan bool) {
		    for  {
		            done := false
		            select {
		    		case b := <- *termchannel:
		    		   done = b
				default:
				    done = false
		    		}
		            if done {
		    		return
		            }
			    p := make([]byte, 4096)
			    _, err := io.ReadFull(conn, p)
			    if err == io.EOF {
				    t.Logf("GOT EOF ON LISTENING SOCKET!")
				    return
			    } else if err != nil {
				    t.Errorf("error at listening socket %v",err)
			    }
		    }
	    }(&endchannel)
	err = outputHandler.Initialize("127.0.0.1:41337",&config)
	if err != nil {
		t.Errorf("%v",err)
		t.FailNow()
	} else {
		t.Logf("Netoutput initialized succesfully, entering test")
	}
	processTestEventsWithRealHandler(t,outputDir,jsonmessageprocessor.MarshalJSON,outputHandler,&endchannel)
}


func processTestEventsWithRealHandler(t *testing.T, outputDir string, outputFunc outputMessageFunc, oh output.OutputHandler, endchannel *  chan bool) {
	t.Logf("Tring to preform test with %v",oh)
	formats := [...]struct {
		formatType string
		process    func(string, []byte) ([]map[string]interface{}, error)
	}{{"json", jsmp.ProcessJSON}, {"protobuf", pbmp.ProcessProtobuf}}

	messages := make(chan string)
	errors := make(chan error)

	go oh.Go(messages, errors)

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
			os.MkdirAll(path.Join("../test_output", outputDir), 0755)

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
				select {
					case messages <- out:
						t.Logf("Successfully wrote to oh channel")
					case e := <- errors:
						t.Errorf("%v",e)
					default:
						t.Errorf("Timed out trying to send %s to the output handler" , out)
				}
			}
		}
	}
	if endchannel != nil {
		t.Logf("Triggering end condition....finished processing input files and sending to OH")
		select {
			case *endchannel <- false:
				t.Logf("Succesfully send term signal to testcase")
			default:
				t.Logf("Something went wrong signalling endchannel")
		}
	}
}
