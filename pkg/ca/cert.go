package ca

import (
	"bytes"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"math/rand/v2"

	"github.com/pavlo-v-chernykh/keystore-go/v4"
	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/synq"
	"go.uber.org/zap"
)

// type of certificate file
type FileType int

const (
	// plaintext certificate bundle file (e.g., PEM)
	PEM FileType = iota
	// Javakeystore file (e.g., JKS, PKCS12)
	Keystore
)

type Cert struct {
	// orignal cert location on the filesystem
	Location string

	// ca bytes to inject
	caBytes []byte

	// first pid in the container
	pidOne int

	// inject strategy
	strategy InjectStrategy

	// processes map
	processes *synq.Map[int, struct{}]

	// replacement location
	replacement string

	// file type
	fileType FileType

	// root id
	rootID uint64

	// installed status
	installed bool

	// did we create the file?
	createdFile bool

	// logger
	logger *zap.Logger

	// bpf objects
	certObjs *tap.CertsObjects
	tapObjs  *tap.TapObjects

	// java keystore password
	keystorePassword string

	// mutex
	mu sync.Mutex
}

type CertOption func(*Cert)

func WithKeystorePassword(password string) CertOption {
	return func(c *Cert) {
		c.keystorePassword = password
	}
}

func NewCert(
	location string,
	pidOne int,
	caBytes []byte,
	strategy InjectStrategy,
	fileType FileType,
	logger *zap.Logger,
	certObjs *tap.CertsObjects,
	captureObjs *tap.TapObjects,
	opts ...CertOption,
) *Cert {
	cert := &Cert{
		Location:  location,
		pidOne:    pidOne,
		caBytes:   caBytes,
		strategy:  strategy,
		fileType:  fileType,
		logger:    logger,
		certObjs:  certObjs,
		tapObjs:   captureObjs,
		processes: synq.NewMap[int, struct{}](),
	}

	// apply the optional configurations
	for _, opt := range opts {
		opt(cert)
	}

	return cert
}

func (m *Cert) Inject(pid int, rootID uint64) error {
	// set the process information
	m.rootID = rootID

	if m.strategy == InjectStrategyEbpf {
		if err := m.injectWithEbpf(pid); err != nil {
			m.logger.Error("failed to inject cert with ebpf", zap.Error(err))
			return fmt.Errorf("failed to inject cert with ebpf: %w", err)
		}
	}

	if m.strategy == InjectStrategyInline {
		if err := m.injectNative(pid); err != nil {
			m.logger.Error("failed to inject cert", zap.Error(err))
			return fmt.Errorf("failed to inject cert: %w", err)
		}
	}

	if m.strategy == InjectStrategyManual {
		if err := m.setWatched(pid, m.Location); err != nil {
			return fmt.Errorf("failed to set cert key in bpf map: %w", err)
		}
	}

	// add the process to the cert's processes map
	m.processes.Store(pid, struct{}{})

	return nil
}

func (m *Cert) injectWithEbpf(pid int) error {
	// generate the replacement location
	m.generateReplacementName()

	// determine the path to the custom cert file
	custom := m.replacement

	// determine the path to the ca source file
	original := m.Location

	customFullPath := filepath.Join("/proc", strconv.Itoa(m.pidOne), "root", custom)
	originalFullPath := filepath.Join("/proc", strconv.Itoa(m.pidOne), "root", original)
	prefix := filepath.Join("/proc", strconv.Itoa(m.pidOne), "root")

	// ensure the parent directory of custom exists
	parentDir := filepath.Dir(customFullPath)
	if err := ensureDir(parentDir); err != nil {
		return fmt.Errorf("failed to create parent directory at %s: %w", parentDir, err)
	}

	// resolve the symlinks in the original file path
	originalFullPath, err := resolveCertSource(originalFullPath, prefix)
	if err != nil {
		return fmt.Errorf("failed to resolve symlink: %w", err)
	}

	// install the cert
	if err := m.install(originalFullPath, customFullPath); err != nil {
		return fmt.Errorf("failed to install cert: %w", err)
	}

	// set the cert key in the bpf map
	if err := m.setInjected(pid, original, custom); err != nil {
		return fmt.Errorf("failed to set cert key in bpf map: %w", err)
	}

	// log the injection
	// m.logger.Debug("injected ca cert", zap.Int("pid", pid), zap.String("file", m.Location))

	return nil
}

