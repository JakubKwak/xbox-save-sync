package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
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

	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	sdkConfig, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	s3Client := s3.NewFromConfig(sdkConfig)

	return &Store{
		client:  s3Client,
		bucket:  bucket,
		prefix:  strings.Trim(prefix, "/"),
		region:  region,
		timeout: 5 * time.Minute,
	}, nil
}

func (s *Store) key(filename string) string {
	if s.prefix == "" {
		return filename
	}
	return s.prefix + "/" + filename
}

func (s *Store) Download(filename string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	key := s.key(filename)

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

	key := s.key(filename)
	length := int64(len(data))
	reader := bytes.NewReader(data)

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          reader,
		ContentLength: &length,
	})

	return err
}
