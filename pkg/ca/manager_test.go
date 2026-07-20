package ca

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type blockingRingReader struct {
	started chan struct{}
	closed  chan struct{}
	release chan struct{}
	once    sync.Once
}

type errorRingReader struct{}

func (errorRingReader) ReadInto(*ringbuf.Record) error { return os.ErrClosed }
func (errorRingReader) Close() error                   { return errors.New("reader close failed") }

type errorProbe struct{}

func (errorProbe) Attach() error { return nil }
func (errorProbe) Detach() error { return errors.New("probe detach failed") }
func (errorProbe) ID() string    { return "test-probe" }

var _ common.Probe = errorProbe{}

type trackingProbe struct {
	attachErr   error
	detachErr   error
	attachCalls atomic.Int32
	detachCalls atomic.Int32
}

func (p *trackingProbe) Attach() error {
	p.attachCalls.Add(1)
	return p.attachErr
}

func (p *trackingProbe) Detach() error {
	p.detachCalls.Add(1)
	return p.detachErr
}

func (*trackingProbe) ID() string { return "tracking-probe" }

type trackingRingReader struct {
	closed     chan struct{}
	closeErr   error
	closeOnce  sync.Once
	readCalls  atomic.Int32
	closeCalls atomic.Int32
}

type retryCloseReader struct {
	started    chan struct{}
	closed     chan struct{}
	startOnce  sync.Once
	closeOnce  sync.Once
	closeCalls atomic.Int32
}

