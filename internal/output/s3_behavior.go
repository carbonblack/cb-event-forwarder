package output

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"strings"
)

type WrappedS3 interface {
	PutObject(input *s3.PutObjectInput) (*s3.PutObjectOutput, error)
	HeadBucket(inhb *s3.HeadBucketInput) (*s3.HeadBucketOutput, error)
}

type S3Behavior struct {
	bucketName string
	Out        WrappedS3
	region     string
	S3ServerSideEncryption string
	S3CredentialProfileName string
	S3ACLPolicy	string
	S3ObjectPrefix string
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
	if o.S3ObjectPrefix != "" {
		s := []string{o.S3ObjectPrefix, filepath.Base(fileName)}
		baseName = strings.Join(s, "/")
	} else {
		baseName = filepath.Base(fileName)
	}

	_, err := o.Out.PutObject(&s3.PutObjectInput{
		Body:                 fp,
		Bucket:               &o.bucketName,
		Key:                  &baseName,
		ServerSideEncryption: &(o.S3ServerSideEncryption),
		ACL:                  &(o.S3ACLPolicy),
	})
	fp.Close()

	log.WithFields(log.Fields{"Filename": fileName, "Bucket": &o.bucketName}).Debug("Uploading File to Bucket")

	return UploadStatus{FileName: fileName, Result: err}
}

func S3BehaviorFromCfg(cfg map[interface{}] interface{}) (S3Behavior, error) {
	var bucketName, region,credential_profile_name, server_side_encryption,acl_policy,object_prefix string
	if t, ok := cfg["bucket_name"]; ok {
		bucketName = t.(string)
	}
	if t, ok := cfg["region"]; ok {
		region = t.(string)
	}
	if t, ok := cfg["credential_profile_name"]; ok {
		credential_profile_name = t.(string)
	}
	if t, ok := cfg["server_side_encryption"]; ok {
		server_side_encryption = t.(string)
	}
	if t, ok := cfg["acl_policy"]; ok {
		acl_policy = t.(string)
	}
	if t, ok := cfg["object_prefix"]; ok {
		object_prefix = t.(string)
	}
	return NewS3Behavior(bucketName,region,credential_profile_name,server_side_encryption,acl_policy,object_prefix)
}

func NewS3Behavior(bucketName,region,s3CredentialProfileName ,s3ServerSideEncryption , s3ACLPolicy, s3ObjectPrefix string) (S3Behavior, error) {
	// bucketName can either be a single value (just the bucket name itself, defaulting to "/var/cb/data/event-forwarder" as the
	// temporary file directory and "us-east-1" for the AWS region), or:
	//
	// if bucketName contains two colons, treat it as follows: (temp-file-directory):(region):(bucket-name)
	temp := S3Behavior{bucketName:bucketName, region:region ,S3CredentialProfileName:s3CredentialProfileName, S3ServerSideEncryption:s3ServerSideEncryption, S3ACLPolicy:s3ACLPolicy, S3ObjectPrefix: s3ObjectPrefix}

	err := temp.NewSession()

	return temp, err
}

func (this *S3Behavior) NewSession() error {

	awsConfig := &aws.Config{Region: aws.String(this.region)}
	if this.S3CredentialProfileName != "" {
		parts := strings.SplitN(this.S3CredentialProfileName, ":", 2)
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
	if this.Out == nil {
		this.Out = s3.New(sess)
	}
	_, err := this.Out.HeadBucket(&s3.HeadBucketInput{Bucket: &this.bucketName})
	if err != nil {
		// converting this to a warning, as you could have buckets with PutObject rights but not ListBucket
		log.Infof("Could not open bucket %s: %s", this.bucketName, err)
	}

	return err
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
		EncryptionEnabled: o.S3ServerSideEncryption != "",
	}
}
