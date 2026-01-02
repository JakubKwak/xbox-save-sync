package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Store struct {
	client  *s3.Client
	bucket  string
	prefix  string
	region  string
	timeout time.Duration
}

func New(bucket string, prefix string, region string) (*Store, error) {
	ctx := context.Background()
	// TODO configure
	sdkConfig, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	s3Client := s3.NewFromConfig(sdkConfig)

	return &Store{
		client:  s3Client,
		bucket:  bucket,
		prefix:  prefix,
		region:  region,
		timeout: 30 * time.Second,
	}, nil
}

func (s *Store) Download(filename string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	key := s.prefix + "/" + filename

	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (s *Store) Upload(filename string, data []byte) error {
	if len(data) == 0 {
		return errors.New("no data to upload")
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	key := s.prefix + "/" + filename

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   io.NopCloser(io.Reader(bytes.NewReader(data))),
	})

	return err
}
