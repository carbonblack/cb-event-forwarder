package tests

import (
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/stretchr/testify/mock"
	"io"
	"os"
)

type MockS3 struct {
	mock.Mock
	outfile *os.File
}

func (ms3 *MockS3) PutObject(input *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
	buffer := make([]byte, 1024)
	for {
		read, err := input.Body.Read(buffer)
		if read == 0 && err == io.EOF {
			break
		}
		ms3.outfile.Write(buffer)
	}
	return nil, nil
}
func (ms3 *MockS3) HeadBucket(inhb *s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
	return nil, nil
}
