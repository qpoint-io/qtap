//go:build integration

package s3

import (
	"context"
	"os"
	"testing"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestS3Integration_WithEnvCredentials tests S3 with AWS environment credentials
// This test requires:
// - AWS_ACCESS_KEY_ID environment variable
// - AWS_SECRET_ACCESS_KEY environment variable
// - S3_TEST_BUCKET environment variable
// Run with: go test -tags=integration ./pkg/services/objectstore/s3/...
func TestS3Integration_WithEnvCredentials(t *testing.T) {
	bucket := os.Getenv("S3_TEST_BUCKET")
	if bucket == "" {
		t.Skip("S3_TEST_BUCKET environment variable not set, skipping integration test")
	}

	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		t.Skip("AWS credentials not set, skipping integration test")
	}

	// Set up logger for factory
	logger := zaptest.NewLogger(t)
	oldLogger := zap.L()
	defer zap.ReplaceGlobals(oldLogger)
	zap.ReplaceGlobals(logger)

	f := &Factory{}
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_S3,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreS3Config: config.ObjectStoreS3Config{
				Bucket: bucket,
				Region: "us-east-1",
				// No explicit credentials - should use env vars via credential chain
			},
		},
	}

	err := f.Init(context.Background(), cfg)
	require.NoError(t, err)
	assert.NotNil(t, f.objectstore)

	// Test that we can actually put an object
	ctx := context.Background()
	data := []byte("test data for integration test")
	digest := "test-digest-env-creds"
	contentType := "text/plain"

	metadata, err := f.objectstore.Put(ctx, digest, contentType, data)
	require.NoError(t, err)
	assert.NotNil(t, metadata)
	assert.Equal(t, bucket, metadata["BUCKET"])
	assert.Equal(t, digest, metadata["DIGEST"])
}

// TestS3Integration_WithStaticCredentials tests S3 with explicitly configured credentials
// This test requires:
// - S3_TEST_BUCKET environment variable
// - S3_TEST_ACCESS_KEY environment variable
// - S3_TEST_SECRET_KEY environment variable
// Run with: go test -tags=integration ./pkg/services/objectstore/s3/...
func TestS3Integration_WithStaticCredentials(t *testing.T) {
	bucket := os.Getenv("S3_TEST_BUCKET")
	if bucket == "" {
		t.Skip("S3_TEST_BUCKET environment variable not set, skipping integration test")
	}

	accessKey := os.Getenv("S3_TEST_ACCESS_KEY")
	secretKey := os.Getenv("S3_TEST_SECRET_KEY")
	if accessKey == "" || secretKey == "" {
		t.Skip("S3 test credentials not set, skipping integration test")
	}

	// Set up logger for factory
	logger := zaptest.NewLogger(t)
	oldLogger := zap.L()
	defer zap.ReplaceGlobals(oldLogger)
	zap.ReplaceGlobals(logger)

	f := &Factory{}
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_S3,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreS3Config: config.ObjectStoreS3Config{
				Bucket: bucket,
				Region: "us-east-1",
				AccessKey: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: accessKey,
				},
				SecretKey: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: secretKey,
				},
			},
		},
	}

	err := f.Init(context.Background(), cfg)
	require.NoError(t, err)
	assert.NotNil(t, f.objectstore)

	// Test that we can actually put an object
	ctx := context.Background()
	data := []byte("test data for integration test with static creds")
	digest := "test-digest-static-creds"
	contentType := "text/plain"

	metadata, err := f.objectstore.Put(ctx, digest, contentType, data)
	require.NoError(t, err)
	assert.NotNil(t, metadata)
	assert.Equal(t, bucket, metadata["BUCKET"])
	assert.Equal(t, digest, metadata["DIGEST"])
}

// TestS3Integration_WithIAM tests S3 with IAM credentials (IRSA or IMDS)
// This test requires running in an environment with IAM credentials available:
// - EKS cluster with IRSA configured
// - EC2 instance with instance profile
// - S3_TEST_BUCKET environment variable
// Run with: go test -tags=integration ./pkg/services/objectstore/s3/...
func TestS3Integration_WithIAM(t *testing.T) {
	bucket := os.Getenv("S3_TEST_BUCKET")
	if bucket == "" {
		t.Skip("S3_TEST_BUCKET environment variable not set, skipping integration test")
	}

	// Clear any existing AWS env credentials to force IAM
	origAccessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	origSecretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	os.Unsetenv("AWS_ACCESS_KEY_ID")
	os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	defer func() {
		if origAccessKey != "" {
			os.Setenv("AWS_ACCESS_KEY_ID", origAccessKey)
		}
		if origSecretKey != "" {
			os.Setenv("AWS_SECRET_ACCESS_KEY", origSecretKey)
		}
	}()

	// Set up logger for factory
	logger := zaptest.NewLogger(t)
	oldLogger := zap.L()
	defer zap.ReplaceGlobals(oldLogger)
	zap.ReplaceGlobals(logger)

	f := &Factory{}
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_S3,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreS3Config: config.ObjectStoreS3Config{
				Bucket: bucket,
				Region: "us-east-1",
				// No credentials - should use IAM
			},
		},
	}

	err := f.Init(context.Background(), cfg)

	// This will only succeed if running in an IAM-enabled environment
	if err != nil {
		t.Logf("IAM credentials not available (expected in non-IAM environments): %v", err)
		t.Skip("Skipping IAM test - not running in IAM environment")
		return
	}

	require.NoError(t, err)
	assert.NotNil(t, f.objectstore)

	// Test that we can actually put an object
	ctx := context.Background()
	data := []byte("test data for integration test with IAM")
	digest := "test-digest-iam"
	contentType := "text/plain"

	metadata, err := f.objectstore.Put(ctx, digest, contentType, data)
	require.NoError(t, err)
	assert.NotNil(t, metadata)
	assert.Equal(t, bucket, metadata["BUCKET"])
	assert.Equal(t, digest, metadata["DIGEST"])
}
