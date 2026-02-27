package e2e

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// generateSelfSignedCert creates a self-signed ECDSA certificate with the given
// IP as a SAN. This is used for gRPC TLS tests.
func generateSelfSignedCert(ip string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{Organization: []string{"qtap e2e test"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP(ip)},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}, nil
}

// NewGRPCPlainServer creates a plaintext gRPC echo server bound to the given IP
// with a random port. Returns the server, listener, and any error.
func NewGRPCPlainServer(machineIP string) (*grpc.Server, net.Listener, error) {
	lis, err := net.Listen("tcp", machineIP+":0")
	if err != nil {
		return nil, nil, err
	}

	server := NewGRPCEchoServer()
	return server, lis, nil
}

// NewGRPCTLSServer creates a TLS gRPC echo server bound to the given IP
// with a random port and a self-signed certificate. Returns the server,
// listener, and any error.
func NewGRPCTLSServer(machineIP string) (*grpc.Server, net.Listener, error) {
	cert, err := generateSelfSignedCert(machineIP)
	if err != nil {
		return nil, nil, err
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
	creds := credentials.NewTLS(tlsConfig)

	lis, err := net.Listen("tcp", machineIP+":0")
	if err != nil {
		return nil, nil, err
	}

	server := NewGRPCEchoServer(grpc.Creds(creds))
	return server, lis, nil
}
