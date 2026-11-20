package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/config"
)

// ObjectStorage abstracts local-disk and S3-compatible backends.
type ObjectStorage interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Open(ctx context.Context, key string) (io.ReadSeekCloser, error)
	Delete(ctx context.Context, key string) error
}

// PutFile is a convenience helper computing the SHA-256 digest along the way.
func PutFile(ctx context.Context, s ObjectStorage, key string, r io.Reader) (sha string, err error) {
	tmp, err := os.CreateTemp("", "ctc-upload-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())

	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		return "", err
	}
	defer tmp.Close()

	ct := detectContentType(key)
	if err := s.Put(ctx, key, tmp, size, ct); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func detectContentType(key string) string {
	if ct := mime.TypeByExtension(filepath.Ext(key)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// ---------- local disk ----------

type LocalStorage struct{ Root string }

func NewLocalStorage(root string) (*LocalStorage, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &LocalStorage{Root: root}, nil
}

func safeKey(key string) (string, error) {
	clean := filepath.Clean("/" + strings.TrimPrefix(key, "/"))
	if strings.Contains(clean, "..") {
		return "", fmt.Errorf("invalid object key %q", key)
	}
	return clean, nil
}

func (l *LocalStorage) path(key string) (string, error) {
	k, err := safeKey(key)
	if err != nil {
		return "", err
	}
	return filepath.Join(l.Root, k), nil
}

func (l *LocalStorage) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (l *LocalStorage) Open(_ context.Context, key string) (io.ReadSeekCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (l *LocalStorage) Delete(_ context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// ---------- S3 / MinIO ----------

type S3Storage struct {
	client *minio.Client
	bucket string
}

func NewS3Storage(ctx context.Context, cfg *config.Config) (*S3Storage, error) {
	client, err := minio.New(cfg.S3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.S3AccessKey, cfg.S3SecretKey, ""),
		Secure: cfg.S3UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("init s3 client: %w", err)
	}
	bctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.MakeBucket(bctx, cfg.S3Bucket, minio.MakeBucketOptions{}); err != nil {
		exists, berr := client.BucketExists(bctx, cfg.S3Bucket)
		if !exists || berr != nil {
			return nil, fmt.Errorf("ensure s3 bucket: %w", err)
		}
	}
	return &S3Storage{client: client, bucket: cfg.S3Bucket}, nil
}

func (s *S3Storage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *S3Storage) Open(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err := obj.Seek(0, io.SeekStart); err != nil {
		obj.Close()
		return nil, err
	}
	return obj, nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

// NewStorage returns the configured backend; local disk is the default for dev/edge.
func NewStorage(ctx context.Context, cfg *config.Config, log *slog.Logger) (ObjectStorage, error) {
	if cfg.StorageMode == "s3" {
		return NewS3Storage(ctx, cfg)
	}
	log.Info("using local disk object storage", "dir", cfg.StorageDir)
	return NewLocalStorage(cfg.StorageDir)
}
