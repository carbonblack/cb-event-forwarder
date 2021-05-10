package outputs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	. "github.com/carbonblack/cb-event-forwarder/pkg/config"
	log "github.com/sirupsen/logrus"
)

type S3Behavior struct {
	Config     *Configuration
	bucketName string
	out        *s3.S3
	region     string
}

func NewS3OutputFromConfig(cfg *Configuration) *BundledOutput {
	return &BundledOutput{Config: cfg, Behavior: &S3Behavior{Config: cfg}}
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
	if o.Config.S3ObjectPrefix != nil {
		s := []string{*o.Config.S3ObjectPrefix, filepath.Base(fileName)}
		baseName = strings.Join(s, "/")
	} else {
		baseName = filepath.Base(fileName)
	}

	_, err := o.out.PutObject(&s3.PutObjectInput{
		Body:                 fp,
		Bucket:               &o.bucketName,
		Key:                  &baseName,
		ServerSideEncryption: o.Config.S3ServerSideEncryption,
		ACL:                  o.Config.S3ACLPolicy,
	})
	fp.Close()

	log.WithFields(log.Fields{"Filename": fileName, "Bucket": &o.bucketName}).Debug("Uploading File to Bucket")

	return UploadStatus{fileName: fileName, result: err}
}

func (o *S3Behavior) Initialize(connString string) error {
	// bucketName can either be a single value (just the bucket name itself, defaulting to "/var/cb/data/event-forwarder" as the
	// temporary file directory and "us-east-1" for the AWS region), or:
	//
	// if bucketName contains two colons, treat it as follows: (temp-file-directory):(region):(bucket-name)

	parts := strings.SplitN(connString, ":", 2)
	switch len(parts) {
	case 1:
		o.bucketName = connString
		o.region = "us-east-1"
	case 2:
		o.bucketName = parts[1]
		o.region = parts[0]
	default:
		return fmt.Errorf("Invalid connection string: '%s' should look like (temp-file-directory):(region):bucket-name",
			connString)
	}

	awsConfig := &aws.Config{Region: aws.String(o.region)}

	if o.Config.S3Endpoint != nil {
		awsConfig = &aws.Config{Endpoint: aws.String(*o.Config.S3Endpoint), Region: aws.String(o.region), DisableSSL: aws.Bool(true),
			S3ForcePathStyle: aws.Bool(true)}
	}

	if o.Config.S3UseDualStack == true {
		awsConfig.WithUseDualStack(true)
	}

	if o.Config.S3CredentialProfileName != nil {
		parts = strings.SplitN(*o.Config.S3CredentialProfileName, ":", 2)
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
		// converting this to a warning, as you could have buckets with PutObject rights but not ListBucket
		log.Infof("Could not open bucket %s: %s", o.bucketName, err)
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
		EncryptionEnabled: o.Config.S3ServerSideEncryption != nil,
	}
}
