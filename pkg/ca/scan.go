package ca

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/qpoint-io/qtap/pkg/process"
)

var SslCertEnvVars = []string{
	"SSL_CERT_FILE",              // single CA certificate file
	"NODE_EXTRA_CA_CERTS",        // CA bundle file for Node.js
	"CURL_CA_BUNDLE",             // CA certificate bundle file for cURL
	"GIT_SSL_CAINFO",             // file with CA certificates for Git operations
	"REQUESTS_CA_BUNDLE",         // CA bundle file for Python's requests library
	"WEBSOCKET_CLIENT_CA_BUNDLE", // CA bundle file for Python's websocket-client library
	"HTTPLIB2_CA_CERTS",          // CA certificates file for Python's httplib2 library
	"AWS_CA_BUNDLE",              // CA bundle file for AWS CLI and SDKs
	"LDAPTLS_CACERT",             // CA certificate file for LDAP TLS connections
	"ERL_SSL_CA_FILE",            // file with PEM-encoded CA certificates for Erlang/Elixir
	"PYTHON_CERTIFI_BUNDLE",      // custom CA bundle for Python's certifi library
	"OPENSSL_CERT_FILE",          // file with CA certificates for OpenSSL
	"ELIXIR_SSL_CA_FILE",         // Contains path to CA certificates file for some Elixir-specific libraries
}

var KeystoreEnvVars = []string{
	"TRUST_STORE",
	"TRUST_STORE_PASSWORD",
}

func (c *Container) scanCustomCerts(p *process.Process) (bool, error) {
	// were any language-specific certs found?
	foundCerts := false

	for _, envVar := range SslCertEnvVars {
		if certFile, ok := p.Env[envVar]; ok {
			found, err := c.scanCertFile(certFile, PEM, p)
			if err != nil {
				return false, err
			}
			if found {
				foundCerts = true
			}
		}
	}

	// scan the java keystore
	if trustStore, ok := p.Env["TRUST_STORE"]; ok {
		trustStorePassword := p.Env["TRUST_STORE_PASSWORD"]

		// if we don't have a trust store password, set the default
		if trustStorePassword == "" {
			trustStorePassword = "changeit"
		}

		found, err := c.scanCertFile(trustStore, Keystore, p, WithKeystorePassword(trustStorePassword))
		if err != nil {
			return false, err
		}
		foundCerts = foundCerts || found
	}

	return foundCerts, nil
}

func (c *Container) scanCertFile(certFile string, fileType FileType, p *process.Process, opts ...CertOption) (bool, error) {
	// look for the cert file in the process's root namespace
	sourceFile := filepath.Join("/proc", strconv.Itoa(p.Pid), "root", certFile)

	// resolve any symlinks
	locations, err := resolveLinks(sourceFile, filepath.Join("/proc", strconv.Itoa(p.Pid), "root"))
	if err != nil {
		return false, fmt.Errorf("failed to resolve links for %s: %w", sourceFile, err)
	}

	// if we're not using ebpf, we only want the first location
	if c.strategy != InjectStrategyEbpf && len(locations) > 1 {
		locations = locations[:1]
	}

	// if no locations were found, use the source file (doesn't matter if it doesn't exist)
	if len(locations) == 0 {
		locations = []string{sourceFile}
	}

	// setup each hard location
	for _, location := range locations {
		// strip the process root namespace
		location = strings.TrimPrefix(location, fmt.Sprintf("/proc/%d/root", p.Pid))

		// see if the cert is already in the container
		cert, exists := c.certs.Load(location)

		// create the cert if it doesn't exist
		if !exists {
			cert = NewCert(location, c.pidOne, c.caBytes, c.strategy, fileType, c.logger, c.certObjs, c.tapObjs, opts...)
			c.certs.Store(location, cert)
		}

		if err := cert.Inject(p.Pid, p.RootID); err != nil {
			return false, fmt.Errorf("failed to inject cert %s: %w", location, err)
		}

		// notify observers
		if err := c.certInjected(p.Pid, location); err != nil {
			return false, fmt.Errorf("notifying certificate injection for %s: %w", location, err)
		}
	}

	return true, nil
}

func fileExists(filename string) bool {
	_, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return err == nil
}

func resolveLinks(path string, prefix string) ([]string, error) {
	// check if the path is a symlink
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// file doesn't exist, return an empty list
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to get file info for %s: %w", path, err)
	}

	// if it's not a symlink, return the original path
	if info.Mode()&os.ModeSymlink == 0 {
		return []string{path}, nil
	}

	// resolve the symlink
	resolvedPath, err := os.Readlink(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve symlink %s: %w", path, err)
	}

	// if resolved path is not absolute, make it absolute
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(filepath.Dir(path), resolvedPath)
	}

	if prefix != "" && !strings.HasPrefix(resolvedPath, prefix) {
		resolvedPath = filepath.Join(prefix, resolvedPath)
	}

	// resolve symlink again in case it's circular
	resolved, err := resolveLinks(resolvedPath, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve links for %s: %w", path, err)
	}

	return append(resolved, path), nil
}
