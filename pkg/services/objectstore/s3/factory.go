package s3

import (
	"context"
	"errors"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	minio "github.com/qpoint-io/qtap/pkg/services/objectstore/s3/minio"
	"go.uber.org/zap"
)

const (
	TypeS3ObjectStore services.ServiceType = "s3"
)

type S3ObjectStorer interface {
	Put(ctx context.Context, digest string, contentType string, data []byte) (map[string]string, error)
}

type Factory struct {
	objectstore.BaseObjectStore

	logger      *zap.Logger
	objectstore S3ObjectStorer
	accessURL   string
}

func (f *Factory) Init(ctx context.Context, cfg any) error {
	f.logger = zap.L().With(zap.String("service_factory", f.FactoryType().String()))

	c, ok := cfg.(config.ServiceObjectStore)
	if !ok {
		return fmt.Errorf("invalid config type: %T wanted config.ServiceObjectStore", cfg)
	}

	if c.Type != config.ObjectStoreType_S3 {
		return fmt.Errorf("invalid object store type: %s", c.Type)
	}

	if c.ObjectStoreS3Config.Endpoint == "" {
		// default to amazon endpoint
		c.ObjectStoreS3Config.Endpoint = "s3.amazonaws.com"
	}
	if c.ObjectStoreS3Config.Region == "" {
		// default to us-east-1
		c.ObjectStoreS3Config.Region = "us-east-1"
	}
	if c.ObjectStoreS3Config.Bucket == "" {
		return errors.New("bucket is required")
	}
	if c.ObjectStoreS3Config.AccessURL != "" {
		f.accessURL = c.ObjectStoreS3Config.AccessURL
	}
	if c.ObjectStoreS3Config.AccessKey.String() == "" {
		if c.ObjectStoreS3Config.AccessKey.Type == config.ValueSourceType_ENV {
			return fmt.Errorf("s3 access key env var (%s) is empty or not set", c.ObjectStoreS3Config.AccessKey.Value)
		}
		return errors.New("access_key is required")
	}
	if c.ObjectStoreS3Config.SecretKey.String() == "" {
		if c.ObjectStoreS3Config.SecretKey.Type == config.ValueSourceType_ENV {
			return fmt.Errorf("s3 secret key env var (%s) is empty or not set", c.ObjectStoreS3Config.SecretKey.Value)
		}
		return errors.New("secret_key is required")
	}

	s, err := minio.NewS3ObjectStore(
		f.logger,
		c.ObjectStoreS3Config.Endpoint,
		c.ObjectStoreS3Config.Bucket,
		c.ObjectStoreS3Config.Region,
		c.ObjectStoreS3Config.AccessKey.String(),
		c.ObjectStoreS3Config.SecretKey.String(),
		c.ObjectStoreS3Config.Insecure)
	if err != nil {
		return fmt.Errorf("failed to create s3 object store: %w", err)
	}

	f.objectstore = s

	return nil
}

func (f *Factory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	return &ObjectStore{
		putFn:     f.objectstore.Put,
		accessURL: f.accessURL,
	}, nil
}

func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", objectstore.TypeObjectStore, TypeS3ObjectStore))
}
