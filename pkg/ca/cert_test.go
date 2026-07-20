package ca

import (
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type overlappingDeleteMap struct {
	secondEntered chan struct{}
	waiting       atomic.Bool
	overlapped    atomic.Bool
	calls         atomic.Int32
}

type retryPutMap struct {
	putErr error
}

func (m *retryPutMap) Put(any, any) error { return m.putErr }
func (*retryPutMap) Delete(any) error     { return nil }

type cleanupOrderMap struct {
	path            string
	fileAtMapDelete bool
	deleteCalls     int
}

type failSecondPutMap struct {
	calls int
	err   error
}

func (m *failSecondPutMap) Put(any, any) error {
	m.calls++
	if m.calls == 2 {
		return m.err
	}
	return nil
}
func (*failSecondPutMap) Delete(any) error { return nil }

func (*cleanupOrderMap) Put(any, any) error { return nil }
func (m *cleanupOrderMap) Delete(any) error {
	m.deleteCalls++
	_, err := os.Stat(m.path)
	m.fileAtMapDelete = err == nil
	return nil
}

func newOverlappingDeleteMap() *overlappingDeleteMap {
	return &overlappingDeleteMap{secondEntered: make(chan struct{})}
}

func (*overlappingDeleteMap) Put(any, any) error { return nil }

func (m *overlappingDeleteMap) Delete(any) error {
	switch m.calls.Add(1) {
	case 1:
		m.waiting.Store(true)
		select {
		case <-m.secondEntered:
		case <-time.After(100 * time.Millisecond):
		}
		m.waiting.Store(false)
	case 2:
		m.overlapped.Store(m.waiting.Load())
		close(m.secondEntered)
	}
	return nil
}

func TestCertRemoveAllReturnsErrorsAndAttemptsEveryProcess(t *testing.T) {
	cert := NewCert("/ca.pem", 1, nil, InjectStrategyManual, PEM, zap.NewNop(), nil, nil)
	cert.processes.Store(10, struct{}{})
	cert.processes.Store(20, struct{}{})

	err := cert.RemoveAll()

	require.ErrorContains(t, err, "failed to unset cert key in bpf map")
	require.False(t, cert.IsEmpty(), "failed process cleanup must remain retryable")
	_, firstExists := cert.processes.Load(10)
	_, secondExists := cert.processes.Load(20)
	require.True(t, firstExists)
	require.True(t, secondExists)
}

func TestContainerCleanupReturnsCertificateErrors(t *testing.T) {
	container := NewContainer(1, 1, nil, InjectStrategyManual, zap.NewNop(), nil, nil, nil, nil)
	cert := NewCert("/ca.pem", 1, nil, InjectStrategyManual, PEM, zap.NewNop(), nil, nil)
	cert.processes.Store(10, struct{}{})
	container.certs.Store(cert.Location, cert)

	err := container.Cleanup()

	require.ErrorContains(t, err, "failed to unset cert key in bpf map")
	require.Equal(t, 1, container.certs.Len(), "failed certificate cleanup must remain retryable")
}

func TestCertRemovePreservesProcessWhenFileCleanupFails(t *testing.T) {
	destination := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(destination, "keep"), []byte("content"), 0o600))
	cert := NewCert(destination, os.Getpid(), nil, InjectStrategyInline, PEM, zap.NewNop(), nil, nil)
	cert.watchedMap = &retryPutMap{}
	cert.installed = true
	cert.createdFile = true
	cert.processes.Store(os.Getpid(), struct{}{})

	err := cert.Remove(os.Getpid())

	require.ErrorContains(t, err, "failed to remove cert")
	_, exists := cert.processes.Load(os.Getpid())
	require.True(t, exists, "failed file cleanup must remain retryable")
}

func TestCertInjectRollsBackFileWhenMapUpdateFails(t *testing.T) {
	location := filepath.Join(t.TempDir(), "ca.pem")
	original := []byte("original certificate bundle\n")
	require.NoError(t, os.WriteFile(location, original, 0o600))

	mapErr := errors.New("map update failed")
	watchMap := &retryPutMap{putErr: mapErr}
	cert := NewCert(location, os.Getpid(), []byte("qtap-ca"), InjectStrategyInline, PEM, zap.NewNop(), nil, nil)
	cert.watchedMap = watchMap

	require.ErrorIs(t, cert.Inject(os.Getpid(), 1), mapErr)
	content, err := os.ReadFile(location)
	require.NoError(t, err)
	require.Equal(t, original, content, "failed injection must restore the trust bundle")

	watchMap.putErr = nil
	require.NoError(t, cert.Inject(os.Getpid(), 1), "injection must remain retryable after rollback")
	require.NoError(t, cert.Remove(os.Getpid()))
	content, err = os.ReadFile(location)
	require.NoError(t, err)
	require.Equal(t, original, content)
}

