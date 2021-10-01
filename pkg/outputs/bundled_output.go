package outputs

import (
	"errors"
	. "github.com/carbonblack/cb-event-forwarder/pkg/config"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
)

type UploadStatus struct {
	fileName string
	result   error
	status   int
}

type BundledOutput struct {
	Behavior BundleBehavior

	tempFileDirectory string
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

	Config *Configuration

	// TODO: make this thread-safe from the status page
	sync.RWMutex
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
	Initialize(connString string) error
	Statistics() interface{}
	Key() string
	String() string
}

func (o *BundledOutput) uploadOne(fileName string) {
	fp, err := os.OpenFile(fileName, os.O_RDONLY, 0644)
	if err != nil {
		o.fileResultChan <- UploadStatus{fileName: fileName, result: err}
		return
	}

	fileInfo, err := fp.Stat()
	if err != nil {
		o.fileResultChan <- UploadStatus{fileName: fileName, result: err}
		fp.Close()
		return
	}
	if fileInfo.Size() > 0 || o.Config.UploadEmptyFiles {
		// only upload if the file size is greater than zero
		uploadStatus := o.Behavior.Upload(fileName, fp)
		err = uploadStatus.result
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
	fp, err := os.Open(o.tempFileDirectory)
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
			o.filesToUpload = append(o.filesToUpload, filepath.Join(o.tempFileDirectory, fn))
		}
	}
}

func (o *BundledOutput) Initialize(connString string) error {
	o.fileResultChan = make(chan UploadStatus)
	o.filesToUpload = make([]string, 0)

	// maximum file size before we trigger an upload is ~10MB.
	o.maxFileSize = o.Config.BundleSizeMax

	// roll over duration defaults to five minutes
	o.rollOverDuration = o.Config.BundleSendTimeout

	parts := strings.SplitN(connString, ":", 2)

	var firstPartIsPath = parts[0][0] == '/'

	if len(parts) > 1 && parts[0] != "http" && parts[0] != "https" && firstPartIsPath {
		o.tempFileDirectory = parts[0]
		connString = parts[1]
	} else {
		// temporary file location
		o.tempFileDirectory = "/var/cb/data/event-forwarder"
	}

	if o.Behavior == nil {
		return errors.New("BundledOutput Initialize called without a behavior")
	}

	if err := o.Behavior.Initialize(connString); err != nil {
		return err
	}

	if err := os.MkdirAll(o.tempFileDirectory, 0700); err != nil {
		return err
	}

	currentPath := filepath.Join(o.tempFileDirectory, "event-forwarder")

	o.tempFileOutput = &FileOutput{Config: o.Config}
	err := o.tempFileOutput.Initialize(currentPath)

	// find files in the output directory that haven't been uploaded yet and add them to the list
	// we ignore any errors that may occur during this process
	o.queueStragglers()

	return err
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
	if o.currentFileSize == 0 && !o.Config.UploadEmptyFiles {
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
		BundleSendTimeout:    int64(o.Config.BundleSendTimeout / time.Second),
		BundleSizeMax:        o.Config.BundleSizeMax,
		UploadEmptyFiles:     o.Config.UploadEmptyFiles,
	}
}

func (o *BundledOutput) Go(messages <-chan string, signals <-chan os.Signal, exitCond *sync.Cond) error {
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)

		defer SignalExitCond(exitCond)
		defer refreshTicker.Stop()
		defer o.tempFileOutput.closeFile()
		defer o.tempFileOutput.flushOutput(true)

		for {
			select {
			case message := <-messages:
				if err := o.output(message); err != nil && !o.Config.DryRun {
					log.Errorf("Error during output %s", err)
					return
				}

			case <-refreshTicker.C:
				if time.Now().Sub(o.tempFileOutput.lastRolledOver) > o.rollOverDuration {
					if err := o.rollOver(); err != nil {
						log.Errorf("Error rolling temp file %s", err)
						return
					}
				}

				if len(o.filesToUpload) > 0 {
					var fn string
					fn, o.filesToUpload = o.filesToUpload[0], o.filesToUpload[1:]
					go o.uploadOne(fn)
				}

			case fileResult := <-o.fileResultChan:
				if fileResult.result != nil {
					o.uploadErrors++
					o.lastUploadError = fileResult.result.Error()
					o.lastUploadErrorTime = time.Now()
					// Handle 400s - lets stop processing the file and move it to debug zone
					if fileResult.status != 400 {
						// our default behavior is to try and upload the file next time around...
						o.filesToUpload = append(o.filesToUpload, fileResult.fileName)
					} else {
						// if we receive HTTP 400 error code (Bad Request), we assume the error is "permanent" and
						//  due not to some transient issue on the server side (overloading, service not available, etc)
						//  and instead an issue with the data we've sent. So move the file to the debug area and
						//  don't try to upload it again.
						o.Config.MoveFileToDebug(fileResult.fileName)
					}

					log.Infof("Error uploading file %s: %s", fileResult.fileName, fileResult.result)
				} else {
					o.successfulUploads++
					o.lastSuccessfulUpload = time.Now()
					log.Infof("Successfully uploaded file %s to %s.", fileResult.fileName, o.Behavior.String())
				}
			case signal := <-signals:
				switch signal {
				case syscall.SIGHUP:
					// flush to S3 immediately
					log.Infof("Received SIGHUP, sending data to %s immediately.", o.Behavior.String())
					if err := o.rollOver(); err != nil {
						log.Errorf("Error Flushing output %s", err)
						return
					}
				case syscall.SIGTERM, syscall.SIGINT:
					// handle exit gracefully
					return
				}
			}
		}
	}()

	return nil
}
