package s3

import (
	"context"
	"errors"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	minio "github.com/qpoint-io/qtap/pkg/services/objectstore/s3/minio"
	"go.uber.org/zap"
)

// ensure we implement the ObjectStore interface
var _ objectstore.ObjectStore = (*ObjectStore)(nil)

const (
	TypeS3ObjectStore services.ServiceType = "s3"
)

type S3ObjectStorer interface {
	Put(ctx context.Context, digest string, contentType string, data []byte) (map[string]string, error)
}

type Factory struct {
	objectstore.BaseObjectStore

	logger             *zap.Logger
	objectstore        S3ObjectStorer
	accessURL          string
	eventStoreSelector config.EventStoreSelector
}

func (f *Factory) Init(ctx context.Context, cfg any) error {
	f.logger = zap.L().With(zap.String("service_factory", f.FactoryType().String()))

	c, ok := cfg.(config.ServiceObjectStore)
	if !ok {
		return fmt.Errorf("invalid config type: %T wanted config.ServiceObjectStore", cfg)
	}
	f.eventStoreSelector = c.EventStore

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

	// Check if credentials are provided
	accessKey := c.ObjectStoreS3Config.AccessKey.String()
	secretKey := c.ObjectStoreS3Config.SecretKey.String()

	// Both credentials must be provided together, or both omitted
	hasAccessKey := accessKey != ""
	hasSecretKey := secretKey != ""

	if hasAccessKey != hasSecretKey {
		// One is provided but not the other - this is an error
		if hasAccessKey {
			return errors.New("secret_key is required when access_key is provided")
		}
		return errors.New("access_key is required when secret_key is provided")
	}

	// If AccessKey has type ENV but the env var is empty, that's a config error
	if c.ObjectStoreS3Config.AccessKey.Type == config.ValueSourceType_ENV &&
		c.ObjectStoreS3Config.AccessKey.Value != "" &&
		accessKey == "" {
		return fmt.Errorf("s3 access key env var (%s) is empty or not set", c.ObjectStoreS3Config.AccessKey.Value)
	}

	// If SecretKey has type ENV but the env var is empty, that's a config error
	if c.ObjectStoreS3Config.SecretKey.Type == config.ValueSourceType_ENV &&
		c.ObjectStoreS3Config.SecretKey.Value != "" &&
		secretKey == "" {
		return fmt.Errorf("s3 secret key env var (%s) is empty or not set", c.ObjectStoreS3Config.SecretKey.Value)
	}

	// Log the credential mode being used
	if hasAccessKey && hasSecretKey {
		f.logger.Debug("using explicit S3 credentials from config")
	} else {
		f.logger.Debug("no explicit S3 credentials configured, will use IAM credential chain")
	}

	s, err := minio.NewS3ObjectStore(
		f.logger,
		c.ObjectStoreS3Config.Endpoint,
		c.ObjectStoreS3Config.Bucket,
		c.ObjectStoreS3Config.Region,
		accessKey,
		secretKey,
		c.ObjectStoreS3Config.Insecure)
	if err != nil {
		return fmt.Errorf("failed to create s3 object store: %w", err)
	}

	f.objectstore = s

	return nil
}

func (f *Factory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	eventStore, err := services.GetService[eventstore.EventStore](ctx, svcRegistry, eventstore.TypeEventStore, f.eventStoreSelector.ID)
	if err != nil {
		return nil, err
	}

	return &ObjectStore{
		put: func(logger *zap.Logger, artifact *eventstore.Artifact) {
			// We create a new context that is not cancelled when the parent
			// is because the parent context is cancelled when the connection
			// is closed but the object may not have been uploaded yet.
			ctx := context.WithoutCancel(ctx)
			logger = logger.With(f.LogFields(artifact)...)
			m, err := f.objectstore.Put(ctx, artifact.Digest(), artifact.ContentType, artifact.Data)
			if err != nil {
				logger.Error("failed to put artifact", zap.Error(err))
				return
			}

			if eventStore != nil {
				url := templateURL(f.accessURL, m)
				record := artifact.Record(url)
				eventStore.Save(ctx, record)
				logger.Debug("artifact stored", zap.String("url", url))
			} else {
				logger.Debug("artifact stored")
			}
		},
		eventStore: eventStore,
	}, nil
}

func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType(fmt.Sprintf("%s.%s", objectstore.TypeObjectStore, TypeS3ObjectStore))
}