func TestCertSecondMapFailurePreservesSharedInstallation(t *testing.T) {
	location := filepath.Join(t.TempDir(), "ca.pem")
	original := []byte("original\n")
	require.NoError(t, os.WriteFile(location, original, 0o600))
	mapErr := errors.New("map full")
	watchMap := &failSecondPutMap{err: mapErr}
	cert := NewCert(location, os.Getpid(), []byte("qtap-ca"), InjectStrategyInline, PEM, zap.NewNop(), nil, nil)
	cert.watchedMap = watchMap

	require.NoError(t, cert.Inject(10, 1))
	contentAfterFirst, err := os.ReadFile(location)
	require.NoError(t, err)
	require.ErrorIs(t, cert.Inject(20, 1), mapErr)
	contentAfterSecond, err := os.ReadFile(location)
	require.NoError(t, err)
	require.Equal(t, contentAfterFirst, contentAfterSecond, "a later process failure must not remove shared trust")
	require.NotEqual(t, original, contentAfterSecond)
}

func TestCertRemoveRestoresExactOriginalBundle(t *testing.T) {
	location := filepath.Join(t.TempDir(), "ca.pem")
	caBytes := []byte("same-ca")
	original := append([]byte("bundle prefix\n"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caBytes})...)
	require.NoError(t, os.WriteFile(location, original, 0o640))

	cert := NewCert(location, os.Getpid(), caBytes, InjectStrategyInline, PEM, zap.NewNop(), nil, nil)
	cert.watchedMap = &retryPutMap{}
	require.NoError(t, cert.Inject(os.Getpid(), 1))
	require.NoError(t, cert.Remove(os.Getpid()))

	content, err := os.ReadFile(location)
	require.NoError(t, err)
	require.Equal(t, original, content)
	info, err := os.Stat(location)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

func TestCertConcurrentRemovalsDoNotSkipLastProcessCleanup(t *testing.T) {
	location := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(location, []byte("certificate"), 0o600))

	cert := NewCert(location, os.Getpid(), nil, InjectStrategyInline, PEM, zap.NewNop(), nil, nil)
	cert.watchedMap = newOverlappingDeleteMap()
	cert.installed = true
	cert.createdFile = true
	cert.processes.Store(10, struct{}{})
	cert.processes.Store(20, struct{}{})

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, pid := range []int{10, 20} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- cert.Remove(pid)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.NoFileExists(t, location, "the final removal must perform native cleanup")
	require.False(t, cert.watchedMap.(*overlappingDeleteMap).overlapped.Load(), "certificate transitions must not overlap")
}

func TestContainerRemovalUsesRemainingProcessNamespace(t *testing.T) {
	location := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(location, []byte("certificate"), 0o600))

	cert := NewCert(location, -1, nil, InjectStrategyInline, PEM, zap.NewNop(), nil, nil)
	cert.watchedMap = &retryPutMap{}
	cert.installed = true
	cert.createdFile = true
	cert.processes.Store(10, struct{}{})
	cert.processes.Store(os.Getpid(), struct{}{})
	container := NewContainer(1, -1, nil, InjectStrategyInline, zap.NewNop(), nil, nil, nil, nil)
	container.certs.Store(location, cert)
	container.pids.Store(10, nil)
	container.pids.Store(os.Getpid(), nil)

	require.NoError(t, container.RemoveProcess(10))
	require.NoError(t, container.RemoveProcess(os.Getpid()))
	require.NoFileExists(t, location, "cleanup must use a live process sharing the root namespace")
}

func TestCertRemoveDisablesMapBeforeRestoringTrust(t *testing.T) {
	location := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(location, []byte("certificate"), 0o600))
	watchMap := &cleanupOrderMap{path: location}
	cert := NewCert(location, os.Getpid(), nil, InjectStrategyInline, PEM, zap.NewNop(), nil, nil)
	cert.watchedMap = watchMap
	cert.installed = true
	cert.createdFile = true
	cert.processes.Store(os.Getpid(), struct{}{})

	require.NoError(t, cert.Remove(os.Getpid()))
	require.True(t, watchMap.fileAtMapDelete, "routing/watch state must be disabled before trust is removed")
	require.NoFileExists(t, location)
}