func newRetryCloseReader() *retryCloseReader {
	return &retryCloseReader{
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (r *retryCloseReader) ReadInto(*ringbuf.Record) error {
	r.startOnce.Do(func() { close(r.started) })
	<-r.closed
	return os.ErrClosed
}

func (r *retryCloseReader) Close() error {
	if r.closeCalls.Add(1) == 1 {
		return errors.New("reader close failed")
	}
	r.forceClose()
	return nil
}

func (r *retryCloseReader) forceClose() {
	r.closeOnce.Do(func() { close(r.closed) })
}

type retryDetachProbe struct {
	attachErr   error
	attachCalls atomic.Int32
	detachCalls atomic.Int32
}

func (p *retryDetachProbe) Attach() error {
	p.attachCalls.Add(1)
	return p.attachErr
}

func (p *retryDetachProbe) Detach() error {
	if p.detachCalls.Add(1) == 1 {
		return errors.New("probe detach failed")
	}
	return nil
}

func (*retryDetachProbe) ID() string { return "retry-probe" }

func newTrackingRingReader() *trackingRingReader {
	return &trackingRingReader{closed: make(chan struct{})}
}

func (r *trackingRingReader) ReadInto(*ringbuf.Record) error {
	r.readCalls.Add(1)
	<-r.closed
	return os.ErrClosed
}

func (r *trackingRingReader) Close() error {
	r.closeCalls.Add(1)
	r.closeOnce.Do(func() { close(r.closed) })
	return r.closeErr
}

type processGetterStub struct {
	process *process.Process
}

func (s processGetterStub) Get(int) *process.Process { return s.process }

type blockingObserver struct {
	started      chan struct{}
	release      chan struct{}
	secondCalled chan struct{}
	calls        atomic.Int32
}

type recordingObserver struct {
	certReads chan string
}

func (*recordingObserver) CertInjected(*process.Process, string, uint64) error { return nil }
func (*recordingObserver) CertRemoved(*process.Process, string, uint64) error  { return nil }
func (o *recordingObserver) CertRead(_ *process.Process, path string) error {
	o.certReads <- path
	return nil
}

type transientRingReader struct {
	raw   []byte
	calls atomic.Int32
}

func (r *transientRingReader) ReadInto(record *ringbuf.Record) error {
	if r.calls.Add(1) == 1 {
		record.RawSample = r.raw
		return errors.New("transient read failure")
	}
	return os.ErrClosed
}

func (*transientRingReader) Close() error { return nil }

func (o *blockingObserver) CertInjected(*process.Process, string, uint64) error { return nil }
func (o *blockingObserver) CertRemoved(*process.Process, string, uint64) error  { return nil }
func (o *blockingObserver) CertRead(*process.Process, string) error {
	if o.calls.Add(1) == 1 {
		close(o.started)
		<-o.release
	} else {
		close(o.secondCalled)
	}
	return nil
}

func newBlockingRingReader() *blockingRingReader {
	return &blockingRingReader{
		started: make(chan struct{}),
		closed:  make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (r *blockingRingReader) ReadInto(*ringbuf.Record) error {
	r.once.Do(func() { close(r.started) })
	<-r.closed
	<-r.release
	return os.ErrClosed
}

func (r *blockingRingReader) Close() error {
	close(r.closed)
	return nil
}

func TestCaManagerStopClosesReaderAndWaitsForReadLoop(t *testing.T) {
	reader := newBlockingRingReader()
	manager := &CaManager{
		logger:       zap.NewNop(),
		containers:   synq.NewMap[uint64, *Container](),
		rdCertEvents: reader,
	}

	manager.readerWG.Add(1)
	go func() {
		defer manager.readerWG.Done()
		manager.readCertEvents(t.Context(), reader)
	}()
	<-reader.started

	stopped := make(chan error, 1)
	go func() { stopped <- manager.Stop() }()

	select {
	case <-reader.closed:
	case <-time.After(time.Second):
		t.Fatal("Stop did not close the ring reader")
	}

	select {
	case err := <-stopped:
		require.Failf(t, "Stop returned before the read loop exited", "error: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(reader.release)
	require.NoError(t, <-stopped)
}

func TestCaManagerReadCertEventsDoesNotParseRecordAfterReadFailure(t *testing.T) {
	var sample bytes.Buffer
	require.NoError(t, binary.Write(&sample, binary.NativeEndian, CertEventMeta{Type: CertRead}))
	event := CertReadEvent{Pid: 42, FileSize: uint32(len("/stale.pem") + 1)}
	copy(event.File[:], []int8{'/', 's', 't', 'a', 'l', 'e', '.', 'p', 'e', 'm', 0})
	require.NoError(t, binary.Write(&sample, binary.NativeEndian, event))

	reader := &transientRingReader{raw: sample.Bytes()}
	observer := &recordingObserver{certReads: make(chan string, 1)}
	manager := &CaManager{
		logger:         zap.NewNop(),
		processManager: processGetterStub{process: &process.Process{Pid: 42}},
		state:          caManagerStarted,
	}
	manager.Observe(observer)

	manager.readCertEvents(t.Context(), reader)

	select {
	case path := <-observer.certReads:
		t.Fatalf("parsed stale record after read failure: %s", path)
	case <-time.After(20 * time.Millisecond):
	}
	require.EqualValues(t, 2, reader.calls.Load())
}

func TestCaManagerStartIsIdempotentAndCannotRestartAfterStop(t *testing.T) {
	reader := newTrackingRingReader()
	probe := &trackingProbe{}
	var readerOpens atomic.Int32
	manager := NewCaManager(nil, InjectStrategyManual, zap.NewNop(), nil, nil, nil)
	manager.openReader = func() (certEventReader, error) {
		readerOpens.Add(1)
		return reader, nil
	}
	manager.newProbes = func() []common.Probe { return []common.Probe{probe} }

	require.NoError(t, manager.Start(t.Context()))
	require.NoError(t, manager.Start(t.Context()))
	require.EqualValues(t, 1, readerOpens.Load())
	require.EqualValues(t, 1, probe.attachCalls.Load())

	require.NoError(t, manager.Stop())
	err := manager.Start(t.Context())
	require.ErrorContains(t, err, "after it has stopped")
	require.EqualValues(t, 1, readerOpens.Load())
}

func TestCaManagerSerializesStartAndStop(t *testing.T) {
	reader := newTrackingRingReader()
	probe := &trackingProbe{}
	opening := make(chan struct{})
	continueOpening := make(chan struct{})
	manager := NewCaManager(nil, InjectStrategyManual, zap.NewNop(), nil, nil, nil)
	manager.openReader = func() (certEventReader, error) {
		close(opening)
		<-continueOpening
		return reader, nil
	}
	manager.newProbes = func() []common.Probe { return []common.Probe{probe} }

	started := make(chan error, 1)
	go func() { started <- manager.Start(t.Context()) }()
	<-opening

	stopped := make(chan error, 1)
	go func() { stopped <- manager.Stop() }()
	select {
	case err := <-stopped:
		require.Failf(t, "Stop completed while Start was still opening resources", "error: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(continueOpening)
	require.NoError(t, <-started)
	require.NoError(t, <-stopped)
	require.EqualValues(t, 1, reader.closeCalls.Load())
	require.EqualValues(t, 1, probe.detachCalls.Load())
}

func TestCaManagerStartRollsBackReaderAndAttachedProbes(t *testing.T) {
	reader := newTrackingRingReader()
	attachedProbe := &trackingProbe{}
	failingProbe := &trackingProbe{attachErr: errors.New("attach failed")}
	manager := NewCaManager(nil, InjectStrategyManual, zap.NewNop(), nil, nil, nil)
	manager.openReader = func() (certEventReader, error) { return reader, nil }
	manager.newProbes = func() []common.Probe {
		return []common.Probe{attachedProbe, failingProbe}
	}

	err := manager.Start(t.Context())

	require.ErrorContains(t, err, "attach failed")
	require.EqualValues(t, 1, reader.closeCalls.Load())
	require.Zero(t, reader.readCalls.Load(), "read loop should not start before probes attach")
	require.EqualValues(t, 1, attachedProbe.detachCalls.Load())
}

func TestCaManagerFailedStartupCleanupBlocksStartAndCanBeRetried(t *testing.T) {
	reader := newRetryCloseReader()
	attachedProbe := &retryDetachProbe{}
	failingProbe := &trackingProbe{attachErr: errors.New("attach failed")}
	var readerOpens atomic.Int32
	manager := NewCaManager(nil, InjectStrategyManual, zap.NewNop(), nil, nil, nil)
	manager.openReader = func() (certEventReader, error) {
		readerOpens.Add(1)
		return reader, nil
	}
	manager.newProbes = func() []common.Probe {
		return []common.Probe{attachedProbe, failingProbe}
	}

	require.ErrorContains(t, manager.Start(t.Context()), "attach failed")
	require.Error(t, manager.Start(t.Context()), "Start must remain blocked while rollback resources are live")
	require.EqualValues(t, 1, readerOpens.Load(), "blocked Start must not open duplicate resources")
	require.EqualValues(t, 1, attachedProbe.attachCalls.Load())

	require.NoError(t, manager.Stop(), "Stop must retry failed startup cleanup")
	require.EqualValues(t, 2, reader.closeCalls.Load())
	require.EqualValues(t, 2, attachedProbe.detachCalls.Load())
}

func TestCaManagerStopRetriesFailedReaderAndProbeCleanupWithoutHanging(t *testing.T) {
	reader := newRetryCloseReader()
	probe := &retryDetachProbe{}
	manager := NewCaManager(nil, InjectStrategyManual, zap.NewNop(), nil, nil, nil)
	manager.openReader = func() (certEventReader, error) { return reader, nil }
	manager.newProbes = func() []common.Probe { return []common.Probe{probe} }
	require.NoError(t, manager.Start(t.Context()))
	<-reader.started

	firstStop := make(chan error, 1)
	go func() { firstStop <- manager.Stop() }()

	returnedPromptly := false
	var firstErr error
	select {
	case firstErr = <-firstStop:
		returnedPromptly = true
	case <-time.After(100 * time.Millisecond):
		reader.forceClose()
		firstErr = <-firstStop
	}

	require.ErrorContains(t, firstErr, "reader close failed")
	require.ErrorContains(t, firstErr, "probe detach failed")
	require.True(t, returnedPromptly, "Stop must not wait for a reader whose Close failed")
	require.NoError(t, manager.Stop(), "a second Stop must retry retained resources")
	require.EqualValues(t, 2, reader.closeCalls.Load())
	require.EqualValues(t, 2, probe.detachCalls.Load())
}

func TestCaManagerStopWaitsForObserversAndPreventsNewNotifications(t *testing.T) {
	observer := &blockingObserver{
		started:      make(chan struct{}),
		release:      make(chan struct{}),
		secondCalled: make(chan struct{}),
	}
	manager := &CaManager{
		logger:         zap.NewNop(),
		containers:     synq.NewMap[uint64, *Container](),
		processManager: processGetterStub{process: &process.Process{Pid: 42, RootID: 7}},
		state:          caManagerStarted,
	}
	container := NewContainer(7, 42, nil, InjectStrategyManual, zap.NewNop(), nil, nil, nil, nil)
	cert := NewCert("/ca.pem", 42, nil, InjectStrategyManual, PEM, zap.NewNop(), nil, nil)
	cert.watchedMap = &retryPutMap{}
	cert.processes.Store(42, struct{}{})
	container.certs.Store(cert.Location, cert)
	manager.containers.Store(7, container)
	manager.Observe(observer)

	firstNotification := make(chan error, 1)
	go func() { firstNotification <- manager.handleCertRead(42, "/ca.pem") }()
	<-observer.started

	stopped := make(chan error, 1)
	go func() { stopped <- manager.Stop() }()
	select {
	case err := <-stopped:
		require.Failf(t, "Stop returned before the observer exited", "error: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(observer.release)
	require.NoError(t, <-firstNotification)
	require.NoError(t, <-stopped)
	require.NoError(t, manager.handleCertRead(42, "/another-ca.pem"))

	select {
	case <-observer.secondCalled:
		t.Fatal("observer was notified after Stop returned")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestCaManagerSerializesObserverNotifications(t *testing.T) {
	observer := &blockingObserver{
		started:      make(chan struct{}),
		release:      make(chan struct{}),
		secondCalled: make(chan struct{}),
	}
	manager := &CaManager{
		logger:         zap.NewNop(),
		containers:     synq.NewMap[uint64, *Container](),
		processManager: processGetterStub{process: &process.Process{Pid: 42, RootID: 7}},
		state:          caManagerStarted,
	}
	container := NewContainer(7, 42, nil, InjectStrategyManual, zap.NewNop(), nil, nil, nil, nil)
	cert := NewCert("/ca.pem", 42, nil, InjectStrategyManual, PEM, zap.NewNop(), nil, nil)
	cert.processes.Store(42, struct{}{})
	container.certs.Store(cert.Location, cert)
	manager.containers.Store(7, container)
	manager.Observe(observer)

	firstDone := make(chan error, 1)
	go func() { firstDone <- manager.handleCertRead(42, "/ca.pem") }()
	<-observer.started
	secondDone := make(chan error, 1)
	go func() { secondDone <- manager.handleCertRead(42, "/ca.pem") }()

	select {
	case <-observer.secondCalled:
		t.Fatal("later certificate notification overtook an earlier notification")
	case <-time.After(20 * time.Millisecond):
	}
	close(observer.release)
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
}

func TestCaManagerStopWaitsForProcessCallbacksAndRejectsNewWork(t *testing.T) {
	container := NewContainer(7, 1, nil, InjectStrategyManual, zap.NewNop(), nil, nil, nil, nil)
	container.pids.Store(99, nil)
	manager := &CaManager{
		logger:     zap.NewNop(),
		containers: synq.NewMap[uint64, *Container](),
		state:      caManagerStarted,
	}
	manager.containers.Store(7, container)

	container.mu.Lock()
	locked := true
	defer func() {
		if locked {
			container.mu.Unlock()
		}
	}()

	callbackDone := make(chan error, 1)
	go func() {
		callbackDone <- manager.ProcessStarted(t.Context(), &process.Process{
			Pid:      10,
			RootID:   7,
			Strategy: process.StrategyForward,
			Env:      map[string]string{},
		})
	}()
	require.Eventually(t, func() bool {
		manager.lifecycleMu.Lock()
		defer manager.lifecycleMu.Unlock()
		return manager.inFlight == 1
	}, time.Second, time.Millisecond)

	stopped := make(chan error, 1)
	go func() { stopped <- manager.Stop() }()
	require.Eventually(t, func() bool {
		manager.lifecycleMu.Lock()
		defer manager.lifecycleMu.Unlock()
		return manager.state == caManagerStopping
	}, time.Second, time.Millisecond)

	require.NoError(t, manager.ProcessStarted(t.Context(), &process.Process{Pid: 20, RootID: 8}))
	_, addedAfterStop := manager.containers.Load(8)
	require.False(t, addedAfterStop)
	require.NoError(t, manager.ProcessStopped(t.Context(), &process.Process{Pid: 99, RootID: 7}))
	_, removedAfterStop := container.pids.Load(99)
	require.True(t, removedAfterStop)

	select {
	case err := <-stopped:
		require.Failf(t, "Stop returned before the process callback exited", "error: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	container.mu.Unlock()
	locked = false
	require.NoError(t, <-callbackDone)
	require.NoError(t, <-stopped)
}

func TestCaManagerProcessReplacedRescansCertificateEnvironment(t *testing.T) {
	certFile := t.TempDir() + "/ca.pem"
	require.NoError(t, os.WriteFile(certFile, []byte("original\n"), 0o600))
	container := NewContainer(7, os.Getpid(), []byte("qtap-ca"), InjectStrategyInline, zap.NewNop(), nil, nil, nil, nil)
	container.pids.Store(os.Getpid(), nil)
	manager := &CaManager{
		caBytes:    []byte("qtap-ca"),
		strategy:   InjectStrategyInline,
		logger:     zap.NewNop(),
		containers: synq.NewMap[uint64, *Container](),
		state:      caManagerStarted,
	}
	manager.containers.Store(7, container)

	p := process.NewProcess(os.Getpid(), "", zap.NewNop())
	p.RootID = 7
	p.Strategy = process.StrategyProxy
	p.Env = map[string]string{"SSL_CERT_FILE": certFile}
	require.NoError(t, p.SetTlsOk(true))
	err := manager.ProcessReplaced(t.Context(), p)

	require.ErrorContains(t, err, "failed to inject cert", "replacement must rescan the new environment")
	require.False(t, p.TlsOk(), "replacement must revoke stale TLS termination eligibility")
	content, readErr := os.ReadFile(certFile)
	require.NoError(t, readErr)
	require.Equal(t, []byte("original\n"), content, "failed replacement injection must restore the bundle")
}

func TestCaManagerProcessStartedRetriesScanForKnownPID(t *testing.T) {
	certFile := t.TempDir() + "/ca.pem"
	require.NoError(t, os.WriteFile(certFile, []byte("original\n"), 0o600))
	container := NewContainer(7, os.Getpid(), []byte("qtap-ca"), InjectStrategyInline, zap.NewNop(), nil, nil, nil, nil)
	container.pids.Store(os.Getpid(), nil)
	manager := &CaManager{
		caBytes:    []byte("qtap-ca"),
		strategy:   InjectStrategyInline,
		logger:     zap.NewNop(),
		containers: synq.NewMap[uint64, *Container](),
		state:      caManagerStarted,
	}
	manager.containers.Store(7, container)
	p := &process.Process{
		Pid:      os.Getpid(),
		RootID:   7,
		Strategy: process.StrategyProxy,
		Env:      map[string]string{"SSL_CERT_FILE": certFile},
	}

	require.ErrorContains(t, manager.ProcessStarted(t.Context(), p), "failed to inject cert")
	require.ErrorContains(t, manager.ProcessStarted(t.Context(), p), "failed to inject cert", "known PIDs must retry failed scans")
}

func TestCaManagerDelayedOldStopPreservesReusedPIDMap(t *testing.T) {
	oldProcess := &process.Process{Pid: 42, RootID: 7}
	newProcess := &process.Process{Pid: 42, RootID: 8}
	watchMap := &cleanupOrderMap{}
	cert := NewCert("/ca.pem", os.Getpid(), nil, InjectStrategyManual, PEM, zap.NewNop(), nil, nil)
	cert.watchedMap = watchMap
	cert.processes.Store(42, struct{}{})
	container := NewContainer(7, os.Getpid(), nil, InjectStrategyManual, zap.NewNop(), nil, nil, nil, nil)
	container.certs.Store(cert.Location, cert)
	container.pids.Store(42, nil)
	manager := &CaManager{
		logger:         zap.NewNop(),
		containers:     synq.NewMap[uint64, *Container](),
		processManager: processGetterStub{process: newProcess},
		state:          caManagerStarted,
	}
	manager.containers.Store(7, container)

	require.NoError(t, manager.ProcessStopped(t.Context(), oldProcess))
	require.Zero(t, watchMap.deleteCalls, "old cleanup must not delete a reused PID's map entry")
}

func TestCaManagerStopAggregatesCleanupErrors(t *testing.T) {
	container := NewContainer(7, 1, nil, InjectStrategyManual, zap.NewNop(), nil, nil, nil, nil)
	cert := NewCert("/ca.pem", 1, nil, InjectStrategyManual, PEM, zap.NewNop(), nil, nil)
	cert.processes.Store(10, struct{}{})
	container.certs.Store(cert.Location, cert)

	manager := &CaManager{
		logger:       zap.NewNop(),
		containers:   synq.NewMap[uint64, *Container](),
		rdCertEvents: errorRingReader{},
		probes:       []common.Probe{errorProbe{}},
	}
	manager.containers.Store(7, container)

	err := manager.Stop()

	require.ErrorContains(t, err, "reader close failed")
	require.ErrorContains(t, err, "probe detach failed")
	require.ErrorContains(t, err, "failed to unset cert key in bpf map")
	_, exists := manager.containers.Load(7)
	require.True(t, exists, "failed container cleanup must remain retryable")

	retryErr := manager.Stop()
	require.ErrorContains(t, retryErr, "failed to unset cert key in bpf map")
}

func TestCaManagerStopDeletesSuccessfullyCleanedContainers(t *testing.T) {
	manager := &CaManager{
		logger:     zap.NewNop(),
		containers: synq.NewMap[uint64, *Container](),
		state:      caManagerStarted,
	}
	manager.containers.Store(7, NewContainer(7, 1, nil, InjectStrategyManual, zap.NewNop(), nil, nil, nil, nil))

	require.NoError(t, manager.Stop())
	_, exists := manager.containers.Load(7)
	require.False(t, exists)
}

func TestCaManagerProcessStoppedReturnsCleanupErrors(t *testing.T) {
	container := NewContainer(7, 1, nil, InjectStrategyManual, zap.NewNop(), nil, nil, nil, nil)
	container.pids.Store(10, nil)
	cert := NewCert("/ca.pem", 1, nil, InjectStrategyManual, PEM, zap.NewNop(), nil, nil)
	cert.processes.Store(10, struct{}{})
	container.certs.Store(cert.Location, cert)

	manager := &CaManager{
		logger:     zap.NewNop(),
		containers: synq.NewMap[uint64, *Container](),
		state:      caManagerStarted,
	}
	manager.containers.Store(7, container)

	err := manager.ProcessStopped(t.Context(), &process.Process{Pid: 10, RootID: 7})

	require.ErrorContains(t, err, "failed to unset cert key in bpf map")
	_, exists := manager.containers.Load(7)
	require.True(t, exists, "failed container cleanup must remain retryable")
	_, processExists := container.pids.Load(10)
	require.True(t, processExists, "failed process cleanup must remain retryable")
	_, certExists := container.certs.Load(cert.Location)
	require.True(t, certExists, "failed certificate cleanup must remain retryable")
}
