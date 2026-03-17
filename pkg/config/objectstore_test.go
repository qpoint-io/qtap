package config

import (
	"testing"

	"os"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestObjectStoreUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     ServiceObjectStore
		wantErr  bool
	}{
		{
			name:     "console objectstore",
			filename: "testdata/objectstore_console.yaml",
			want: ServiceObjectStore{
				Type: ObjectStoreType_CONSOLE,
				ID:   "console-store",
			},
			wantErr: false,
		},
		{
			name:     "qpoint objectstore",
			filename: "testdata/objectstore_qpoint.yaml",
			want: ServiceObjectStore{
				Type: ObjectStoreType_QPOINT,
				ID:   "qpoint-store",
				ObjectStoreConfig: ObjectStoreConfig{
					ObjectStoreQPointWarehouseConfig: ObjectStoreQPointWarehouseConfig{
						URL: "https://warehouse.example.com",
						Token: ValueSource{
							Value: "qpoint-token",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:     "s3 objectstore",
			filename: "testdata/objectstore_s3.yaml",
			want: ServiceObjectStore{
				Type: ObjectStoreType_S3,
				ID:   "s3-store",
				ObjectStoreConfig: ObjectStoreConfig{
					ObjectStoreS3Config: ObjectStoreS3Config{
						Endpoint:  "custom.s3.endpoint.com",
						Bucket:    "my-test-bucket",
						Region:    "us-west-2",
						AccessURL: "https://custom.cdn.example.com",
						Insecure:  false,
						AccessKey: ValueSource{
							Value: "access-key-123",
						},
						SecretKey: ValueSource{
							Value: "secret-key-456",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:     "otel objectstore",
			filename: "testdata/objectstore_otel.yaml",
			want: ServiceObjectStore{
				Type: ObjectStoreType_OTEL,
				ID:   "otel-clickhouse",
				ObjectStoreConfig: ObjectStoreConfig{
					ObjectStoreOTelConfig: ObjectStoreOTelConfig{
						OTelEndpoint: "localhost:4317",
						Protocol:     "grpc",
						ServiceName:  "qtap",
						Environment:  "production",
						Headers: map[string]ValueSource{
							"api-key": {
								Type:  "text",
								Value: "test-key",
							},
						},
						TLS: ObjectStoreOTelTLS{
							Enabled:            false,
							InsecureSkipVerify: false,
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:     "disabled objectstore",
			filename: "testdata/objectstore_disabled.yaml",
			want: ServiceObjectStore{
				Type: ObjectStoreType_DISABLED,
				ID:   "disabled-store",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.filename)
			if err != nil {
				t.Fatalf("failed to read test file: %v", err)
			}

			var got ServiceObjectStore
			err = yaml.Unmarshal(data, &got)
			if (err != nil) != tt.wantErr {
				t.Errorf("yaml.Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			assert.Equal(t, tt.want, got)
		})
	}
}
