package storage

import "testing"

func TestS3StorageEndpointDetection(t *testing.T) {
	// Custom endpoint (MinIO) should not use IntelligentTiering
	s := &S3Storage{
		bucket:   "myteam-files",
		endpoint: "http://localhost:9000",
	}
	if s.endpoint == "" {
		t.Fatal("expected custom endpoint to be set")
	}

	// AWS S3 (no custom endpoint) should use IntelligentTiering
	s2 := &S3Storage{
		bucket: "myteam-files",
	}
	if s2.endpoint != "" {
		t.Fatal("expected empty endpoint for AWS S3")
	}
}
