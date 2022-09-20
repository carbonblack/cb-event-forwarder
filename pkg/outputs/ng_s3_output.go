package outputs

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	. "github.com/carbonblack/cb-event-forwarder/pkg/config"
	log "github.com/sirupsen/logrus"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type NGS3Output struct {
	Config            *Configuration
	bucketName        string
	region            string
	s3API             *s3.S3
	s3Session         *session.Session
	rollOverDuration  time.Duration
	chunkingPublisher *S3ChunkingPublisher
}

func NewNGS3OutputFromConfig(cfg *Configuration) *BaseOutput {
	return &BaseOutput{OutputHandler: &NGS3Output{Config: cfg}}
}

func (so *NGS3Output) String() string {
	return fmt.Sprintf("%s:%s", so.region, so.bucketName)
}

func (so *NGS3Output) Statistics() interface{} {
	return nil
}

func (so *NGS3Output) Key() string {
	return "s3out"
}

func (so *NGS3Output) Initialize(connString string) error {
	parts := strings.Split(connString, ":")
	switch len(parts) {
	case 1:
		so.bucketName = connString
		so.region = "us-east-1"
	case 2:
		so.region = parts[0]
		so.bucketName = parts[1]
	case 3:
		tempFileDirectory := parts[0]
		so.region = parts[1]
		so.bucketName = parts[2]
		log.Debugf("Using temp file directory: %s", tempFileDirectory)
	default:
		return fmt.Errorf("Invalid connection string: '%s' should look like (temp-file-directory):(region):bucket-name",
			connString)
	}

	// roll over duration defaults to five minutes
	so.rollOverDuration = so.Config.BundleSendTimeout

	awsConfig := &aws.Config{Region: aws.String(so.region)}

	if so.Config.S3Endpoint != nil {
		awsConfig = &aws.Config{Endpoint: aws.String(*so.Config.S3Endpoint), Region: aws.String(so.region), DisableSSL: aws.Bool(true),
			S3ForcePathStyle: aws.Bool(true)}
	}

	if so.Config.S3UseDualStack == true {
		awsConfig.WithUseDualStack(true)
	}

	if so.Config.S3CredentialProfileName != nil {
		parts = strings.SplitN(*so.Config.S3CredentialProfileName, ":", 2)
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

	s3Session, err := session.NewSession(awsConfig)
	if err != nil {
		log.Errorf("Error establishing s3 sesison %e", err)
		return err
	}
	so.s3Session = s3Session
	so.s3API = s3.New(so.s3Session)

	_, err = so.s3API.HeadBucket(&s3.HeadBucketInput{Bucket: &so.bucketName})
	if err != nil {
		log.Errorf("Could not open bucket %s: %v", so.bucketName, err)
		return err
	}

	uploader := s3manager.NewUploader(s3Session)

	so.chunkingPublisher = NewS3ChunkingPublisher(so.Config, uploader, so.bucketName)

	return err
}

func (so *NGS3Output) Start() (err error) {
	err = so.chunkingPublisher.Start()
	if err != nil {
		log.Errorf("Error starting s3 output %v", err)
		return err
	}
	log.Debugf("NG S3 output starting %+v", so.chunkingPublisher)
	return nil
}

func (so *NGS3Output) ExitCleanup() {
	log.Debugf("NG S3 output before exit")
}

func (so *NGS3Output) HandleTick() error {
	return so.chunkingPublisher.RollChunkIfTimeElapsed(so.Config.UploadEmptyFiles, so.rollOverDuration)
}

func (so *NGS3Output) HandleMessage(message string) error {
	so.chunkingPublisher.Input <- message
	return nil
}

func (so *NGS3Output) HandleHup() error {
	so.chunkingPublisher.RollChunkIf(so.Config.UploadEmptyFiles)
	return nil
}

func (so *NGS3Output) HandleTerm() {
	so.chunkingPublisher.Stop()
}

type S3ChunkingPublisher struct {
	config *Configuration
	S3Publisher
	chunkers        []*S3OutputChunkWorker
	Input           chan string
	uploads         chan *S3OutputChunk
	uploadWaitGroup *sync.WaitGroup
	inputWaitGroup  *sync.WaitGroup
	bucketName      string
}

func NewS3ChunkingPublisher(cfg *Configuration, uploader WrappedUploader, bucketName string) *S3ChunkingPublisher {
	waitGroupInput := &sync.WaitGroup{}
	waitGroupUpload := &sync.WaitGroup{}
	uploads := make(chan *S3OutputChunk)
	inputs := make(chan string)
	chunkers := make([]*S3OutputChunkWorker, 0)
	publisher := NewS3Publisher(waitGroupUpload, uploader, uploads)
	return &S3ChunkingPublisher{config: cfg, chunkers: chunkers, Input: inputs, bucketName: bucketName, inputWaitGroup: waitGroupInput, uploadWaitGroup: waitGroupUpload, uploads: uploads, S3Publisher: publisher}
}

func (chunkingPublisher *S3ChunkingPublisher) Start() error {
	chunkingPublisher.S3Publisher.LaunchUploadWorkers(chunkingPublisher.config.S3Concurrency)
	err := chunkingPublisher.LaunchInputWorkers(chunkingPublisher.config.S3Concurrency)
	return err
}

func (chunkingPublisher *S3ChunkingPublisher) LaunchInputWorkers(workerNum int) error {
	log.Debugf("Launching %d input workers...", workerNum)
	for chunkerId := 0; chunkerId < workerNum; chunkerId++ {
		chunker, err := NewS3ChunkWorker(chunkingPublisher.config, chunkingPublisher.uploads, chunkingPublisher.config.BundleSizeMax, chunkingPublisher.config.BundleSizeMax/100, "event-forwarder", chunkingPublisher.bucketName)
		if err != nil {
			log.Errorf("Error making chunker %e", err)
			return err
		} else {
			chunkingPublisher.chunkers = append(chunkingPublisher.chunkers, chunker)
			go chunker.Work(chunkerId, chunkingPublisher.inputWaitGroup, chunkingPublisher.Input)
			log.Debugf("Launched input worker - %d", chunkerId)
		}
	}
	return nil
}

func (chunkingPublisher *S3ChunkingPublisher) RollChunkIf(uploadEmpty bool) (err error) {
	var rollError error = nil
	for i, chunker := range chunkingPublisher.chunkers {
		chunker.Lock()
		rollError = chunker.RollChunkIf(uploadEmpty)
		chunker.Unlock()

		if rollError != nil {
			if err == nil {
				err = fmt.Errorf("[worker-%d]:%v", i, rollError)
			} else {
				err = fmt.Errorf("%v\n[worker-%d]:%v", err, i, rollError)
			}
		}
	}
	return err
}

func (chunkingPublisher *S3ChunkingPublisher) Stop() {
	log.Debugf("Stopping s3 uploader")
	close(chunkingPublisher.Input)
	chunkingPublisher.inputWaitGroup.Wait()
	log.Debugf("Input workers exited...")
	close(chunkingPublisher.uploads)
	chunkingPublisher.uploadWaitGroup.Wait()
	log.Debugf("Output workers exited")
	log.Debugf("S3 uploader stopped OK")
}

func (chunkingPublisher *S3ChunkingPublisher) RollChunkIfTimeElapsed(uploadEmpty bool, duration time.Duration) (err error) {
	var rollError error = nil
	for i, chunker := range chunkingPublisher.chunkers {
		rollError = chunker.RollChunkIfTimeElapsed(uploadEmpty, duration)
		if rollError != nil {
			if err == nil {
				err = fmt.Errorf("[worker-%d]:%v", i, rollError)
			} else {
				err = fmt.Errorf("%v\n[worker-%d]:%v", err, i, rollError)
			}
		}
	}
	return err
}

type S3OutputChunkWorker struct {
	chunkSize     int64
	flushSize     int64
	baseFileName  string
	bucketName    string
	publishing    bool
	uploadOutputs chan<- *S3OutputChunk
	currentChunk  *S3OutputChunk
	config        *Configuration
	sync.RWMutex
}

type S3OutputChunk struct {
	config           *Configuration
	reader           *io.PipeReader
	writer           FlushableWriteCloser
	baseWriter       *io.PipeWriter
	chunkSize        int64
	flushSize        int64
	bytesRead        int64
	fileName         string
	bucketName       string
	currentByteCount int64
	sinceFlush       int64
	sent             bool
	createTime       time.Time
	Closed           bool
}

func NewS3OutputChunk(cfg *Configuration, chunkSize, flushSize int64, fileName, bucketName string) (*S3OutputChunk, error) {
	chunk := S3OutputChunk{config: cfg, Closed: false, bytesRead: 0, fileName: fileName, bucketName: bucketName, sent: false, chunkSize: chunkSize, flushSize: flushSize, sinceFlush: 0, currentByteCount: 0, createTime: time.Now()}
	err := chunk.initGzipStream()
	return &chunk, err
}

func (chunk *S3OutputChunk) MarkSent() {
	chunk.sent = true
}

func (chunk *S3OutputChunk) NeedsFlush() bool {
	return chunk.sinceFlush >= chunk.flushSize
}

func (chunk *S3OutputChunk) initGzipStream() (err error) {
	reader, writer := io.Pipe()
	chunk.reader = reader
	chunk.writer, err = chunk.config.WrapWriterWithCompressionSettings(writer)
	chunk.baseWriter = writer
	return err
}

func (chunk *S3OutputChunk) CloseChunkWriters() error {
	chunk.Closed = true
	writerCloseErr := chunk.writer.Close()
	baseWriterCloseErr := chunk.baseWriter.Close()
	if writerCloseErr != nil || baseWriterCloseErr != nil {
		return fmt.Errorf("CHUNK CLOSE ERRORS: %v %v", writerCloseErr, baseWriterCloseErr)
	}
	return nil
}

// Read is called by the S3 uploader. It is repeatedly called to pull data off the pipe, so it can be written
// to S3. Once the writer end of the pipe is closed, the current S3 upload will be completed and no more reading
// will be performed on that pipe.
func (chunk *S3OutputChunk) Read(buffer []byte) (n int, err error) {
	n, err = chunk.reader.Read(buffer)
	if err == nil {
		atomic.AddInt64(&chunk.bytesRead, int64(n))
	}
	return n, err
}

func (chunk *S3OutputChunk) CloseChunkReader() error {
	return chunk.reader.Close()
}

func (chunk *S3OutputChunk) Write(message string) error {
	writtenByteCount, err := chunk.writer.Write([]byte(message))
	if err == nil {
		chunk.addWrittenBytes(int64(writtenByteCount))
		return chunk.FlushIfNeeded()
	}
	return err
}

func (chunk *S3OutputChunk) flushWriter() error {
	err := chunk.writer.Flush()
	chunk.sinceFlush = 0
	return err
}
func (chunk *S3OutputChunk) FlushIfNeeded() error {
	if chunk.NeedsFlush() {
		return chunk.flushWriter()
	}
	return nil
}

func (chunk *S3OutputChunk) addWrittenBytes(n int64) {
	chunk.currentByteCount += n
	chunk.sinceFlush += n
}

func NewS3ChunkWorker(cfg *Configuration, uploads chan<- *S3OutputChunk, chunkSize, flushSize int64, fileName, bucketName string) (*S3OutputChunkWorker, error) {
	newChunk, err := NewS3OutputChunk(cfg, chunkSize, flushSize, fileName, bucketName)
	chunkWorker := S3OutputChunkWorker{config: cfg, bucketName: bucketName, uploadOutputs: uploads, currentChunk: newChunk, publishing: false, chunkSize: chunkSize, flushSize: flushSize, baseFileName: fileName}
	return &chunkWorker, err
}

func (chunkWorker *S3OutputChunkWorker) CurrentChunkTime() time.Time {
	return chunkWorker.currentChunk.createTime
}

func (chunkWorker *S3OutputChunkWorker) CloseCurrentChunk() error {
	chunkWorker.Lock()
	defer chunkWorker.Unlock()

	if chunkWorker.currentChunk.Closed {
		return nil
	}
	return chunkWorker.currentChunk.CloseChunkWriters()
}

func (chunkWorker *S3OutputChunkWorker) RollChunkIf(emptyOk bool) error {
	if emptyOk || (!emptyOk && chunkWorker.currentChunk.currentByteCount > 0) {
		log.Debugf("Rolling chunk due to timeout or termination.")
		return chunkWorker.RollChunkAndSend()
	}
	return nil
}

func (chunkWorker *S3OutputChunkWorker) RollChunkIfTimeElapsed(emptyOk bool, duration time.Duration) error {
	now := time.Now()

	chunkWorker.Lock()
	defer chunkWorker.Unlock()

	if now.Sub(chunkWorker.CurrentChunkTime()) >= duration {
		return chunkWorker.RollChunkIf(emptyOk)
	}
	return nil
}

// RollChunk ends the current chunk and creates another. Ending it is accomplished by closing the pipe writer
// which results in the read and then the S3 upload completing. The chunk is created before any data is received.
func (chunkWorker *S3OutputChunkWorker) RollChunk() error {
	err := chunkWorker.currentChunk.CloseChunkWriters()
	chunkWorker.currentChunk, err = NewS3OutputChunk(chunkWorker.config, chunkWorker.chunkSize, chunkWorker.flushSize, chunkWorker.baseFileName, chunkWorker.bucketName)
	return err
}

// RollChunkAndSend creates a new chunk and sends it to the thead doing the S3 uploading via a channel.
func (chunkWorker *S3OutputChunkWorker) RollChunkAndSend() error {
	err := chunkWorker.RollChunk()
	if err == nil {
		chunkWorker.SendChunk()
	}
	return err
}

// output writes data (previously received from RabbitMQ) to the pipe. Once the amount of data read from the pipe
// (yes, read from, not written to) exceeds the configured "chunkSize", the current chunk is completed and a new one
// is created.
func (chunkWorker *S3OutputChunkWorker) output(message string) (err error) {
	chunkWorker.Lock()
	defer chunkWorker.Unlock()

	err = chunkWorker.currentChunk.Write(message)
	if chunkWorker.currentChunk.Full() {
		return chunkWorker.RollChunkAndSend()
	}
	return err
}

// SendChunk - sends the details of a chunk (which includes the pipe being used to send data) and is one of the first
// things that is done. Doing this triggers the creation of a new S3 uploader, which will continuously read from
// the pipe until it is closed. This does not send RabbitMQ data, but rather a chunk object.
func (chunkWorker *S3OutputChunkWorker) SendChunk() {
	if !chunkWorker.currentChunk.sent {
		chunkWorker.uploadOutputs <- chunkWorker.currentChunk
		chunkWorker.currentChunk.MarkSent()
	}
}

func (chunkWorker *S3OutputChunkWorker) Work(workerId int, wg *sync.WaitGroup, input <-chan string) {
	wg.Add(1)
	defer chunkWorker.CloseCurrentChunk()
	defer log.Infof("[%d]Chunk worker exiting...", workerId)
	defer wg.Done()

	chunkWorker.Lock()
	chunkWorker.SendChunk()
	chunkWorker.Unlock()

	for inputData := range input {
		err := chunkWorker.output(inputData)
		if err != nil {
			log.Infof("[%d]Error in chunk worker %s", workerId, err)
			return
		}
	}
}

func (chunk *S3OutputChunk) PrepareS3UploadInput(workerId int) *s3manager.UploadInput {
	var baseName string

	//
	// If a prefix is specified then concatenate it with the Base of the filename
	//
	fileSuffix := chunk.config.FileExtensionForCompressionType()
	if chunk.config.S3ObjectPrefix != nil {
		s := []string{*chunk.config.S3ObjectPrefix, filepath.Base(chunk.fileName)}
		baseName = strings.Join(s, "/")
	} else {
		baseName = filepath.Base(chunk.fileName)
	}
	return &s3manager.UploadInput{
		Body:                 chunk,
		Bucket:               aws.String(chunk.bucketName),
		Key:                  aws.String(fmt.Sprintf("%s.%s-%d%s", baseName, time.Now().Format("2006-01-02T15:04:05.000"), workerId, fileSuffix)),
		ServerSideEncryption: chunk.config.S3ServerSideEncryption,
		ACL:                  chunk.config.S3ACLPolicy,
	}
}

func (chunk *S3OutputChunk) Full() bool {
	bytesRead := atomic.LoadInt64(&chunk.bytesRead)
	return bytesRead >= chunk.chunkSize
}

type S3Publisher struct {
	WrappedUploader
	uploadInputs <-chan *S3OutputChunk
	waitGroup    *sync.WaitGroup
}

func NewS3Publisher(waitGroup *sync.WaitGroup, uploader WrappedUploader, uploadInputs <-chan *S3OutputChunk) S3Publisher {
	return S3Publisher{waitGroup: waitGroup, WrappedUploader: uploader, uploadInputs: uploadInputs}
}

func (publisher *S3Publisher) LaunchUploadWorkers(workerNum int) {
	for workerId := 0; workerId < workerNum; workerId++ {
		go publisher.worker(workerId)
	}
}

func (publisher *S3Publisher) worker(workerId int) {
	NewS3PublisherWorker(workerId, publisher.waitGroup, publisher.uploadInputs, publisher.WrappedUploader).Work()
}

type WrappedUploader interface {
	Upload(input *s3manager.UploadInput, options ...func(*s3manager.Uploader)) (*s3manager.UploadOutput, error)
}

type S3PublisherWorker struct {
	uploads   <-chan *S3OutputChunk
	uploader  WrappedUploader
	workerId  int
	waitGroup *sync.WaitGroup
}

func NewS3PublisherWorker(workerId int, waitGroup *sync.WaitGroup, uploads <-chan *S3OutputChunk, publisher WrappedUploader) *S3PublisherWorker {
	return &S3PublisherWorker{waitGroup: waitGroup, uploads: uploads, uploader: publisher, workerId: workerId}
}

// Work waits for a new chunk on the channel and then uses it to setup a new S3 uploader. That uploader will
// make calls to the above S3OutputChunk.Read() to pull data off the internal pipe where we are also writing data
// that is received from RabbitMQ. Once the pipe's writer is closed, the data will be uploaded and this will again
// wait for the next chunk. Note that chunks are created before any data is received.
func (worker *S3PublisherWorker) Work() {
	worker.waitGroup.Add(1)
	defer log.Debugf("[WORKER%d] S3 uploader-worker exiting", worker.workerId)
	defer worker.waitGroup.Done()
	for inputChunk := range worker.uploads {
		s3Input := inputChunk.PrepareS3UploadInput(worker.workerId)
		uploadResult, err := worker.uploader.Upload(s3Input)
		inputChunk.CloseChunkReader()
		if err != nil {
			log.Errorf("[WORKER%d]-Upload error %v", worker.workerId, err)
		} else {
			log.Debugf("[WORKER%d]-Uploaded successfully %v", worker.workerId, uploadResult)
		}
	}
}
