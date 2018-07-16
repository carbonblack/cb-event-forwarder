package output

import (
	"errors"
	"github.com/carbonblack/cb-event-forwarder/internal/encoder"
	"github.com/carbonblack/cb-event-forwarder/internal/util"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type UploadStatus struct {
	FileName string
	Result   error
	Status   int
}

type BundledOutput struct {
	Behavior BundleBehavior

	TempFileDirectory string
	tempFileOutput    *FileOutput
	rollOverDuration  time.Duration
	currentFileSize   int64
	maxFileSize       int64

	lastUploadError      string
	lastUploadErrorTime  time.Time
	lastSuccessfulUpload time.Time

	uploadErrors      int64
	successfulUploads int64
	fileResultChan    chan UploadStatus

	filesToUpload []string

	// TODO: make this thread-safe from the status page
	sync.RWMutex

	UploadEmptyFiles bool
	DebugFlag        bool
	DebugStore       string

	Encoder encoder.Encoder
}

type BundleStatistics struct {
	FilesUploaded        int64       `json:"files_uploaded"`
	UploadErrors         int64       `json:"upload_errors"`
	LastErrorTime        time.Time   `json:"last_error_time"`
	LastErrorText        string      `json:"last_error_text"`
	LastSuccessfulUpload time.Time   `json:"last_successful_upload"`
	HoldingArea          interface{} `json:"file_holding_area"`
	StorageStatistics    interface{} `json:"storage_statistics"`
	BundleSendTimeout    int64       `json:"bundle_send_timeout"`
	BundleSizeMax        int64       `json:"bundle_size_max"`
	UploadEmptyFiles     bool        `json:"upload_empty_files"`
}

// Each bundled output plugin must implement the BundleBehavior interface, specifying how to upload files,
// initialize itself, and report back statistics.
type BundleBehavior interface {
	Upload(fileName string, fp *os.File) UploadStatus
	Statistics() interface{}
	Key() string
	String() string
}

func (o *BundledOutput) uploadOne(fileName string) {
	fp, err := os.OpenFile(fileName, os.O_RDONLY, 0644)
	if err != nil {
		o.fileResultChan <- UploadStatus{FileName: fileName, Result: err}
		return
	}

	fileInfo, err := fp.Stat()
	if err != nil {
		o.fileResultChan <- UploadStatus{FileName: fileName, Result: err}
		fp.Close()
		return
	}
	if fileInfo.Size() > 0 || o.UploadEmptyFiles {
		// only upload if the file size is greater than zero
		uploadStatus := o.Behavior.Upload(fileName, fp)
		err = uploadStatus.Result
		o.fileResultChan <- uploadStatus
	}

	fp.Close()

	if err == nil {
		// only remove the old file if there was no error
		err = os.Remove(fileName)
		if err != nil {
			log.Infof("error removing %s: %s", fileName, err.Error())
		}
	}
}

func (o *BundledOutput) queueStragglers() {
	fp, err := os.Open(o.TempFileDirectory)
	if err != nil {
		return
	}

	infos, err := fp.Readdir(0)
	if err != nil {
		return
	}

	for _, info := range infos {
		if info.IsDir() {
			continue
		}

		fn := info.Name()
		if !strings.HasPrefix(fn, "event-forwarder") {
			continue
		}

		if len(strings.TrimPrefix(fn, "event-forwarder")) > 0 {
			o.filesToUpload = append(o.filesToUpload, filepath.Join(o.TempFileDirectory, fn))
		}
	}
}

func NewBundledOutput(temp_file_directory string, bundle_size_max, bundle_send_timeout int64, upload_empty_files, debug bool, debugstore string, behavior BundleBehavior, encoder encoder.Encoder) (BundledOutput, error) {
	tempBundledOutput := BundledOutput{TempFileDirectory: temp_file_directory, rollOverDuration: time.Duration(bundle_send_timeout) * time.Second, UploadEmptyFiles: upload_empty_files, maxFileSize: bundle_size_max, Behavior: behavior, Encoder: encoder}
	tempBundledOutput.fileResultChan = make(chan UploadStatus)
	tempBundledOutput.filesToUpload = make([]string, 0)

	if tempBundledOutput.TempFileDirectory == "" {
		tempBundledOutput.TempFileDirectory = "/var/cb/data/event-forwarder"
	}

	if err := os.MkdirAll(tempBundledOutput.TempFileDirectory, 0700); err != nil {
		return tempBundledOutput, err
	}

	currentPath := filepath.Join(tempBundledOutput.TempFileDirectory, "event-forwarder")
	f := NewFileOutputHandler(currentPath, encoder)
	tempBundledOutput.tempFileOutput = &f
	// find files in the output directory that haven't been uploaded yet and add them to the list
	// we ignore any errors that may occur during this process
	tempBundledOutput.queueStragglers()

	return tempBundledOutput, nil
}

func (o *BundledOutput) output(message string) error {
	if o.currentFileSize+int64(len(message)) > o.maxFileSize {
		err := o.rollOver()
		if err != nil {
			return err
		}
	}

	// first try to write the message to our output file
	o.currentFileSize += int64(len(message))
	return o.tempFileOutput.output(message)
}

func (o *BundledOutput) rollOver() error {
	if o.currentFileSize == 0 && !o.UploadEmptyFiles {
		// don't upload zero length files if UploadEmptyFiles is false
		return nil
	}

	fn, err := o.tempFileOutput.rollOverFile("2006-01-02T15:04:05.000")

	if err != nil {
		return err
	}

	go o.uploadOne(fn)
	o.currentFileSize = 0

	return nil
}

func (o *BundledOutput) Key() string {
	return o.Behavior.Key()
}

func (o *BundledOutput) String() string {
	return o.Behavior.String()
}

func (o *BundledOutput) Statistics() interface{} {
	return BundleStatistics{
		FilesUploaded:        o.successfulUploads,
		LastErrorTime:        o.lastUploadErrorTime,
		LastErrorText:        o.lastUploadError,
		LastSuccessfulUpload: o.lastSuccessfulUpload,
		UploadErrors:         o.uploadErrors,
		HoldingArea:          o.tempFileOutput.Statistics(),
		StorageStatistics:    o.Behavior.Statistics(),
		BundleSendTimeout:    int64(o.rollOverDuration / time.Second),
		BundleSizeMax:        o.maxFileSize,
		UploadEmptyFiles:     o.UploadEmptyFiles,
	}
}

func (o *BundledOutput) Go(messages <-chan map[string]interface{}, errorChan chan<- error, controlchan <-chan os.Signal, wg sync.WaitGroup) error {
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)

		defer refreshTicker.Stop()
		defer o.tempFileOutput.closeFile()
		defer wg.Done()

		for {
			select {
			case message := <-messages:
				if encodedMsg, err := o.Encoder.Encode(message); err == nil {
					if err := o.output(encodedMsg); err != nil {
						errorChan <- err
						return
					}
				} else {
					errorChan <- err
				}

			case <-refreshTicker.C:
				if time.Now().Sub(o.tempFileOutput.LastRolledOver) > o.rollOverDuration {
					log.Infof("last rolled over = %s , duration = %s", o.tempFileOutput.LastRolledOver, o.rollOverDuration)
					if err := o.rollOver(); err != nil {
						errorChan <- err
						return
					}
				}

				if len(o.filesToUpload) > 0 {
					var fn string
					fn, o.filesToUpload = o.filesToUpload[0], o.filesToUpload[1:]
					go o.uploadOne(fn)
				}

			case fileResult := <-o.fileResultChan:
				if fileResult.Result != nil {
					o.uploadErrors++
					o.lastUploadError = fileResult.Result.Error()
					o.lastUploadErrorTime = time.Now()
					//Handle 400s - lets stop processing the file and move it to debug zone
					if fileResult.Status != 400 {
						// our default behavior is to try and upload the file next time around...
						o.filesToUpload = append(o.filesToUpload, fileResult.FileName)
					} else {
						// if we receive HTTP 400 error code (Bad Request), we assume the error is "permanent" and
						//  due not to some transient issue on the server side (overloading, service not available, etc)
						//  and instead an issue with the data we've sent. So move the file to the debug area and
						//  don't try to upload it again.
						util.MoveFileToDebug(o.DebugFlag, o.DebugStore, fileResult.FileName)
					}

					log.Infof("Error uploading file %s: %s", fileResult.FileName, fileResult.Result)
				} else {
					o.successfulUploads++
					o.lastSuccessfulUpload = time.Now()
					log.Infof("Successfully uploaded file %s to %s.", fileResult.FileName, o.Behavior.String())
				}
			case cmsg := <-controlchan:
				switch cmsg {
				case syscall.SIGHUP:
					// flush to S3 immediately
					log.Infof("Received SIGHUP, sending data to %s immediately.", o.Behavior.String())
					if err := o.rollOver(); err != nil {
						errorChan <- err
						return
					}
				case syscall.SIGTERM:
					// handle exit gracefully
					errorChan <- errors.New("SIGTERM received")
					refreshTicker.Stop()
					log.Info("Received SIGTERM. Exiting")
					return
				}
			}
		}
	}()

	return nil
}
