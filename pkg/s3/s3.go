package s3

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/getsentry/sentry-go"
)

type S3BucketInfo struct {
	S3Acl        string
	S3Region     string
	S3Endpoint   string
	S3BucketName string
}

type Credentials struct {
	UserName string
	Password string
}

type S3ConnectParams struct {
	S3Region          string
	S3AccessKeyId     string
	S3SecretAccessKey string
}

type S3Upload struct {
	BucketName    string
	Path          string
	ActionType    string
	Data          io.ReadCloser
	ConnectParams *S3ConnectParams
	GzipFileName  string
	Size          int64
	GzipSize      int64
}

type S3FileInfo struct {
	FileName string
	FileSize int64
}

var S3AccessKeyId string
var S3SecretAccessKey string
var S3BucketInfoValues S3BucketInfo

func S3Session(params S3ConnectParams) (*session.Session, error) {
	var err error = nil
	var s3Session *session.Session
	var awsConfig aws.Config

	awsConfig = aws.Config{
		Region:      aws.String(S3BucketInfoValues.S3Region),
		Endpoint:    aws.String(S3BucketInfoValues.S3Endpoint),
		Credentials: credentials.NewStaticCredentials(S3AccessKeyId, S3SecretAccessKey, ""),
	}
	if s3Session, err = session.NewSession(&awsConfig); err != nil {
		log.Printf("ERROR NewSession err: %v\n", err)
		log.Fatal(err)
	}
	return s3Session, err
}

func UploadToS3(gzipFileName string, jsonBytes []byte, path string, actionType string) (string, int64, error) {
	var err error = nil
	var w *gzip.Writer
	var s3Session *session.Session
	var s3ConnectParams S3ConnectParams
	var s3Upload *S3Upload
	var result *s3manager.UploadOutput
	var uploader *s3manager.Uploader

	s3ConnectParams = S3ConnectParams{
		S3Region:          S3BucketInfoValues.S3Region,
		S3AccessKeyId:     S3AccessKeyId,
		S3SecretAccessKey: S3SecretAccessKey,
	}
	reader := io.NopCloser(strings.NewReader(string(jsonBytes)))
	s3Upload = &S3Upload{
		BucketName:    S3BucketInfoValues.S3BucketName,
		Path:          path,
		ActionType:    actionType,
		Data:          reader,
		ConnectParams: &s3ConnectParams,
	}

	defer s3Upload.Data.Close()
	s3Upload.GzipFileName = gzipFileName
	reader, writer := io.Pipe()

	go func() {
		if w, err = gzip.NewWriterLevel(writer, gzip.BestCompression); err != nil {
			log.Printf("ERROR in UploadToS3 NewWriterLevel: %v\n", err)
			sentry.CaptureException(err)
			return
		}
		if s3Upload.Size, err = io.Copy(w, s3Upload.Data); err != nil {
			log.Printf("ERROR in UploadToS3 Copy: %v\n", err)
			sentry.CaptureException(err)
			return
		}
		log.Printf("=== s3Upload.Size: %d\n", s3Upload.Size)
		w.Close()
		writer.Close()
	}()

	s3Session = session.Must(S3Session(*s3Upload.ConnectParams))
	uploadConfig := &s3manager.UploadInput{
		Bucket: aws.String(s3Upload.BucketName),
		Key:    aws.String(fmt.Sprintf("%s/%s", s3Upload.Path, s3Upload.GzipFileName)),
		ACL:    aws.String(S3BucketInfoValues.S3Acl),
		Body:   reader,
	}
	uploader = s3manager.NewUploader(s3Session, func(u *s3manager.Uploader) {
		u.LeavePartsOnError = true
	})
	if result, err = uploader.Upload(uploadConfig); err != nil {
		log.Printf("ERROR in Upload: %v\n", err)
		sentry.CaptureException(err)
		return "", 0, err
	}
	if result == nil {
		// FIXME!! check for error
	}
	return s3Upload.GzipFileName, s3Upload.Size, err
}

func DownloadFromS3(path string, gzipFileName string) (string, error) {
	var err error = nil
	var s3ConnectParams S3ConnectParams
	var gzSize int64
	var gzipSize int
	var r *gzip.Reader
	var s3Session *session.Session
	var downloader *s3manager.Downloader
	var gzData []byte
	var s3DownloadData []byte
	var fileContents string

	s3ConnectParams = S3ConnectParams{
		S3Region:          S3BucketInfoValues.S3Region,
		S3AccessKeyId:     S3AccessKeyId,
		S3SecretAccessKey: S3SecretAccessKey,
	}
	s3Session = session.Must(S3Session(s3ConnectParams))
	downloadConfig := &s3.GetObjectInput{
		Bucket: aws.String(S3BucketInfoValues.S3BucketName),
		Key:    aws.String(path + gzipFileName),
	}
	log.Printf("path: %v gzipFileName: %v\n", path, gzipFileName)
	awsConfig := aws.Config{
		Region:      aws.String(S3BucketInfoValues.S3Region),
		Endpoint:    aws.String(S3BucketInfoValues.S3Endpoint),
		Credentials: credentials.NewStaticCredentials(S3AccessKeyId, S3SecretAccessKey, ""),
	}

	svc := s3.New(session.New(&awsConfig))
	var objData *s3.HeadObjectOutput
	log.Printf("AWS_BUCKET_NAME: %v path: %v gzipFileName: %v\n", S3BucketInfoValues.S3BucketName, path, gzipFileName)
	if objData, err = svc.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(S3BucketInfoValues.S3BucketName),
		Key:    aws.String(path + gzipFileName),
	}); err != nil {
		log.Printf("ERROR in getting HeadObject: %v", err)
		sentry.CaptureException(err)
		return "", err
	}
	gzipSize = int(*objData.ContentLength)
	gzData = make([]byte, gzipSize, gzipSize)
	writeAtBuffer := aws.NewWriteAtBuffer(gzData)
	downloader = s3manager.NewDownloader(s3Session)
	if gzSize, err = downloader.Download(writeAtBuffer, downloadConfig); err != nil {
		log.Printf("ERROR in Download: %v\n", err)
		sentry.CaptureException(err)
		return "", err
	}
	if gzSize == 0 {
		// FIXME eventually error message??
	}
	//log.Printf("gzSize: %d\n", gzSize)
	if r, err = gzip.NewReader(bytes.NewReader(gzData)); err != nil {
		log.Printf("ERROR in DownloadFromS3 gzip.NewReader: %v\n", err)
		sentry.CaptureException(err)
		return "", err
	}
	if s3DownloadData, err = ioutil.ReadAll(r); err != nil {
		log.Printf("ERROR in ReadAll gzData: %v\n", err)
		sentry.CaptureException(err)
		return "", err
	}
	fileContents = string(s3DownloadData)
	return fileContents, err
}
