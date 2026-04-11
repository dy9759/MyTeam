package storage

import "testing"

func TestBuildPutObjectInputSkipsStorageClassForCustomEndpoint(t *testing.T) {
	s := &S3Storage{
		bucket:   "myteam-files",
		endpoint: "http://localhost:9000",
	}

	input := s.buildPutObjectInput("hello.txt", []byte("hello"), "text/plain", "hello.txt")
	if input.StorageClass != "" {
		t.Fatalf("expected empty storage class for custom endpoint, got %q", input.StorageClass)
	}
}

func TestBuildPutObjectInputUsesTieringForAwsS3(t *testing.T) {
	s := &S3Storage{
		bucket: "myteam-files",
	}

	input := s.buildPutObjectInput("hello.txt", []byte("hello"), "text/plain", "hello.txt")
	if input.StorageClass == "" {
		t.Fatal("expected intelligent tiering storage class for aws s3")
	}
}