func (m *Cert) injectNative(pid int) error {
	// determine the path to the cert file
	fullPath := filepath.Join("/proc", strconv.Itoa(m.pidOne), "root", m.Location)

	// ensure the parent directory of fullPath exists
	parentDir := filepath.Dir(fullPath)
	if err := ensureDir(parentDir); err != nil {
		return fmt.Errorf("failed to create parent directory at %s: %w", parentDir, err)
	}

	// install the cert
	if err := m.install("", fullPath); err != nil {
		return fmt.Errorf("failed to install cert: %w", err)
	}

	// set the cert key in the bpf map
	if err := m.setWatched(pid, m.Location); err != nil {
		return fmt.Errorf("failed to set cert key in bpf map: %w", err)
	}

	// log the injection
	// m.logger.Debug("injected ca cert", zap.Int("pid", pid), zap.String("file", m.Location))

	return nil
}

func (m *Cert) install(originalFullPath, customFullPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// return if we're already installed
	if m.installed {
		return nil
	}

	// create an empty file at custom if it doesn't exist
	if !fileExists(customFullPath) {
		if err := os.WriteFile(customFullPath, []byte{}, 0o644); err != nil {
			return fmt.Errorf("failed to create empty file at %s: %w", customFullPath, err)
		}
		m.createdFile = true
	}

	// copy the contents of the ca source file to the custom file, if it exists
	if originalFullPath != "" && fileExists(originalFullPath) {
		if err := copyFile(originalFullPath, customFullPath); err != nil {
			return fmt.Errorf("failed to copy file contents: %w", err)
		}
	}

	switch m.fileType {
	case PEM:
		if err := m.injectPEM(customFullPath); err != nil {
			return fmt.Errorf("failed to inject PEM file: %w", err)
		}
	case Keystore:
		if err := m.injectKeystore(customFullPath); err != nil {
			return fmt.Errorf("failed to inject Keystore file: %w", err)
		}
	}

	// set the installed status
	m.installed = true

	return nil
}

func (m *Cert) injectPEM(destination string) error {
	// open the destination file in append mode
	f, err := os.OpenFile(destination, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open destination file: %w", err)
	}
	defer f.Close()

	// encode and write the certificate bytes to the file
	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: m.caBytes}); err != nil {
		return fmt.Errorf("failed to encode and write certificate: %w", err)
	}

	return nil
}

// Injects the certificate into a Java truststore file.
//
// Verify with:
// keytool -list -alias "Qtap Injected CA" -cacerts -storepass changeit
// keytool -list -alias "Qtap Injected CA" -keystore /etc/ssl/certs/java/cacerts -storepass changeit
func (m *Cert) injectKeystore(destination string) error {
	// define the keystore file and password
	keystoreFile := destination
	password := []byte(m.keystorePassword)

	// create a new keystore
	ks := keystore.New()

	// read the existing keystore
	if !m.createdFile {
		var err error
		ks, err = readKeyStore(keystoreFile, password)
		if err != nil {
			return fmt.Errorf("failed to read keystore file %s: %w", keystoreFile, err)
		}
	}

	// create a trusted certificate entry
	tce := keystore.TrustedCertificateEntry{
		CreationTime: time.Now(),
		Certificate: keystore.Certificate{
			Type:    "X509",
			Content: m.caBytes,
		},
	}

	// add the new certificate to the keystore
	alias := "Qtap Injected CA"
	if err := ks.SetTrustedCertificateEntry(alias, tce); err != nil {
		return fmt.Errorf("failed to set trusted certificate entry: %w", err)
	}

	// save the updated keystore
	if err := writeKeyStore(ks, keystoreFile, password); err != nil {
		return fmt.Errorf("failed to write keystore file %s: %w", keystoreFile, err)
	}

	return nil
}

