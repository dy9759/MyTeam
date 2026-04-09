#!/usr/bin/env bash
set -euo pipefail

# Ensure MinIO bucket exists for local file uploads
S3_ENDPOINT="${S3_ENDPOINT:-http://localhost:9000}"
S3_BUCKET="${S3_BUCKET:-myteam-files}"
AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-minioadmin}"
AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-minioadmin}"

if [ -z "$S3_BUCKET" ] || [ -z "$S3_ENDPOINT" ]; then
  echo "S3_BUCKET or S3_ENDPOINT not set, skipping MinIO setup"
  exit 0
fi

echo "==> Ensuring MinIO bucket '$S3_BUCKET' exists at $S3_ENDPOINT..."

# Wait for MinIO to be ready
for i in $(seq 1 15); do
  if curl -s "$S3_ENDPOINT/minio/health/ready" > /dev/null 2>&1; then
    break
  fi
  sleep 1
done

# Create bucket using AWS CLI or curl
if command -v aws &> /dev/null; then
  aws --endpoint-url "$S3_ENDPOINT" s3 mb "s3://$S3_BUCKET" 2>/dev/null || true
  # Set bucket policy to public-read for local dev
  aws --endpoint-url "$S3_ENDPOINT" s3api put-bucket-policy --bucket "$S3_BUCKET" --policy '{
    "Version": "2012-10-17",
    "Statement": [
      {
        "Effect": "Allow",
        "Principal": "*",
        "Action": "s3:GetObject",
        "Resource": "arn:aws:s3:::'"$S3_BUCKET"'/*"
      }
    ]
  }' 2>/dev/null || true
  echo "✓ MinIO bucket '$S3_BUCKET' ready"
else
  # Fallback: use curl to create bucket
  curl -s -X PUT "$S3_ENDPOINT/$S3_BUCKET" \
    -u "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY" \
    > /dev/null 2>&1 || true
  echo "✓ MinIO bucket '$S3_BUCKET' ready (created via curl)"
fi
