package minio

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const credsRespStsImpl = `<AssumeRoleWithWebIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
<AssumeRoleWithWebIdentityResult>
  <SubjectFromWebIdentityToken>amzn1.account.AF6RHO7KZU5XRVQJGXK6HB56KR2A</SubjectFromWebIdentityToken>
  <Audience>client.5498841531868486423.1548@apps.example.com</Audience>
  <AssumedRoleUser>
	<Arn>arn:aws:sts::123456789012:assumed-role/FederatedWebIdentityRole/app1</Arn>
	<AssumedRoleId>AROACLKWSDQRAOEXAMPLE:app1</AssumedRoleId>
  </AssumedRoleUser>
  <Credentials>
	<SessionToken>token</SessionToken>
	<SecretAccessKey>secret</SecretAccessKey>
	<Expiration>%s</Expiration>
	<AccessKeyId>accessKey</AccessKeyId>
  </Credentials>
  <Provider>www.amazon.com</Provider>
</AssumeRoleWithWebIdentityResult>
<ResponseMetadata>
  <RequestId>ad4156e9-bce1-11e2-82e6-6b6efEXAMPLE</RequestId>
</ResponseMetadata>
</AssumeRoleWithWebIdentityResponse>`

func TestNewS3ObjectStore_WithStaticCredentials(t *testing.T) {
	logger := zaptest.NewLogger(t)

	store, err := NewS3ObjectStore(
		logger,
		"s3.amazonaws.com",
		"test-bucket",
		"us-east-1",
		"test-access-key",
		"test-secret-key",
		false,
	)

	// We expect this to succeed in creating the client
	// (it won't connect, but the client should be constructed)
	require.NoError(t, err)
	assert.NotNil(t, store)
	assert.NotNil(t, store.client)
	assert.Equal(t, "test-bucket", store.bucket)
}

func TestNewS3ObjectStore_WithCredentialChain(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Set up environment variables for the credential chain
	t.Setenv("AWS_ACCESS_KEY_ID", "test-aws-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-aws-secret")

	store, err := NewS3ObjectStore(
		logger,
		"s3.amazonaws.com",
		"test-bucket",
		"us-east-1",
		"", // Empty access key
		"", // Empty secret key
		false,
	)

	// Should succeed because AWS env vars are set
	require.NoError(t, err)
	assert.NotNil(t, store)
	assert.NotNil(t, store.client)
}

func TestNewS3ObjectStore_WithWebIdentity(t *testing.T) {
	logger := zaptest.NewLogger(t)

	expire := time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen for local STS test server: %v", err)
	}

	sts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		fmt.Fprintf(w, credsRespStsImpl, expire)
	}))
	sts.Listener = listener
	sts.Start()
	defer sts.Close()

	tokenFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("test-token"), 0o600))

	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", tokenFile)
	t.Setenv("AWS_ROLE_ARN", "arn:aws:sts::123456789012:assumed-role/FederatedWebIdentityRole/app1")
	t.Setenv("AWS_STS_ENDPOINT", sts.URL)
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	store, err := NewS3ObjectStore(
		logger,
		"s3.amazonaws.com",
		"test-bucket",
		"us-east-1",
		"", // Empty access key
		"", // Empty secret key
		false,
	)

	require.NoError(t, err)
	assert.NotNil(t, store)
	assert.NotNil(t, store.client)
}

func TestNewS3ObjectStore_WithCredentialChain_NoCredentials(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Ensure no environment credentials are available
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("MINIO_ACCESS_KEY", "")
	t.Setenv("MINIO_SECRET_KEY", "")

	store, err := NewS3ObjectStore(
		logger,
		"s3.amazonaws.com",
		"test-bucket",
		"us-east-1",
		"", // Empty access key
		"", // Empty secret key
		false,
	)

	// This test might succeed or fail depending on the environment:
	// - In CI/local dev: Should fail because no credentials are available
	// - In EC2/EKS: Might succeed because IAM credentials (IMDS/IRSA) are available
	// We accept both outcomes as valid
	if err != nil {
		// Expected in environments without IAM
		assert.Contains(t, err.Error(), "failed to retrieve credentials from chain")
		assert.Nil(t, store)
	} else {
		// IAM credentials were found (IMDS/IRSA available)
		assert.NotNil(t, store)
		t.Log("IAM credentials were available in this environment")
	}
}

func TestDetermineCredentialProvider_IAM(t *testing.T) {
	credValue := credentials.Value{
		AccessKeyID:     "ASIATESTACCESSKEY",
		SecretAccessKey: "test-secret",
		SessionToken:    "test-session-token",
		SignerType:      credentials.SignatureV4,
	}

	provider := determineCredentialProvider(credValue)
	assert.Equal(t, "IAM (IRSA/IMDS)", provider)
}

func TestDetermineCredentialProvider_WebIdentity(t *testing.T) {
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "/tmp/token")

	credValue := credentials.Value{
		AccessKeyID:     "ASIATESTACCESSKEY",
		SecretAccessKey: "test-secret",
		SessionToken:    "test-session-token",
		SignerType:      credentials.SignatureV4,
	}

	provider := determineCredentialProvider(credValue)
	assert.Equal(t, "WebIdentity (IRSA)", provider)
}

func TestDetermineCredentialProvider_EnvAWS(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key")

	credValue := credentials.Value{
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		SessionToken:    "", // No session token
		SignerType:      credentials.SignatureV4,
	}

	provider := determineCredentialProvider(credValue)
	assert.Equal(t, "EnvAWS", provider)
}

func TestDetermineCredentialProvider_EnvMinio(t *testing.T) {
	// Clear AWS env vars to ensure we detect Minio
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("MINIO_ACCESS_KEY", "test-key")

	credValue := credentials.Value{
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		SessionToken:    "", // No session token
		SignerType:      credentials.SignatureV4,
	}

	provider := determineCredentialProvider(credValue)
	assert.Equal(t, "EnvMinio", provider)
}

func TestDetermineCredentialProvider_Unknown(t *testing.T) {
	// Clear all env vars
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("MINIO_ACCESS_KEY", "")

	credValue := credentials.Value{
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		SessionToken:    "", // No session token
		SignerType:      credentials.SignatureV4,
	}

	provider := determineCredentialProvider(credValue)
	assert.Equal(t, "unknown", provider)
}
