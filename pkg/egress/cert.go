package egress

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/synq"
	"go.uber.org/zap"
)

type Cert struct {
	// key
	key *rsa.PrivateKey

	// template
	template *x509.Certificate

	// cert
	cert *tls.Certificate

	// root cert
	rootCert *Cert
}

func NewSANCert(rootCert *Cert) (*Cert, error) {
	// Create leaf certificate
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// create the template
	template := x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			Organization: []string{"Qpoint Forwarder"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0), // Valid for 1 year
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		DNSNames:              []string{},
	}

	return &Cert{
		template: &template,
		rootCert: rootCert,
		cert: &tls.Certificate{
			Certificate: [][]byte{nil, rootCert.cert.Certificate[0]},
			PrivateKey:  key,
		},
		key: key,
	}, nil
}

func (c *Cert) AddDomain(domain string) error {
	// update the template
	c.template.DNSNames = append(c.template.DNSNames, domain)

	// create the cert
	bytes, err := x509.CreateCertificate(
		rand.Reader,
		c.template,
		c.rootCert.template,
		&c.key.PublicKey,
		c.rootCert.key,
	)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// update the cert
	c.cert.Certificate[0] = bytes

	return nil
}

type CertStore struct {
	// logger
	logger *zap.Logger

	// root certificate
	rootCert *Cert

	// current SAN cert
	currentSanCert *Cert

	// current SAN cert counter
	currentSanCertCounter int

	// SAN cert max size
	sanCertMaxSize int

	// mapping of domain to cert
	domainToCert *synq.Map[string, *Cert]

	// mutex
	mu sync.Mutex
}

func NewCertStore(sanCertMaxSize int, logger *zap.Logger) *CertStore {
	return &CertStore{
		logger:         logger,
		domainToCert:   synq.NewMap[string, *Cert](),
		sanCertMaxSize: sanCertMaxSize,
	}
}

func (c *CertStore) Init() error {
	// generate a private key (root CA key)
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create root CA template
	rootTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Forwarder Self-Signed Root CA"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0), // Valid for 10 years
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// create the root certificate
	rootDerBytes, err := x509.CreateCertificate(rand.Reader, &rootTemplate, &rootTemplate, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("failed to create root certificate: %w", err)
	}

	// create the root cert
	c.rootCert = &Cert{
		template: &rootTemplate,
		cert: &tls.Certificate{
			Certificate: [][]byte{rootDerBytes},
			PrivateKey:  priv,
		},
		key: priv,
	}

	return nil
}

func (c *CertStore) GetRootCertBytes() ([]byte, error) {
	// ensure we have a root cert
	if c.rootCert == nil {
		return nil, errors.New("root cert not initialized")
	}

	// get the root cert
	return c.rootCert.cert.Certificate[0], nil
}

func (c *CertStore) GetCert(domain string) (*tls.Certificate, error) {
	// lookup the domain in the map
	if cert, ok := c.domainToCert.Load(domain); ok {
		return cert.cert, nil
	}

	// lock
	c.mu.Lock()
	defer c.mu.Unlock()

	// we need to find or create a cert
	var cert *Cert

	// if we're creating a new cert, generate it
	if c.currentSanCert == nil || c.currentSanCertCounter == c.sanCertMaxSize {
		c.logger.Debug("creating new SAN cert", zap.Int("current_cert_counter", c.currentSanCertCounter), zap.Int("san_cert_max_size", c.sanCertMaxSize))

		var err error
		cert, err = NewSANCert(c.rootCert)
		if err != nil {
			return nil, fmt.Errorf("failed to create new SAN cert: %w", err)
		}

		// set the new current cert
		c.currentSanCert = cert

		// reset the cert counter
		c.currentSanCertCounter = 0
	} else {
		c.logger.Debug("using existing SAN cert", zap.Int("current_cert_counter", c.currentSanCertCounter), zap.Int("san_cert_max_size", c.sanCertMaxSize))
		// get the cert
		cert = c.currentSanCert
	}

	// add the domain to the cert
	if err := cert.AddDomain(domain); err != nil {
		return nil, fmt.Errorf("failed to add domain to cert: %w", err)
	} else {
		c.logger.Debug("added domain to cert", zap.String("domain", domain))
	}

	// update the counter
	c.currentSanCertCounter++

	// update the map
	c.domainToCert.Store(domain, cert)

	// return the tls cert
	return cert.cert, nil
}
