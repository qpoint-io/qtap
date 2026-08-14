package javassl

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
	agentConfig "github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/l7detect"
	"github.com/qpoint-io/qtap/pkg/synq"
	"go.uber.org/zap"
)

// Java's SSLEngine is a TLS library for handling TLS connections in an asynchronous manner.
// The fundamental challenge is that SSLEngine decouples the encryption/decryption from the
// network I/O, which means we lose the ability to correlate plaintext data with the underlying
// network connection.
//
// To further complicate matters, eBPF is highly constrained and buffering data (which is needed for correlation)
// is too complex to fit within the limited instructions available (10k). Instead, we use eBPF uprobes and syscall probes
// to capture the data and metadata, then submit the data to this manager via a ringbuffer. This manager is responsible
// for reading the events off of the ringbuffer and correlating the data with the underlying network connection,
// than finally forwarding the data to the connection manager to be consumed by the larger system.

type ConnectionEvents interface {
	WriteProtocolEvent(cookie uint64, protocol connection.Protocol, isTLS bool) error
	WriteDataEvent(cookie uint64, direction connection.Direction, data []byte) error
}

// probes and maps to the eBPF program
type SslEngineBridge struct {
	// socket probes
	SocketProbes []*common.Tracepoint

	// java process pid map
	JavaProcessPidMap *ebpf.Map

	// SSLEngine session ignore map
	SessionIgnoreMap *ebpf.Map

	// syscall correlated map
	SyscallCorrelatedMap *ebpf.Map

	// uprobe correlated map
	UprobeCorrelatedMap *ebpf.Map

	// events ringbuffer reader
	EventsRingbufferReader *ringbuf.Reader
}

type correlationSource uint32

const (
	CORRELATION_SOURCE_UPROBE correlationSource = iota
	CORRELATION_SOURCE_SYSCALL
)

func (s correlationSource) String() string {
	switch s {
	case CORRELATION_SOURCE_UPROBE:
		return "uprobe"
	case CORRELATION_SOURCE_SYSCALL:
		return "syscall"
	}
	return "unknown"
}

// correlation (SSLEngine session id <-> connection (or pid/fd pair))
type correlation struct {
	Timestamp time.Time
	Pid       uint32
	SessionId uint64
	Cookie    uint64
	Fd        int32
}

// plaintext data, awaiting correlation
type pendingMessage struct {
	Direction direction
	Msg       []byte
}

// track all metadata for a pid for proper cleanup
type pidMeta struct {
	SessionIds []uint64
	PidFds     []uint64
}

type SslEngineManager struct {
	logger *zap.Logger

	// available stream protocols
	streamProtocols *synq.Map[string, bool]

	// bridge to the eBPF program
	bridge *SslEngineBridge

	// connection events
	connEvents ConnectionEvents

	// active correlations map (SSLEngine session id -> correlation)
	activeCorrelations *synq.Map[uint64, *correlation]

	// pending correlation map (prefix hash -> correlation)
	pendingCorrelations *synq.Map[uint64, *correlation]

	// map of source (pid/fd key or session id) -> pending correlations (needed for cleanup after correlation)
	pendingCorrelationsBySource *synq.Map[uint64, []uint64]

	// plaintext pending (SSLEngine session id -> plaintext pending queue)
	pendingMessages *synq.Map[uint64, []*pendingMessage]

	// map of pid/fd key -> session id (needed for cleanup when socket is closed)
	pidFdSessionIdMap *synq.Map[uint64, uint64]

	// map of pid -> metadata
	pidMeta *synq.Map[uint32, *pidMeta]

	// pending correlation expiration ticker
	pendingCorrelationExpirationTicker *time.Ticker
}

func NewSslEngineManager(logger *zap.Logger, bridge *SslEngineBridge, connEvents ConnectionEvents) *SslEngineManager {
	return &SslEngineManager{
		logger:                      logger,
		bridge:                      bridge,
		connEvents:                  connEvents,
		streamProtocols:             synq.NewMap[string, bool](),
		activeCorrelations:          synq.NewMap[uint64, *correlation](),
		pendingCorrelations:         synq.NewMap[uint64, *correlation](),
		pendingMessages:             synq.NewMap[uint64, []*pendingMessage](),
		pendingCorrelationsBySource: synq.NewMap[uint64, []uint64](),
		pidFdSessionIdMap:           synq.NewMap[uint64, uint64](),
		pidMeta:                     synq.NewMap[uint32, *pidMeta](),
	}
}

