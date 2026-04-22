// Package storage: tos.go — Volcengine TOS adapter via S3-compatible
// API. TOS exposes an S3v4-signature endpoint, so we reuse
// aws-sdk-go-v2 with BaseEndpoint override (zero new dep). This file
// satisfies the Storage interface defined in storage.go.
//
// Why this layout (per plan §1):
//   - User picked option B (S3-compatible) over Volcengine official Go SDK.
//   - Same file_index row layout works for both S3 and TOS — backend
//     column says which one wrote storage_path.
//   - Presign needed for Doubao 妙记 to fetch audio without our keys.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// TOSConfig holds the per-workspace TOS credentials. Loaded from
// workspace_secret rows by storage.NewFromWorkspace; never read from
// process env (multi-tenant).
type TOSConfig struct {
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	Region          string // e.g. "cn-beijing"
	Endpoint        string // e.g. "https://tos-s3-cn-beijing.volces.com"
}

// TOSStorage implements Storage against a TOS bucket.
type TOSStorage struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
	endpoint  string
}

// NewTOSStorage builds a TOS-backed Storage. Returns error if config
// is incomplete; caller decides whether to fall back to S3 or fail.
func NewTOSStorage(ctx context.Context, cfg TOSConfig) (*TOSStorage, error) {
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("tos: access_key_id and secret_access_key required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("tos: bucket required")
	}
	if cfg.Region == "" {
		cfg.Region = "cn-beijing"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://tos-s3-" + cfg.Region + ".volces.com"
	}

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID, cfg.SecretAccessKey, "",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("tos: load aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = false // TOS prefers virtual-hosted style
	})
	return &TOSStorage{
		client:    client,
		presigner: s3.NewPresignClient(client),
		bucket:    cfg.Bucket,
		endpoint:  cfg.Endpoint,
	}, nil
}

func (t *TOSStorage) Backend() string { return BackendTOS }

func (t *TOSStorage) Put(ctx context.Context, key string, body io.Reader, contentType, filename string) (string, error) {
	// Buffer to []byte; TOS PutObject needs Content-Length, and
	// io.Reader without Seek can't be retried by the SDK.
	buf, err := io.ReadAll(body)
	if err != nil {
		return "", fmt.Errorf("tos: read body: %w", err)
	}
	disp := ""
	if filename != "" {
		disp = fmt.Sprintf(`inline; filename="%s"`, SanitizeFilename(filename))
	}
	input := &s3.PutObjectInput{
		Bucket: aws.String(t.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(buf),
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	if disp != "" {
		input.ContentDisposition = aws.String(disp)
	}
	if _, err := t.client.PutObject(ctx, input); err != nil {
		return "", fmt.Errorf("tos PutObject: %w", err)
	}
	return key, nil
}

func (t *TOSStorage) Get(ctx context.Context, storagePath string) (io.ReadCloser, error) {
	out, err := t.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(t.bucket),
		Key:    aws.String(storagePath),
	})
	if err != nil {
		return nil, fmt.Errorf("tos GetObject: %w", err)
	}
	return out.Body, nil
}

func (t *TOSStorage) Presign(ctx context.Context, storagePath string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	req, err := t.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(t.bucket),
		Key:    aws.String(storagePath),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("tos presign: %w", err)
	}
	return req.URL, nil
}

func (t *TOSStorage) Delete(ctx context.Context, storagePath string) error {
	if storagePath == "" {
		return nil
	}
	_, err := t.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(t.bucket),
		Key:    aws.String(storagePath),
	})
	if err != nil {
		// Treat NotFound as success — Delete is idempotent.
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "NoSuchKey") {
			return nil
		}
		return fmt.Errorf("tos DeleteObject: %w", err)
	}
	return nil
}
