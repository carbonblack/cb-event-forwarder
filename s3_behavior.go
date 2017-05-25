package main

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type S3Behavior struct {
	bucketName string
	out        *s3.S3
	region     string
}

type S3Statistics struct {
	BucketName        string `json:"bucket_name"`
	Region            string `json:"region"`
	EncryptionEnabled bool   `json:"encryption_enabled"`
}

func (o *S3Behavior) Upload(fileName string, fp *os.File) UploadStatus {
	var baseName string

	//
	// If a prefix is specified then concatenate it with the Base of the filename
	//
	if config.S3ObjectPrefix != nil {
		prefix := *config.S3ObjectPrefix
		// cust_name=abc/ingest_dt=2017-05-11/format=cb_response/bucket=the-bucket,source=event-forwarder.2017-05-11T23:59:58
		if config.S3VerboseKey == true {
			current_time := time.Now().UTC()
			baseName = fmt.Sprintf("%s/ingest_dt=%s/format=cb_response/bucket=%s,source=%s", prefix, current_time.Format("2006-01-02"), o.bucketName, filepath.Base(fileName))
		} else {
			s := []string{prefix, filepath.Base(fileName)}
			baseName = strings.Join(s, "/")
		}
	} else {
		baseName = filepath.Base(fileName)
	}

	_, err := o.out.PutObject(&s3.PutObjectInput{
		Body:                 fp,
		Bucket:               &o.bucketName,
		Key:                  &baseName,
		ServerSideEncryption: config.S3ServerSideEncryption,
		ACL:                  config.S3ACLPolicy,
	})
	fp.Close()

	return UploadStatus{fileName: fileName, result: err}
}

func (o *S3Behavior) Initialize(connString string) error {
	// bucketName can either be a single value (just the bucket name itself, defaulting to "/var/cb/data/event-forwarder" as the
	// temporary file directory and "us-east-1" for the AWS region), or:
	//
	// if bucketName contains two colons, treat it as follows: (temp-file-directory):(region):(bucket-name)

	parts := strings.SplitN(connString, ":", 2)
	if len(parts) == 1 {
		o.bucketName = connString
		o.region = "us-east-1"
	} else if len(parts) == 2 {
		o.bucketName = parts[1]
		o.region = parts[0]
	} else {
		return errors.New(fmt.Sprintf("Invalid connection string: '%s' should look like (temp-file-directory):(region):bucket-name",
			connString))
	}

	awsConfig := &aws.Config{Region: aws.String(o.region)}
	if config.S3CredentialProfileName != nil {
		parts = strings.SplitN(*config.S3CredentialProfileName, ":", 2)
		credentialProvider := credentials.SharedCredentialsProvider{}

		if len(parts) == 2 {
			credentialProvider.Filename = parts[0]
			credentialProvider.Profile = parts[1]
		} else {
			credentialProvider.Profile = parts[0]
		}

		creds := credentials.NewCredentials(&credentialProvider)
		awsConfig.Credentials = creds
	}

	sess := session.New(awsConfig)
	o.out = s3.New(sess)

	_, err := o.out.HeadBucket(&s3.HeadBucketInput{Bucket: &o.bucketName})
	if err != nil {
		return errors.New(fmt.Sprintf("Could not open bucket %s: %s", o.bucketName, err))
	}

	return nil
}

func (o *S3Behavior) Key() string {
	return fmt.Sprintf("%s:%s", o.region, o.bucketName)
}

func (o *S3Behavior) String() string {
	return "AWS S3 " + o.Key()
}

func (o *S3Behavior) Statistics() interface{} {
	return S3Statistics{
		BucketName:        o.bucketName,
		Region:            o.region,
		EncryptionEnabled: config.S3ServerSideEncryption != nil,
	}
}