func (m *SslEngineManager) Start() error {
	// attach the socket probes
	for _, probe := range m.bridge.SocketProbes {
		if err := probe.Attach(); err != nil {
			return fmt.Errorf("failed to attach socket probe: %w", err)
		}
	}

	// start the event reader
	go m.readEvents()

	// expire pending correlations
	m.expirePendingCorrelations()

	return nil
}

func (m *SslEngineManager) Stop() error {
	// detach the socket probes
	for _, probe := range m.bridge.SocketProbes {
		if err := probe.Detach(); err != nil {
			return fmt.Errorf("failed to detach socket probe: %w", err)
		}
	}

	// stop the pending correlation expiration ticker
	if m.pendingCorrelationExpirationTicker != nil {
		m.pendingCorrelationExpirationTicker.Stop()
	}

	return nil
}

func (m *SslEngineManager) SetConfig(config *agentConfig.Config) {
	// get the tap config
	tapConfig := config.Tap
	if tapConfig == nil {
		return
	}

	// clear existing protocols
	m.streamProtocols.Iter(func(key string, _ bool) bool {
		m.streamProtocols.Delete(key)
		return true
	})

	// add new protocols
	for _, protocol := range tapConfig.GetAllProtocols() {
		m.streamProtocols.Store(protocol, true)
	}
}

func (m *SslEngineManager) ProcessStarted(pid int) error {
	// store the java process pid
	if err := m.bridge.JavaProcessPidMap.Put(uint32(pid), true); err != nil {
		return err
	}

	return nil
}

func (m *SslEngineManager) ProcessStopped(pid int) error {
	// remove the java process pid
	if err := m.bridge.JavaProcessPidMap.Delete(uint32(pid)); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
		return err
	}

	// clean all correlation metadata from the pid
	m.cleanAllFromPid(uint32(pid))

	return nil
}

func (m *SslEngineManager) ProcessSocketClosed(pid uint32, fd int32) error {
	// find the session id for this pid/fd
	sessionId, ok := m.pidFdSessionIdMap.Load(generatePidFdKey(pid, fd))
	if !ok {
		return nil
	}

	// remove the session id from the pid/fd map
	m.pidFdSessionIdMap.Delete(generatePidFdKey(pid, fd))

	// remove the session id from the active correlations
	m.activeCorrelations.Delete(sessionId)

	// remove the pending correlations by source (session id)
	m.removePendingCorrelationsBySource(sessionId)

	// remove the pending correlations by source (pid/fd key)
	m.removePendingCorrelationsBySource(generatePidFdKey(pid, fd))

	// remove any pending messages for this session
	m.pendingMessages.Delete(sessionId)

	// remove the pid/fd from the pid meta
	m.removePidFdFromPidMeta(pid, fd)

	// remove the session id from the pid meta
	m.removeSessionIdFromPidMeta(pid, sessionId)

	// clear the session ignored
	m.clearSessionIgnored(sessionId)

	// clear the uprobe correlated
	m.clearUprobeCorrelated(sessionId)

	// clear the syscall correlated
	m.clearSyscallCorrelated(pid, fd)

	return nil
}

func (m *SslEngineManager) ProcessPlaintextData(pid uint32, sessionId uint64, direction direction, msg []byte) error {
	// do we have a correlation for this session?
	correlation, ok := m.activeCorrelations.Load(sessionId)
	if ok {
		// ensure this session is not ignored
		isIgnored := false
		if err := m.bridge.SessionIgnoreMap.Lookup(sessionId, &isIgnored); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
			m.logger.Debug("failed to lookup session ignore map", zap.Error(err))
		}
		if isIgnored {
			return nil
		}

		// write the plaintext data to the connection events
		if err := m.connEvents.WriteDataEvent(correlation.Cookie, direction.ToConnectionDirection(), msg); err != nil {
			m.logger.Debug("failed to write Java SSLEngine plaintext data to connection events", zap.Error(err))
		}

		return nil
	}

	// do we have any pending messages for this session already?
	pendingMessages, ok := m.pendingMessages.Load(sessionId)
	if ok {
		// add the message to the pending messages
		pendingMessages = append(pendingMessages, &pendingMessage{
			Direction: direction,
			Msg:       msg,
		})
	} else {
		pendingMessages = []*pendingMessage{
			{
				Direction: direction,
				Msg:       msg,
			},
		}
	}

	// update the pending messages
	m.pendingMessages.Store(sessionId, pendingMessages)

	return nil
}

