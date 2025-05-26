package minio

import (
	"bytes"
	"context"
	"fmt"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/qpoint-io/qtap/pkg/services/client"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
)

var tracer = telemetry.Tracer()

type S3ObjectStore struct {
	logger   *zap.Logger
	insecure bool
	client   *minio.Client
	bucket   string
}

func NewS3ObjectStore(logger *zap.Logger, endpoint, bucket, region, accessKey, secretKey string, insecure bool) (*S3ObjectStore, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure:    !insecure,
		Region:    region,
		Transport: client.NewHttpClient().Transport,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	return &S3ObjectStore{
		logger:   logger,
		insecure: insecure,
		client:   client,
		bucket:   bucket,
	}, nil
}

func (s *S3ObjectStore) Put(ctx context.Context, digest string, contentType string, data []byte) (map[string]string, error) {
	ctx, span := tracer.Start(ctx, "S3.Put")
	defer span.End()

	reader := bytes.NewReader(data)

	_, err := s.client.PutObject(ctx, s.bucket, digest, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upload object: %w", err)
	}

	// construct the permanent URL
	scheme := "http"
	if !s.insecure {
		scheme = "https"
	}

	s.logger.Debug("s3 object uploaded successfully",
		zap.String("bucket", s.bucket),
		zap.String("key", digest),
		zap.Int("size", len(data)))

	return map[string]string{
		"SCHEME":   scheme,
		"ENDPOINT": s.client.EndpointURL().Host,
		"BUCKET":   s.bucket,
		"DIGEST":   digest,
	}, nil
}
