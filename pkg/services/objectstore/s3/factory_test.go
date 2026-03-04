package s3

import (
	"context"
	"strings"
	"testing"

	"github.com/qpoint-io/qtap/pkg/config"
)

func TestFactory_Init(t *testing.T) {
	tests := []struct {
		name    string
		cfg     any
		wantErr string
	}{
		{
			name:    "invalid config type",
			cfg:     "not a config",
			wantErr: "invalid config type",
		},
		{
			name: "wrong object store type",
			cfg: config.ServiceObjectStore{
				Type: config.ObjectStoreType_CONSOLE,
			},
			wantErr: "invalid object store type",
		},
		{
			name: "empty bucket",
			cfg: config.ServiceObjectStore{
				Type: config.ObjectStoreType_S3,
			},
			wantErr: "bucket is required",
		},
		{
			name: "empty access key text type",
			cfg: config.ServiceObjectStore{
				Type: config.ObjectStoreType_S3,
				ObjectStoreConfig: config.ObjectStoreConfig{
					ObjectStoreS3Config: config.ObjectStoreS3Config{
						Bucket:    "my-bucket",
						AccessKey: config.ValueSource{Type: config.ValueSourceType_TEXT, Value: ""},
					},
				},
			},
			wantErr: "access_key is required",
		},
		{
			name: "empty secret key text type",
			cfg: config.ServiceObjectStore{
				Type: config.ObjectStoreType_S3,
				ObjectStoreConfig: config.ObjectStoreConfig{
					ObjectStoreS3Config: config.ObjectStoreS3Config{
						Bucket:    "my-bucket",
						AccessKey: config.ValueSource{Type: config.ValueSourceType_TEXT, Value: "AKID"},
						SecretKey: config.ValueSource{Type: config.ValueSourceType_TEXT, Value: ""},
					},
				},
			},
			wantErr: "secret_key is required",
		},
		{
			name: "access key from empty env var",
			cfg: config.ServiceObjectStore{
				Type: config.ObjectStoreType_S3,
				ObjectStoreConfig: config.ObjectStoreConfig{
					ObjectStoreS3Config: config.ObjectStoreS3Config{
						Bucket:    "my-bucket",
						AccessKey: config.ValueSource{Type: config.ValueSourceType_ENV, Value: "S3_TEST_ACCESS_KEY_EMPTY"},
					},
				},
			},
			wantErr: "s3 access key env var (S3_TEST_ACCESS_KEY_EMPTY) is empty or not set",
		},
		{
			name: "secret key from empty env var",
			cfg: config.ServiceObjectStore{
				Type: config.ObjectStoreType_S3,
				ObjectStoreConfig: config.ObjectStoreConfig{
					ObjectStoreS3Config: config.ObjectStoreS3Config{
						Bucket:    "my-bucket",
						AccessKey: config.ValueSource{Type: config.ValueSourceType_TEXT, Value: "AKID"},
						SecretKey: config.ValueSource{Type: config.ValueSourceType_ENV, Value: "S3_TEST_SECRET_KEY_EMPTY"},
					},
				},
			},
			wantErr: "s3 secret key env var (S3_TEST_SECRET_KEY_EMPTY) is empty or not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("S3_TEST_ACCESS_KEY_EMPTY", "")
			t.Setenv("S3_TEST_SECRET_KEY_EMPTY", "")

			f := &Factory{}
			err := f.Init(context.Background(), tt.cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}
