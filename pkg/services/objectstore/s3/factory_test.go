package s3

import (
	"testing"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestFactory_Init_WithStaticCredentials(t *testing.T) {
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
				Bucket: "test-bucket",
				Region: "us-east-1",
				AccessKey: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: "test-access-key",
				},
				SecretKey: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: "test-secret-key",
				},
			},
		},
	}

	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.NotNil(t, f.objectstore)
}

func TestFactory_Init_WithIAMCredentials(t *testing.T) {
	// Set up logger for factory
	logger := zaptest.NewLogger(t)
	oldLogger := zap.L()
	defer zap.ReplaceGlobals(oldLogger)
	zap.ReplaceGlobals(logger)

	// Set up environment variables to simulate IAM credentials available
	t.Setenv("AWS_ACCESS_KEY_ID", "test-aws-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-aws-secret")

	f := &Factory{}
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_S3,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreS3Config: config.ObjectStoreS3Config{
				Bucket: "test-bucket",
				Region: "us-east-1",
				// AccessKey and SecretKey omitted - should use IAM credential chain
			},
		},
	}

	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.NotNil(t, f.objectstore)
}

func TestFactory_Init_PartialCredentials_OnlyAccessKey(t *testing.T) {
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
				Bucket: "test-bucket",
				Region: "us-east-1",
				AccessKey: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: "test-access-key",
				},
				// SecretKey omitted
			},
		},
	}

	err := f.Init(t.Context(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret_key is required when access_key is provided")
}

func TestFactory_Init_PartialCredentials_OnlySecretKey(t *testing.T) {
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
				Bucket: "test-bucket",
				Region: "us-east-1",
				SecretKey: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: "test-secret-key",
				},
				// AccessKey omitted
			},
		},
	}

	err := f.Init(t.Context(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access_key is required when secret_key is provided")
}

func TestFactory_Init_EmptyEnvVar(t *testing.T) {
	// Set up logger for factory
	logger := zaptest.NewLogger(t)
	oldLogger := zap.L()
	defer zap.ReplaceGlobals(oldLogger)
	zap.ReplaceGlobals(logger)

	// Ensure the env vars don't exist
	t.Setenv("NONEXISTENT_VAR", "")
	t.Setenv("ANOTHER_NONEXISTENT_VAR", "")

	f := &Factory{}
	cfg := config.ServiceObjectStore{
		Type: config.ObjectStoreType_S3,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreS3Config: config.ObjectStoreS3Config{
				Bucket: "test-bucket",
				Region: "us-east-1",
				AccessKey: config.ValueSource{
					Type:  config.ValueSourceType_ENV,
					Value: "NONEXISTENT_VAR",
				},
				SecretKey: config.ValueSource{
					Type:  config.ValueSourceType_ENV,
					Value: "ANOTHER_NONEXISTENT_VAR",
				},
			},
		},
	}

	err := f.Init(t.Context(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "env var")
}

func TestFactory_Init_DefaultEndpoint(t *testing.T) {
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
				Bucket: "test-bucket",
				AccessKey: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: "test-access-key",
				},
				SecretKey: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: "test-secret-key",
				},
				// Endpoint omitted - should default to s3.amazonaws.com
			},
		},
	}

	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.NotNil(t, f.objectstore)
}

func TestFactory_Init_DefaultRegion(t *testing.T) {
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
				Bucket: "test-bucket",
				AccessKey: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: "test-access-key",
				},
				SecretKey: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: "test-secret-key",
				},
				// Region omitted - should default to us-east-1
			},
		},
	}

	err := f.Init(t.Context(), cfg)
	require.NoError(t, err)
	assert.NotNil(t, f.objectstore)
}

func TestFactory_Init_MissingBucket(t *testing.T) {
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
				// Bucket omitted - should error
				AccessKey: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: "test-access-key",
				},
				SecretKey: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: "test-secret-key",
				},
			},
		},
	}

	err := f.Init(t.Context(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bucket is required")
}