func (m *SslEngineManager) ProcessEncryptedData(pid uint32, sessionId uint64, direction direction, msg []byte) error {
	// ensure we have at least 16 bytes
	if len(msg) < 16 {
		return nil
	}

	// ensure we don't already have a correlation for this session
	_, ok := m.activeCorrelations.Load(sessionId)
	if ok {
		return nil
	}

	// generate a prefix hash
	prefixHash := generatePrefixHash(msg)

	// create a correlation
	correlation := &correlation{
		Timestamp: time.Now(),
		Pid:       pid,
		SessionId: sessionId,
		Cookie:    0,
		Fd:        -1,
	}

	// process the correlation
	return m.processCorrelation(CORRELATION_SOURCE_UPROBE, prefixHash, correlation)
}

func (m *SslEngineManager) ProcessCorrelationData(pid uint32, fd int32, cookie uint64, direction direction, msg []byte) error {
	// ensure we have at least 16 bytes
	if len(msg) < 16 {
		return nil
	}

	// ensure we don't already have a correlation for this pid/fd
	exists := false
	if err := m.bridge.SyscallCorrelatedMap.Lookup(struct {
		Pid uint32
		Fd  int32
	}{
		Pid: pid,
		Fd:  fd,
	}, &exists); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
		m.logger.Debug("failed to lookup syscall correlated map", zap.Error(err))
	}
	if exists {
		return nil
	}

	// generate a prefix hash
	prefixHash := generatePrefixHash(msg)

	// create a correlation
	correlation := &correlation{
		Timestamp: time.Now(),
		Pid:       pid,
		SessionId: 0,
		Cookie:    cookie,
		Fd:        fd,
	}

	// process the correlation
	return m.processCorrelation(CORRELATION_SOURCE_SYSCALL, prefixHash, correlation)
}

func (m *SslEngineManager) processCorrelation(source correlationSource, hash uint64, correlation *correlation) error {
	// do we have a pending correlation for this hash?
	pendingCorrelation, ok := m.pendingCorrelations.Load(hash)
	if ok {
		// converge the pending correlation with this incoming correlation
		switch source {
		case CORRELATION_SOURCE_UPROBE:
			correlation.Fd = pendingCorrelation.Fd
			correlation.Cookie = pendingCorrelation.Cookie
		case CORRELATION_SOURCE_SYSCALL:
			correlation.SessionId = pendingCorrelation.SessionId
		}

		// first things first, we need to inform eBPF via the bridge that we have a correlation so they don't send any more data for this correlation
		if err := m.bridge.UprobeCorrelatedMap.Put(correlation.SessionId, true); err != nil {
			m.logger.Debug("failed to put uprobe correlated map", zap.Error(err))
		}
		if err := m.bridge.SyscallCorrelatedMap.Put(struct {
			Pid uint32
			Fd  int32
		}{
			Pid: correlation.Pid,
			Fd:  correlation.Fd,
		}, true); err != nil {
			m.logger.Debug("failed to put syscall correlated map", zap.Error(err))
		}

		// create a new active correlation
		m.activeCorrelations.Store(correlation.SessionId, correlation)

		// store the session id in the pid/fd map
		m.pidFdSessionIdMap.Store(generatePidFdKey(correlation.Pid, correlation.Fd), correlation.SessionId)

		// load the pending correlations by source(both session id and pid/fd key) and remove all of the pending correlations
		m.removePendingCorrelationsBySource(correlation.SessionId)
		m.removePendingCorrelationsBySource(generatePidFdKey(correlation.Pid, correlation.Fd))

		// remove the pending correlation
		m.pendingCorrelations.Delete(hash)

		// grab the pending plaintext messages for this session
		pendingMessages, ok := m.pendingMessages.Load(correlation.SessionId)
		if ok && len(pendingMessages) > 0 {
			// create a new reader around the first message
			r := bytes.NewReader(pendingMessages[0].Msg)

			// wrap the reader in a peeker
			peeker := bufio.NewReader(r)

			// detect the protocol
			l7Protocol, err := l7detect.DetectProtocol(m.logger, peeker)
			if err != nil {
				m.logger.Debug("failed to detect protocol", zap.Error(err))
			}

			// determine if we can stream this protocol
			canStream := false

			// is this protocol in the list of available stream protocols?
			if _, ok := m.streamProtocols.Load(l7Protocol.String()); ok {
				canStream = true
			}

			if !canStream {
				// tell the bridge to ignore this session
				if err := m.bridge.SessionIgnoreMap.Put(correlation.SessionId, true); err != nil {
					m.logger.Debug("failed to put session ignore map", zap.Error(err))
				}
			} else {
				// write the protocol event
				if err := m.connEvents.WriteProtocolEvent(correlation.Cookie, l7Protocol, true); err != nil {
					m.logger.Debug("failed to write Java SSLEngine protocol event to connection events", zap.Error(err))
				}

				// iterate over the pending messages and write them to the connection events
				for _, msg := range pendingMessages {
					if err := m.connEvents.WriteDataEvent(correlation.Cookie, msg.Direction.ToConnectionDirection(), msg.Msg); err != nil {
						m.logger.Debug("failed to write Java SSLEngine plaintext data to connection events", zap.Error(err))
					}
				}
			}

			// remove the pending messages
			m.pendingMessages.Delete(correlation.SessionId)
		}
	} else {
		// add the pending correlation
		m.pendingCorrelations.Store(hash, correlation)

		// generate a key for the pending correlation by source
		key := uint64(0)
		switch source {
		case CORRELATION_SOURCE_UPROBE:
			key = correlation.SessionId
		case CORRELATION_SOURCE_SYSCALL:
			key = generatePidFdKey(correlation.Pid, correlation.Fd)
		}

		// do we have a pending correlations by source for this key?
		pendingCorrelationsBySource, ok := m.pendingCorrelationsBySource.Load(key)
		if ok {
			// add the pending correlation to the pending correlations by source
			pendingCorrelationsBySource = append(pendingCorrelationsBySource, hash)
		} else {
			// create a new pending correlations by source
			pendingCorrelationsBySource = []uint64{hash}
		}

		// persist the pending correlations by source
		m.pendingCorrelationsBySource.Store(key, pendingCorrelationsBySource)
	}

	// add to the pid meta for proper cleanup
	switch source {
	case CORRELATION_SOURCE_UPROBE:
		m.addSessionIdToPidMeta(correlation.Pid, correlation.SessionId)
	case CORRELATION_SOURCE_SYSCALL:
		m.addPidFdToPidMeta(correlation.Pid, correlation.Fd)
	}

	return nil
}

