package main

import (
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type UploadStatus struct {
	fileName string
	result   error
}

type BundledOutput struct {
	behavior BundleBehavior

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
	} else {
		if fileInfo.Size() > 0 || config.UploadEmptyFiles {
			// only upload if the file size is greater than zero
			uploadStatus := o.behavior.Upload(fileName, fp)
			err = uploadStatus.result
			o.fileResultChan <- uploadStatus
		}
	}

	fp.Close()

	if err == nil {
		// only remove the old file if there was no error
		err = os.Remove(fileName)
		if err != nil {
			log.Printf("error removing %s: %s", fileName, err.Error())
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
	o.maxFileSize = config.BundleSizeMax

	// roll over duration defaults to five minutes
	o.rollOverDuration = config.BundleSendTimeout

	parts := strings.SplitN(connString, ":", 2)
	if len(parts) > 1 && parts[0] != "http" && parts[0] != "https" {
		o.tempFileDirectory = parts[0]
		connString = parts[1]
	} else {
		// temporary file location
		o.tempFileDirectory = "/var/cb/data/event-forwarder"
	}

	if o.behavior == nil {
		return errors.New("BundledOutput Initialize called without a behavior")
	}

	if err := o.behavior.Initialize(connString); err != nil {
		return err
	}

	if err := os.MkdirAll(o.tempFileDirectory, 0700); err != nil {
		return err
	}

	currentPath := filepath.Join(o.tempFileDirectory, "event-forwarder")

	o.tempFileOutput = &FileOutput{}
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
	if o.currentFileSize == 0 && !config.UploadEmptyFiles {
		// don't upload zero length files if UploadEmptyFiles is false
		return nil
	}

	fn, err := o.tempFileOutput.rollOverFile("2006-01-02T15:04:05.000Z")

	if err != nil {
		return err
	}

	go o.uploadOne(fn)
	o.currentFileSize = 0

	return nil
}

func (o *BundledOutput) Key() string {
	return o.behavior.Key()
}

func (o *BundledOutput) String() string {
	return o.behavior.String()
}

func (o *BundledOutput) Statistics() interface{} {
	return BundleStatistics{
		FilesUploaded:        o.successfulUploads,
		LastErrorTime:        o.lastUploadErrorTime,
		LastErrorText:        o.lastUploadError,
		LastSuccessfulUpload: o.lastSuccessfulUpload,
		UploadErrors:         o.uploadErrors,
		HoldingArea:          o.tempFileOutput.Statistics(),
		StorageStatistics:    o.behavior.Statistics(),
		BundleSendTimeout:    int64(config.BundleSendTimeout / time.Second),
		BundleSizeMax:        config.BundleSizeMax,
		UploadEmptyFiles:     config.UploadEmptyFiles,
	}
}

func (o *BundledOutput) Go(messages <-chan string, errorChan chan<- error) error {
	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()
		defer o.tempFileOutput.closeFile()

		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)

		defer signal.Stop(hup)

		for {
			select {
			case message := <-messages:
				if err := o.output(message); err != nil {
					errorChan <- err
					return
				}

			case <-refreshTicker.C:
				if time.Now().Sub(o.tempFileOutput.lastRolledOver) > o.rollOverDuration {
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
				if fileResult.result != nil {
					o.uploadErrors += 1
					o.lastUploadError = fileResult.result.Error()
					o.lastUploadErrorTime = time.Now()

					o.filesToUpload = append(o.filesToUpload, fileResult.fileName)

					log.Printf("Error uploading file %s: %s", fileResult.fileName, fileResult.result)
				} else {
					o.successfulUploads += 1
					o.lastSuccessfulUpload = time.Now()
					log.Printf("Successfully uploaded file %s to %s.", fileResult.fileName, o.behavior.String())
				}

			case <-hup:
				// flush to S3 immediately
				log.Printf("Received SIGHUP, sending data to %s immediately.", o.behavior.String())
				if err := o.rollOver(); err != nil {
					errorChan <- err
					return
				}
			}
		}
	}()

	return nil
}