func (m *Cert) Remove(pid int) error {
	// ensure this process is in the cert's processes map
	if _, ok := m.processes.Load(pid); !ok {
		return nil
	}

	// remove the process from the cert's processes map
	m.processes.Delete(pid)

	// unset the cert key in the bpf map
	if m.strategy == InjectStrategyEbpf {
		if err := m.unsetInjected(pid, m.Location); err != nil {
			return fmt.Errorf("failed to unset cert key in bpf map: %w", err)
		}
	} else {
		if err := m.unsetWatched(pid, m.Location); err != nil {
			return fmt.Errorf("failed to unset cert key in bpf map: %w", err)
		}
	}

	// if we're not empty, we're done
	if !m.isEmpty() {
		return nil
	}

	// nothing to clean up if the cert wasn't installed
	if !m.installed {
		return nil
	}

	if m.strategy == InjectStrategyEbpf {
		if err := m.removeWithEbpf(); err != nil {
			return fmt.Errorf("failed to remove cert with ebpf: %w", err)
		}
	}

	if m.strategy == InjectStrategyInline {
		if err := m.removeNative(); err != nil {
			return fmt.Errorf("failed to remove cert: %w", err)
		}
	}

	return nil
}

func (m *Cert) removeWithEbpf() error {
	// determine the path to the custom cert file
	customFullPath := filepath.Join("/proc", strconv.Itoa(m.pidOne), "root", m.replacement)

	// nothing to do if the file doesn't exist
	if !fileExists(customFullPath) {
		return nil
	}

	// remove the custom file
	if err := os.Remove(customFullPath); err != nil {
		return fmt.Errorf("failed to remove mount source file at %s: %w", customFullPath, err)
	}

	return nil
}

func (m *Cert) removeNative() error {
	// determine the path to the cert file
	fullPath := filepath.Join("/proc", strconv.Itoa(m.pidOne), "root", m.Location)

	// nothing to do if the file doesn't exist
	if !fileExists(fullPath) {
		return nil
	}

	// remove the cert file if we created it
	if m.createdFile {
		if err := os.Remove(fullPath); err != nil {
			return fmt.Errorf("failed to remove mount source file at %s: %w", fullPath, err)
		}
	}

	// if we didn't create the file, we need to remove the cert from the file
	if !m.createdFile {
		switch m.fileType {
		case PEM:
			if err := m.removePEM(fullPath); err != nil {
				return fmt.Errorf("failed to remove PEM file: %w", err)
			}
		case Keystore:
			if err := m.removeKeystore(fullPath); err != nil {
				return fmt.Errorf("failed to remove Keystore file: %w", err)
			}
		}
	}

	return nil
}

func (m *Cert) removePEM(destination string) error {
	// Read the entire file
	content, err := os.ReadFile(destination)
	if err != nil {
		return fmt.Errorf("failed to read destination file: %w", err)
	}

	// Encode our injected certificate for comparison
	injectedCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: m.caBytes})

	// Find and remove only our injected certificate
	newContent := bytes.ReplaceAll(content, injectedCert, []byte{})

	// Write the updated content back to the file
	if err := os.WriteFile(destination, newContent, 0644); err != nil {
		return fmt.Errorf("failed to write updated content to file: %w", err)
	}

	return nil
}

func (m *Cert) removeKeystore(destination string) error {
	// define the keystore file and password
	keystoreFile := destination
	password := []byte(m.keystorePassword)

	// read the existing keystore
	ks, err := readKeyStore(keystoreFile, password)
	if err != nil {
		return fmt.Errorf("failed to read keystore file %s: %w", keystoreFile, err)
	}

	// remove the certificate
	ks.DeleteEntry("Qtap Injected CA")

	// save the updated keystore
	if err := writeKeyStore(ks, keystoreFile, password); err != nil {
		return fmt.Errorf("failed to write keystore file %s: %w", keystoreFile, err)
	}

	return nil
}

func (m *Cert) RemoveAll() error {
	// iterate over the processes and remove them
	m.processes.Iter(func(pid int, _ struct{}) bool {
		if err := m.Remove(pid); err != nil {
			m.logger.Error("failed to remove process from cert", zap.Error(err))
			return false
		}
		return true
	})

	return nil
}

func (c *Cert) IsEmpty() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.isEmpty()
}