func (m *SslEngineManager) removePendingCorrelationsBySource(key uint64) {
	pendingCorrelationsBySource, ok := m.pendingCorrelationsBySource.Load(key)
	if ok {
		for _, hash := range pendingCorrelationsBySource {
			m.pendingCorrelations.Delete(hash)
		}

		m.pendingCorrelationsBySource.Delete(key)
	}
}

func (m *SslEngineManager) addSessionIdToPidMeta(pid uint32, sessionId uint64) {
	meta := m.getPidMeta(pid)

	// already in the list
	if slices.Contains(meta.SessionIds, sessionId) {
		return
	}

	meta.SessionIds = append(meta.SessionIds, sessionId)
	m.pidMeta.Store(pid, meta)
}

func (m *SslEngineManager) removeSessionIdFromPidMeta(pid uint32, sessionId uint64) {
	meta := m.getPidMeta(pid)

	// not in the list
	if !slices.Contains(meta.SessionIds, sessionId) {
		return
	}

	meta.SessionIds = slices.DeleteFunc(meta.SessionIds, func(id uint64) bool {
		return id == sessionId
	})
	m.pidMeta.Store(pid, meta)
}

func (m *SslEngineManager) addPidFdToPidMeta(pid uint32, fd int32) {
	meta := m.getPidMeta(pid)

	// generate a key for the pid/fd
	key := generatePidFdKey(pid, fd)

	// already in the list
	if slices.Contains(meta.PidFds, key) {
		return
	}

	meta.PidFds = append(meta.PidFds, key)
	m.pidMeta.Store(pid, meta)
}

func (m *SslEngineManager) removePidFdFromPidMeta(pid uint32, fd int32) {
	meta := m.getPidMeta(pid)

	// generate a key for the pid/fd
	key := generatePidFdKey(pid, fd)

	// not in the list
	if !slices.Contains(meta.PidFds, key) {
		return
	}

	meta.PidFds = slices.DeleteFunc(meta.PidFds, func(id uint64) bool {
		return id == key
	})
	m.pidMeta.Store(pid, meta)
}

func (m *SslEngineManager) clearSessionIgnored(sessionId uint64) {
	// first check if this session is ignored
	isIgnored := false
	if err := m.bridge.SessionIgnoreMap.Lookup(sessionId, &isIgnored); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
		m.logger.Debug("failed to lookup session ignore map", zap.Error(err))
	}
	if !isIgnored {
		return
	}

	// remove the session from the ignored map
	if err := m.bridge.SessionIgnoreMap.Delete(sessionId); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
		m.logger.Debug("failed to delete session ignore map", zap.Error(err))
	}
}

