package tests

import (
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"github.com/carbonblack/cb-event-forwarder/internal/output"
	"github.com/stretchr/testify/mock"
	"io/ioutil"
	"os"
	"path"
	"testing"
)

/*
  Test objects
*/

// MyMockedObject is a mocked object that implements an interface
// that describes an object that the code I am testing relies on.
type MyMockedOutputHandler struct {
	mock.Mock
}

// DoSomething is a method on MyMockedObject that implements some interface
// and just records the activity, and returns what the Mock object tells it to.
//
// In the real object, this method would do something useful, but since this
// is a mocked object - we're just going to stub it out.
//
// NOTE: This method is not being tested here, code that uses this object is.
func (m *MyMockedOutputHandler) Go(messages <-chan string, errorChan chan<- error) error {
	args := m.Called(messages, errorChan)
	return args.Error(1)
}

func (m *MyMockedOutputHandler) String() string {
	return "MockOutputHandler"
}

func (m *MyMockedOutputHandler) Statistics() interface{} {
	return nil
}

func (m *MyMockedOutputHandler) Key() string {
	return "MockOutputHandlerKey"
}

func (m *MyMockedOutputHandler) Initialize(string, *conf.Configuration) error {
	return nil
}

/*
  Actual test functions
*/

// TestSomethingElse is a second example of how to use our test object to
// make assertions about some target code we are testing.
// This time using a placeholder. Placeholders might be used when the
// data being passed in is normally dynamically generated and cannot be
// predicted beforehand (eg. containing hashes that are time sensitive)

func TestProcessEventsWithMockOutputHandler(t *testing.T) {

	// create an instance of our test object
	testOutputHandler := new(MyMockedOutputHandler)

	// setup expectations with a placeholder in the argument list
	testOutputHandler.On("Go", mock.Anything, mock.Anything).Return(nil)

	// call the code we are testing
	processTestEventsWithOutputHandler(t, "mock_output", jsonmessageprocessor.MarshalJSON, testOutputHandler)

	// assert that the expectations were met
	testOutputHandler.AssertExpectations(t)

}

func processTestEventsWithOutputHandler(t *testing.T, outputDir string, outputFunc outputMessageFunc, oh output.OutputHandler) {

	formats := [...]struct {
		formatType string
		process    func(string, []byte) ([]map[string]interface{}, error)
	}{{"json", jsmp.ProcessJSON}, {"protobuf", pbmp.ProcessProtobuf}}

	messages := make(chan string)
	errors := make(chan error)

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
				messages <- out
				oh.Go(messages, errors)
			}
		}
	}
}
