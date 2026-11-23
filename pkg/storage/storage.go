// Package storage is the package that handles the storage of pods
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	ErrFileNotFound  = errors.New("file not found")
	ErrInvalidPath   = errors.New("invalid file path")
	ErrInvalidConfig = errors.New("invalid storage configuration")
)

type Storage interface {
	Upload(ctx context.Context, path string, reader io.Reader, contentType string) (string, error)
	Download(ctx context.Context, path string) (io.ReadCloser, error)
	Delete(ctx context.Context, path string) error
	GetURL(ctx context.Context, path string) (string, error)
}

// Presigner is a store a client can upload to directly, by a presigned
// URL, without the bytes passing through the app.
type Presigner interface {
	PutURL(ctx context.Context, path string, p Put) (string, error)
}

// Put is what a presigned upload must send.
type Put struct {
	ContentType string
	// MD5 is the file's digest, which storage checks.
	MD5 []byte
	// Expires is how long the URL works; 15 minutes when zero.
	Expires time.Duration
}

type Config struct {
	Type string

	LocalPath string

	S3Bucket    string
	S3Region    string
	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3UseSSL    bool
	// S3ForcePathStyle and S3PublicEndpoint: see S3Config.
	S3ForcePathStyle bool
	S3PublicEndpoint string
}

func New(cfg *Config) (Storage, error) {
	switch cfg.Type {
	case "local", "":
		return NewLocalStorage(cfg.LocalPath)
	case "s3":
		return NewS3Storage(S3Config{
			Bucket: cfg.S3Bucket, Region: cfg.S3Region, Endpoint: cfg.S3Endpoint,
			AccessKey: cfg.S3AccessKey, Secret: cfg.S3SecretKey, UseSSL: cfg.S3UseSSL,
			ForcePathStyle: cfg.S3ForcePathStyle, PublicEndpoint: cfg.S3PublicEndpoint,
		})
	default:
		return nil, ErrInvalidConfig
	}
}