func (m *SslEngineManager) clearUprobeCorrelated(sessionId uint64) {
	// first check if this session is correlated
	correlated := false
	if err := m.bridge.UprobeCorrelatedMap.Lookup(sessionId, &correlated); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
		m.logger.Debug("failed to lookup uprobe correlated map", zap.Error(err))
	}
	if !correlated {
		return
	}

	// remove the session from the correlated map
	if err := m.bridge.UprobeCorrelatedMap.Delete(sessionId); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
		m.logger.Debug("failed to delete uprobe correlated map", zap.Error(err))
	}
}

func (m *SslEngineManager) clearSyscallCorrelated(pid uint32, fd int32) {
	// create a key for the pid/fd
	key := struct {
		Pid uint32
		Fd  int32
	}{Pid: pid, Fd: fd}

	// first check if this pid/fd is correlated
	correlated := false
	if err := m.bridge.SyscallCorrelatedMap.Lookup(key, &correlated); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
		m.logger.Debug("failed to lookup syscall correlated map", zap.Error(err))
	}
	if !correlated {
		return
	}

	// remove the pid/fd from the correlated map
	if err := m.bridge.SyscallCorrelatedMap.Delete(key); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
		m.logger.Debug("failed to delete syscall correlated map", zap.Error(err))
	}
}

func (m *SslEngineManager) cleanAllFromPid(pid uint32) {
	meta, ok := m.pidMeta.Load(pid)
	if !ok {
		return
	}

	// cleanup the sessions
	for _, sessionId := range meta.SessionIds {
		m.activeCorrelations.Delete(sessionId)

		// remove the pending correlations by source
		m.removePendingCorrelationsBySource(sessionId)

		// remove the pending messages
		m.pendingMessages.Delete(sessionId)

		// clear the session ignored
		m.clearSessionIgnored(sessionId)

		// clear the uprobe correlated
		m.clearUprobeCorrelated(sessionId)
	}

	// cleanup the pid/fd
	for _, pidFd := range meta.PidFds {
		m.removePendingCorrelationsBySource(pidFd)

		// remove the session id from the pid/fd map
		m.pidFdSessionIdMap.Delete(pidFd)

		// clear the syscall correlated
		m.clearSyscallCorrelated(extractPidFdFromKey(pidFd))
	}

	// cleanup the pid meta
	m.pidMeta.Delete(pid)
}

func (m *SslEngineManager) getPidMeta(pid uint32) *pidMeta {
	meta, ok := m.pidMeta.Load(pid)
	if ok {
		return meta
	}

	// create a new pid meta
	return &pidMeta{
		SessionIds: []uint64{},
		PidFds:     []uint64{},
	}
}

func (m *SslEngineManager) expirePendingCorrelations() {
	// start a ticker to expire pending correlations every 10 seconds
	m.pendingCorrelationExpirationTicker = time.NewTicker(10 * time.Second)

	// run expirations in the background
	go func() {
		for range m.pendingCorrelationExpirationTicker.C {
			// create a now timestamp
			now := time.Now()

			// iterate over the pending correlations and expire them if they are older than the threshold
			m.pendingCorrelations.Iter(func(key uint64, value *correlation) bool {
				if now.Sub(value.Timestamp) > 10*time.Second {
					m.expirePendingCorrelation(key, value)
				}
				return true
			})
		}
	}()
}

func (m *SslEngineManager) expirePendingCorrelation(key uint64, c *correlation) {
	// if this pending correlation has a pid and fd, remove the pending correlations
	if c.Pid != 0 && c.Fd != -1 {
		m.removePendingCorrelationsBySource(generatePidFdKey(c.Pid, c.Fd))
	}

	// pending correlation has a session id
	if c.SessionId != 0 {
		// remove the pending correlations by source
		m.removePendingCorrelationsBySource(c.SessionId)

		// remove any pending plaintext messages for this session
		m.pendingMessages.Delete(c.SessionId)
	}

	// finally, remove the pending correlation
	m.pendingCorrelations.Delete(key)
}

// generate a prefix hash from the first 16 bytes of a []byte
func generatePrefixHash(data []byte) uint64 {
	return xxhash.Sum64(data[:16])
}

// creates a composite key from pid and fd
func generatePidFdKey(pid uint32, fd int32) uint64 {
	return uint64(pid)<<32 | uint64(uint32(fd))
}

func extractPidFdFromKey(key uint64) (uint32, int32) {
	return uint32(key >> 32), int32(key & 0xFFFFFFFF)
}