func (c *Cert) isEmpty() bool {
	return c.processes.Len() == 0
}

type CertKey struct {
	PID      uint32
	FilePath [256]byte
}

func (m *Cert) setInjected(pid int, filePath string, replacement string) error {
	// create a new cert key
	key := CertKey{}

	// replacement value
	value := [256]byte{}
	copy(value[:255], replacement)

	// copy the root id and file path to the key
	key.PID = uint32(pid)
	copy(key.FilePath[:255], filePath)

	// ensure null termination
	key.FilePath[255] = 0
	value[255] = 0

	// set the cert key in the bpf map
	return m.certObjs.PidCertMap.Put(key, value)
}

func (m *Cert) unsetInjected(pid int, filePath string) error {
	// create a new cert key
	key := CertKey{}

	// copy the root id and file path to the key
	key.PID = uint32(pid)
	copy(key.FilePath[:255], filePath)

	// ensure null termination
	key.FilePath[255] = 0

	// unset the cert key in the bpf map
	return m.certObjs.PidCertMap.Delete(key)
}

func (m *Cert) setWatched(pid int, filePath string) error {
	// create a new cert key
	key := CertKey{}

	// copy the root id and file path to the key
	key.PID = uint32(pid)
	copy(key.FilePath[:255], filePath)

	// ensure null termination
	key.FilePath[255] = 0

	// set the cert key in the bpf map
	return m.tapObjs.PidCertMap.Put(key, true)
}

func (m *Cert) unsetWatched(pid int, filePath string) error {
	// create a new cert key
	key := CertKey{}

	// copy the root id and file path to the key
	key.PID = uint32(pid)
	copy(key.FilePath[:255], filePath)

	// ensure null termination
	key.FilePath[255] = 0

	// unset the cert key in the bpf map
	return m.tapObjs.PidCertMap.Delete(key)
}

func (m *Cert) generateReplacementName() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// only need to do this once
	if m.replacement != "" {
		return
	}

	// base location
	base := filepath.Join("/tmp", "ca")

	// calculate the length of the random string
	randomLength := min(
		// cut it to 6
		max(len(m.Location)-len(base)-1, 0), 6)

	// characters to use for the random string
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	// create a byte slice to store the random string
	randomString := make([]byte, randomLength)

	// generate random bytes
	for i := range randomString {
		randomString[i] = charset[rand.N(len(charset))]
	}

	// combine the base path with the random string
	m.replacement = filepath.Join(base, string(randomString))
}

func resolveCertSource(path, prefix string) (string, error) {
	// check if the path is a symlink
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// file doesn't exist, return the original path
			return path, nil
		}
		return "", fmt.Errorf("failed to get file info for %s: %w", path, err)
	}

	// if it's not a symlink, return the original path
	if info.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}

	// resolve the symlink
	resolvedPath, err := os.Readlink(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve symlink %s: %w", path, err)
	}

	// if resolved path is not absolute, make it absolute
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(filepath.Dir(path), resolvedPath)
	}

	// if prefix is empty, return the resolved path directly
	if prefix != "" && !strings.HasPrefix(resolvedPath, prefix) {
		resolvedPath = filepath.Join(prefix, resolvedPath)
	}

	// resolve symlink again in case it's circular
	return resolveCertSource(resolvedPath, prefix)
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer sourceFile.Close()

	destFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open destination file %s: %w", dst, err)
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	return nil
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func readKeyStore(filename string, password []byte) (keystore.KeyStore, error) {
	f, err := os.Open(filename)
	if err != nil {
		return keystore.KeyStore{}, fmt.Errorf("failed to open keystore file %s: %w", filename, err)
	}
	defer f.Close()

	ks := keystore.New()
	if err := ks.Load(f, password); err != nil {
		return keystore.KeyStore{}, fmt.Errorf("failed to load keystore file %s: %w", filename, err)
	}
	return ks, nil
}

func writeKeyStore(ks keystore.KeyStore, filename string, password []byte) error {
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create keystore file %s: %w", filename, err)
	}
	defer f.Close()

	if err := ks.Store(f, password); err != nil {
		return fmt.Errorf("failed to store keystore file %s: %w", filename, err)
	}

	return nil
}
