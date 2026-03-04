package minio

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

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
	httpClient := client.NewHttpClient()
	var creds *credentials.Credentials

	if accessKey != "" && secretKey != "" {
		// Explicit credentials provided - use them directly
		logger.Debug("using static S3 credentials")
		creds = credentials.NewStaticV4(accessKey, secretKey, "")
	} else {
		// No explicit credentials - try IAM/IRSA, then env vars
		logger.Debug("using S3 credential chain (WebIdentity -> IAM -> EnvAWS -> EnvMinio)")

		// Web identity (IRSA) provider sits ahead of IAM to avoid IMDS calls when tokens are present.
		providers := []credentials.Provider{}
		if tokenFile := os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE"); tokenFile != "" && os.Getenv("AWS_ROLE_ARN") != "" {
			stsEndpoint := stsEndpointForRegion(os.Getenv("AWS_REGION"))
			if envEndpoint := os.Getenv("AWS_STS_ENDPOINT"); envEndpoint != "" {
				stsEndpoint = envEndpoint
			}

			providers = append(providers, &credentials.STSWebIdentity{
				Client:      httpClient,
				STSEndpoint: stsEndpoint,
				GetWebIDTokenExpiry: func() (*credentials.WebIdentityToken, error) {
					token, err := os.ReadFile(tokenFile)
					if err != nil {
						return nil, err
					}
					return &credentials.WebIdentityToken{Token: string(token)}, nil
				},
				RoleARN: os.Getenv("AWS_ROLE_ARN"),
			})
		}

		providers = append(providers,
			&credentials.IAM{
				Client: httpClient,
			},
			&credentials.EnvAWS{},
			&credentials.EnvMinio{},
		)

		creds = credentials.NewChainCredentials(providers)
	}

	s3Client, err := minio.New(endpoint, &minio.Options{
		Creds:     creds,
		Secure:    !insecure,
		Region:    region,
		Transport: httpClient.Transport,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	// Log which credential provider succeeded (IAM mode only)
	if accessKey == "" && secretKey == "" {
		credValue, err := creds.Get() //nolint:staticcheck // Get() is used for validation
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve credentials from chain: %w", err)
		}
		providerName := determineCredentialProvider(credValue)
		logger.Info("successfully authenticated to S3",
			zap.String("provider", providerName),
			zap.String("endpoint", endpoint))
	}

	return &S3ObjectStore{
		logger:   logger,
		insecure: insecure,
		client:   s3Client,
		bucket:   bucket,
	}, nil
}

func stsEndpointForRegion(region string) string {
	if region == "" {
		return credentials.DefaultSTSRoleEndpoint
	}

	if strings.HasPrefix(region, "cn-") {
		return "https://sts." + region + ".amazonaws.com.cn"
	}

	return "https://sts." + region + ".amazonaws.com"
}

// determineCredentialProvider attempts to identify which provider succeeded
// based on the credential value characteristics
func determineCredentialProvider(credValue credentials.Value) string {
	if credValue.SessionToken != "" && os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE") != "" {
		return "WebIdentity (IRSA)"
	}

	// IAM credentials have a SessionToken
	if credValue.SessionToken != "" {
		return "IAM (IRSA/IMDS)"
	}

	// Check for AWS env var provider (AWS_ACCESS_KEY_ID)
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" {
		return "EnvAWS"
	}

	// Check for Minio env var provider (MINIO_ACCESS_KEY)
	if os.Getenv("MINIO_ACCESS_KEY") != "" {
		return "EnvMinio"
	}

	return "unknown"
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
