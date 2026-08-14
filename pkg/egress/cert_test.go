package egress

import (
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewSANCert(t *testing.T) {
	// Create a root cert first
	store := NewCertStore(10, zap.NewNop())
	err := store.Init()
	require.NoError(t, err)

	cert, err := NewSANCert(store.rootCert)
	require.NoError(t, err)

	// Verify the cert structure
	assert.NotNil(t, cert.key)
	assert.NotNil(t, cert.template)
	assert.NotNil(t, cert.cert)
	assert.Equal(t, store.rootCert, cert.rootCert)
	assert.Equal(t, []string{}, cert.template.DNSNames)
}

func TestCert_AddDomain(t *testing.T) {
	// Setup
	store := NewCertStore(10, zap.NewNop())
	err := store.Init()
	require.NoError(t, err)

	cert, err := NewSANCert(store.rootCert)
	require.NoError(t, err)

	// Test adding a domain
	err = cert.AddDomain("example.com")
	require.NoError(t, err)

	assert.Contains(t, cert.template.DNSNames, "example.com")
	assert.NotNil(t, cert.cert.Certificate[0])
}

func TestCertStore_Init(t *testing.T) {
	store := NewCertStore(10, zap.NewNop())
	err := store.Init()
	require.NoError(t, err)

	assert.NotNil(t, store.rootCert)
	assert.NotNil(t, store.rootCert.key)
	assert.NotNil(t, store.rootCert.template)
	assert.True(t, store.rootCert.template.IsCA)
}

func TestCertStore_GetRootCertBytes(t *testing.T) {
	store := NewCertStore(10, zap.NewNop())

	// Test without initialization
	bytes, err := store.GetRootCertBytes()
	require.Error(t, err)
	assert.Nil(t, bytes)

	// Test with initialization
	err = store.Init()
	require.NoError(t, err)

	bytes, err = store.GetRootCertBytes()
	require.NoError(t, err)
	assert.NotNil(t, bytes)

	// Verify it's a valid certificate
	_, err = x509.ParseCertificate(bytes)
	require.NoError(t, err)
}

func TestCertStore_GetCert(t *testing.T) {
	store := NewCertStore(2, zap.NewNop()) // Small max size to test rotation
	err := store.Init()
	require.NoError(t, err)

	// Test getting cert for first domain
	cert1, err := store.GetCert("example1.com")
	require.NoError(t, err)
	assert.NotNil(t, cert1)

	// Test getting same domain returns same cert
	cert1Again, err := store.GetCert("example1.com")
	require.NoError(t, err)
	assert.Equal(t, cert1, cert1Again)

	// Test adding second domain to same SAN cert
	cert2, err := store.GetCert("example2.com")
	require.NoError(t, err)
	assert.NotNil(t, cert2)

	// Test rotation: third domain should create new cert
	cert3, err := store.GetCert("example3.com")
	require.NoError(t, err)
	assert.NotNil(t, cert3)
	assert.NotEqual(t, cert1, cert3)
}

func TestNewCertStore(t *testing.T) {
	logger := zap.NewNop()
	store := NewCertStore(10, logger)

	assert.Equal(t, 10, store.sanCertMaxSize)
	assert.Equal(t, logger, store.logger)
	assert.NotNil(t, store.domainToCert)
	assert.Nil(t, store.currentSanCert)
	assert.Equal(t, 0, store.currentSanCertCounter)
}
