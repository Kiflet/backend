package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Storage struct {
	client *s3.Client
	// presign signs URLs for browsers and phones, for the public endpoint
	// when there is one.
	presign *s3.PresignClient
	bucket  string
	region  string
}

// S3Config is an S3 bucket's settings.
type S3Config struct {
	Bucket, Region    string
	Endpoint          string // empty: AWS
	AccessKey, Secret string
	UseSSL            bool
	// ForcePathStyle addresses the bucket in the path
	// (https://files.example/kiflet/key) rather than the host name
	// (https://kiflet.files.example/key), which needs a wildcard name and
	// certificate. Garage behind Caddy wants it. Plain http:// endpoints
	// always use it.
	ForcePathStyle bool
	// PublicEndpoint is the address browsers and phones reach the store
	// at, which presigned URLs are signed for, when the app reaches it at
	// another (http://garage:3900 inside the compose network). Empty: the
	// same as Endpoint.
	PublicEndpoint string
}

// NewS3Storage connects to a bucket; nothing is sent until the first call.
func NewS3Storage(c S3Config) (*S3Storage, error) {
	if c.Bucket == "" {
		return nil, errors.New("bucket name is required")
	}
	if c.Region == "" {
		c.Region = "us-east-1"
	}

	var opts []func(*config.LoadOptions) error

	if c.AccessKey != "" && c.Secret != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(c.AccessKey, c.Secret, ""),
		))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	clientFor := func(endpoint string) *s3.Client {
		return s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.Region = c.Region
			if endpoint == "" {
				return
			}
			o.BaseEndpoint = aws.String(endpoint)
			if c.ForcePathStyle || (!c.UseSSL && strings.HasPrefix(endpoint, "http://")) {
				o.UsePathStyle = true
			}
			// S3-compatible stores differ in their support for the SDK's
			// default integrity checksums, and the SDK logs a warning for
			// every download that lacks one. Send and check them only
			// where S3 requires them.
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
			o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		})
	}
	client := clientFor(c.Endpoint)
	presign := s3.NewPresignClient(client)
	if c.PublicEndpoint != "" {
		presign = s3.NewPresignClient(clientFor(c.PublicEndpoint))
	}

	return &S3Storage{
		client:  client,
		presign: presign,
		bucket:  c.Bucket,
		region:  c.Region,
	}, nil
}

func (s *S3Storage) Upload(ctx context.Context, path string, reader io.Reader, contentType string) (string, error) {
	if path == "" {
		return "", ErrInvalidPath
	}

	path = strings.TrimPrefix(path, "/")

	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(path),
		Body:        reader,
		ContentType: aws.String(contentType),
	}

	_, err := s.client.PutObject(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to upload to S3: %w", err)
	}

	return path, nil
}

func (s *S3Storage) Download(ctx context.Context, path string) (io.ReadCloser, error) {
	if path == "" {
		return nil, ErrInvalidPath
	}

	path = strings.TrimPrefix(path, "/")

	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path),
	}

	result, err := s.client.GetObject(ctx, input)
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, ErrFileNotFound
		}
		return nil, fmt.Errorf("failed to download from S3: %w", err)
	}

	return result.Body, nil
}

func (s *S3Storage) Delete(ctx context.Context, path string) error {
	if path == "" {
		return ErrInvalidPath
	}

	path = strings.TrimPrefix(path, "/")

	input := &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path),
	}

	_, err := s.client.DeleteObject(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to delete from S3: %w", err)
	}

	return nil
}

func (s *S3Storage) GetURL(ctx context.Context, path string) (string, error) {
	if path == "" {
		return "", ErrInvalidPath
	}

	path = strings.TrimPrefix(path, "/")

	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path),
	}

	presignedReq, err := s.presign.PresignGetObject(ctx, input, s3.WithPresignExpires(15*time.Minute))
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return presignedReq.URL, nil
}

// PutURL is a presigned URL a phone or browser uploads one file to, of
// the content type given, for 15 minutes.
func (s *S3Storage) PutURL(ctx context.Context, path, contentType string) (string, error) {
	if path == "" {
		return "", ErrInvalidPath
	}
	req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(strings.TrimPrefix(path, "/")),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(15*time.Minute))
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}
	return req.URL, nil
}
