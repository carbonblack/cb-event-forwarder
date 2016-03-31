package main

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
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

type S3Output struct {
	bucketName string
	out        *s3.S3

	tempFileDirectory string
	tempFileOutput    *FileOutput
	region            string
	rollOverDuration  time.Duration
	currentFileSize   int64
	maxFileSize       int64

	lastUploadError     string
	lastUploadErrorTime time.Time
	uploadErrors        int64
	successfulUploads   int64
	fileResultChan      chan UploadStatus

	filesToUpload []string

	// TODO: make this thread-safe from the status page
	sync.RWMutex
}

type S3Statistics struct {
	BucketName    string      `json:"bucket_name"`
	Region        string      `json:"region"`
	FilesUploaded int64       `json:"files_uploaded"`
	UploadErrors  int64       `json:"upload_errors"`
	LastErrorTime time.Time   `json:"last_error_time"`
	LastErrorText string      `json:"last_error_text"`
	HoldingArea   interface{} `json:"file_holding_area"`

	EncryptionEnabled bool `json:"encryption_enabled"`
}

func (o *S3Output) uploadOne(fileName string) {
	var baseName string

	fp, err := os.OpenFile(fileName, os.O_RDONLY, 0644)
	if err != nil {
		o.fileResultChan <- UploadStatus{fileName: fileName, result: err}
	}

	//
	// If a prefix is specified then concatenate it with the Base of the filename
	//
	if config.S3ObjectPrefix != nil {
		s := []string{*config.S3ObjectPrefix, filepath.Base(fileName)}
		baseName = strings.Join(s, "/")
	} else {
		baseName = filepath.Base(fileName)
	}

	_, err = o.out.PutObject(&s3.PutObjectInput{
		Body:                 fp,
		Bucket:               &o.bucketName,
		Key:                  &baseName,
		ServerSideEncryption: config.S3ServerSideEncryption,
		ACL:                  config.S3ACLPolicy,
	})
	fp.Close()

	if err == nil {
		err = os.Remove(fileName)
		if err != nil {
			log.Printf("error removing %s: %s", fileName, err.Error())
		}
	}

	o.fileResultChan <- UploadStatus{fileName: fileName, result: err}
}

func (o *S3Output) queueStragglers() {
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

func (o *S3Output) Initialize(connString string) error {
	o.fileResultChan = make(chan UploadStatus)
	o.filesToUpload = make([]string, 0)

	// maximum file size before we trigger an upload is ~10MB.
	o.maxFileSize = 10 * 1024 * 1024

	// roll over duration defaults to five minutes
	o.rollOverDuration = 5 * time.Minute

	// bucketName can either be a single value (just the bucket name itself, defaulting to "/var/cb/data/event-forwarder" as the
	// temporary file directory and "us-east-1" for the AWS region), or:
	//
	// if bucketName contains two colons, treat it as follows: (temp-file-directory):(region):(bucket-name)

	parts := strings.SplitN(connString, ":", 3)
	if len(parts) == 1 {
		o.bucketName = connString
		o.tempFileDirectory = "/var/cb/data/event-forwarder"
		o.region = "us-east-1"
	} else if len(parts) == 3 {
		o.bucketName = parts[2]
		o.tempFileDirectory = parts[0]
		o.region = parts[1]
	} else {
		return errors.New(fmt.Sprintf("Invalid connection string: '%s' should look like (temp-file-directory):(region):(bucket-name)",
			connString))
	}

	awsConfig := &aws.Config{Region: aws.String(o.region)}
	if config.S3CredentialProfileName != nil {
		creds := credentials.NewCredentials(
			&credentials.SharedCredentialsProvider{Profile: *config.S3CredentialProfileName})
		awsConfig.Credentials = creds
	}

	sess := session.New(awsConfig)
	o.out = s3.New(sess)

	_, err := o.out.HeadBucket(&s3.HeadBucketInput{Bucket: &o.bucketName})
	if err != nil {
		return errors.New(fmt.Sprintf("Could not open bucket %s: %s", o.bucketName, err))
	}

	if err = os.MkdirAll(o.tempFileDirectory, 0700); err != nil {
		return err
	}

	currentPath := filepath.Join(o.tempFileDirectory, "event-forwarder")

	o.tempFileOutput = &FileOutput{}
	err = o.tempFileOutput.Initialize(currentPath)

	// find files in the output directory that haven't been uploaded yet and add them to the list
	// we ignore any errors that may occur during this process
	o.queueStragglers()

	return err
}

func (o *S3Output) output(message string) error {
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

func (o *S3Output) rollOver() error {
	fn, err := o.tempFileOutput.rollOverFile("2006-01-02T15:04:05")

	if err != nil {
		return err
	}

	go o.uploadOne(fn)
	o.currentFileSize = 0

	return nil
}

func (o *S3Output) Key() string {
	return fmt.Sprintf("%s:%s:%s", o.region, o.bucketName, o.tempFileDirectory)
}

func (o *S3Output) String() string {
	return "AWS S3 " + o.Key()
}

func (o *S3Output) Statistics() interface{} {
	return S3Statistics{
		BucketName:        o.bucketName,
		Region:            o.region,
		FilesUploaded:     o.successfulUploads,
		LastErrorTime:     o.lastUploadErrorTime,
		LastErrorText:     o.lastUploadError,
		UploadErrors:      o.uploadErrors,
		HoldingArea:       o.tempFileOutput.Statistics(),
		EncryptionEnabled: config.S3ServerSideEncryption != nil,
	}
}

func (o *S3Output) Go(messages <-chan string, errorChan chan<- error) error {
	if o.out == nil || o.tempFileOutput == nil {
		return errors.New("S3 output not initialized")
	}

	go func() {
		refreshTicker := time.NewTicker(1 * time.Second)
		defer refreshTicker.Stop()
		defer o.tempFileOutput.close()

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
					log.Printf("Successfully uploaded file %s to %s.", fileResult.fileName, o.bucketName)
				}

			case <-hup:
				// flush to S3 immediately
				log.Println("Received SIGHUP, sending data to S3 immediately.")
				if err := o.rollOver(); err != nil {
					errorChan <- err
					return
				}
			}
		}
	}()

	return nil
}
