package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Storage struct {
	client    *s3.Client
	bucket    string
	cdnDomain string // if set, returned URLs use this instead of bucket name
	endpoint  string // MinIO/custom endpoint for URL generation
}

// NewS3StorageFromEnv creates an S3Storage from environment variables.
// Returns nil if S3_BUCKET is not set.
//
// Environment variables:
//   - S3_BUCKET (required)
//   - S3_REGION (default: us-west-2)
//   - S3_ENDPOINT (optional; set to MinIO/localstack URL for local dev, e.g. http://localhost:9000)
//   - AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY (optional; falls back to default credential chain)
func NewS3StorageFromEnv() *S3Storage {
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		slog.Info("S3_BUCKET not set, file upload disabled")
		return nil
	}

	region := os.Getenv("S3_REGION")
	if region == "" {
		region = "us-west-2"
	}

	opts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}

	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKey != "" && secretKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		slog.Error("failed to load AWS config", "error", err)
		return nil
	}

	endpoint := os.Getenv("S3_ENDPOINT")
	cdnDomain := os.Getenv("CLOUDFRONT_DOMAIN")

	var client *s3.Client
	if endpoint != "" {
		// MinIO / localstack / custom S3-compatible endpoint.
		// Path-style is right for MinIO / LocalStack but wrong for
		// Volcengine TOS (returns 403 InvalidPathAccess). Default to
		// path-style for backwards compatibility; set S3_USE_PATH_STYLE=false
		// in .env for TOS.
		usePathStyle := true
		if v := os.Getenv("S3_USE_PATH_STYLE"); v != "" {
			usePathStyle = v == "true" || v == "1" || v == "yes"
		}
		client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = usePathStyle
		})
		slog.Info("S3 storage initialized (custom endpoint)", "bucket", bucket, "endpoint", endpoint, "path_style", usePathStyle)
	} else {
		client = s3.NewFromConfig(cfg)
		slog.Info("S3 storage initialized", "bucket", bucket, "region", region, "cdn_domain", cdnDomain)
	}

	return &S3Storage{
		client:    client,
		bucket:    bucket,
		cdnDomain: cdnDomain,
		endpoint:  endpoint,
	}
}

// SanitizeFilename removes characters that could cause header injection in Content-Disposition.
func SanitizeFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		// Strip control chars, newlines, null bytes, quotes, semicolons, backslashes
		if r < 0x20 || r == 0x7f || r == '"' || r == ';' || r == '\\' || r == '\x00' {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// KeyFromURL extracts the S3 object key from a CDN or bucket URL.
// e.g. "https://multica-static.copilothub.ai/abc123.png" → "abc123.png"
func (s *S3Storage) KeyFromURL(rawURL string) string {
	// Strip the "https://domain/" prefix.
	for _, prefix := range []string{
		"https://" + s.cdnDomain + "/",
		"https://" + s.bucket + "/",
	} {
		if strings.HasPrefix(rawURL, prefix) {
			return strings.TrimPrefix(rawURL, prefix)
		}
	}
	// Fallback: take everything after the last "/".
	if i := strings.LastIndex(rawURL, "/"); i >= 0 {
		return rawURL[i+1:]
	}
	return rawURL
}

// Download streams an object from S3/TOS. Callers must Close the body.
// The content-type is returned so the proxy endpoint can forward it.
func (s *S3Storage) Download(ctx context.Context, key string) (body io.ReadCloser, contentType string, contentLength int64, err error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", 0, fmt.Errorf("s3 GetObject: %w", err)
	}
	ct := ""
	if out.ContentType != nil {
		ct = *out.ContentType
	}
	var cl int64
	if out.ContentLength != nil {
		cl = *out.ContentLength
	}
	return out.Body, ct, cl, nil
}

// Delete removes an object from S3. Errors are logged but not fatal.
func (s *S3Storage) Delete(ctx context.Context, key string) {
	if key == "" {
		return
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		slog.Error("s3 DeleteObject failed", "key", key, "error", err)
	}
}

// DeleteKeys removes multiple objects from S3. Best-effort, errors are logged.
func (s *S3Storage) DeleteKeys(ctx context.Context, keys []string) {
	for _, key := range keys {
		s.Delete(ctx, key)
	}
}

func (s *S3Storage) Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error) {
	return s.putObject(ctx, key, bytes.NewReader(data), contentType, filename)
}

// UploadReader streams body directly to S3 via PutObject without buffering
// the full payload in memory. body should be an io.ReadSeeker (e.g. a
// multipart.File) so the SDK can retry by seeking back to 0; otherwise
// the SDK may buffer for retry support.
func (s *S3Storage) UploadReader(ctx context.Context, key string, body io.Reader, contentType string, filename string) (string, error) {
	return s.putObject(ctx, key, body, contentType, filename)
}

func (s *S3Storage) putObject(ctx context.Context, key string, body io.Reader, contentType string, filename string) (string, error) {
	safe := SanitizeFilename(filename)
	input := &s3.PutObjectInput{
		Bucket:             aws.String(s.bucket),
		Key:                aws.String(key),
		Body:               body,
		ContentType:        aws.String(contentType),
		ContentDisposition: aws.String(fmt.Sprintf(`inline; filename="%s"`, safe)),
		CacheControl:       aws.String("max-age=432000,public"),
	}
	// Only set IntelligentTiering for AWS S3 — MinIO/custom endpoints don't support it.
	if s.endpoint == "" {
		input.StorageClass = types.StorageClassIntelligentTiering
	}
	_, err := s.client.PutObject(ctx, input)
	if err != nil {
		return "", fmt.Errorf("s3 PutObject: %w", err)
	}

	var link string
	if s.endpoint != "" {
		// MinIO path-style URL: http://localhost:9000/bucket/key
		link = fmt.Sprintf("%s/%s/%s", strings.TrimRight(s.endpoint, "/"), s.bucket, key)
	} else if s.cdnDomain != "" {
		link = fmt.Sprintf("https://%s/%s", s.cdnDomain, key)
	} else {
		link = fmt.Sprintf("https://%s/%s", s.bucket, key)
	}
	return link, nil
}
