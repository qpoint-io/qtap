package connection

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/process"
	servicespkg "github.com/qpoint-io/qtap/pkg/services"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const shutdownTestServiceType servicespkg.ServiceType = "shutdown-test"

type shutdownFinalReport struct {
	processed    int64
	dataEvents   uint64
	streamClosed bool
	naturalClose bool
}

type shutdownTestService struct {
	conn       *Connection
	processor  *shutdownTestProcessor
	closeStart chan struct{}
	closeBlock <-chan struct{}
	reports    chan shutdownFinalReport
	closeOnce  sync.Once
}

func (s *shutdownTestService) SetConnection(conn *Connection) {
	s.conn = conn
}

func (s *shutdownTestService) ServiceType() servicespkg.ServiceType {
	return shutdownTestServiceType
}

func (s *shutdownTestService) Close() error {
	s.closeOnce.Do(func() {
		if s.closeStart != nil {
			close(s.closeStart)
		}
		if s.closeBlock != nil {
			<-s.closeBlock
		}
		s.reports <- shutdownFinalReport{
			processed:    s.processor.processed.Load(),
			dataEvents:   s.conn.DataEventCount(),
			streamClosed: s.processor.Closed(),
			naturalClose: s.conn.CloseEvent != nil,
		}
	})
	return nil
}

type shutdownTestProcessor struct {
	firstStarted chan struct{}
	firstBlock   <-chan struct{}
	processed    atomic.Int64
	closed       atomic.Bool
	startOnce    sync.Once
}

func (p *shutdownTestProcessor) Process(_ *DataEvent) error {
	p.processed.Add(1)
	p.startOnce.Do(func() {
		if p.firstStarted != nil {
			close(p.firstStarted)
		}
		if p.firstBlock != nil {
			<-p.firstBlock
		}
	})
	return nil
}

func (p *shutdownTestProcessor) Close() {
	p.closed.Store(true)
}

func (p *shutdownTestProcessor) Closed() bool {
	return p.closed.Load()
}

func newShutdownTestConnection(t *testing.T, manager *Manager, cookie Cookie, processor *shutdownTestProcessor, service *shutdownTestService) *Connection {
	t.Helper()

	registry := servicespkg.NewFactoryRegistry()
	registry.Register(servicespkg.StaticFactory(shutdownTestServiceType, service), "")
	open := OpenEvent{Cookie: cookie, Source: Client, SocketType: SocketType_TCP}
	conn := NewConnection(t.Context(), zaptest.NewLogger(t), &open, WithServices(manager), WithServiceFactoryRegistry(registry))
	conn.process = &process.Process{}
	conn.streamProcessor = processor
	conn.reportEvent(open)
	service.processor = processor
	_, err := servicespkg.GetService[servicespkg.Service](t.Context(), conn.ServiceRegistry(), shutdownTestServiceType, "")
	require.NoError(t, err)

	manager.connections.Store(cookie, conn)
	manager.trackConnection(conn)
	conn.Open()
	return conn
}

func waitForShutdownAdmissionBarrier(t *testing.T, manager *Manager) {
	t.Helper()
	require.Eventually(t, func() bool {
		manager.admissionMu.RLock()
		defer manager.admissionMu.RUnlock()
		return manager.stopping
	}, time.Second, time.Millisecond)
}

func TestManagerShutdownDrainsBeforeFinalReportAndRejectsNewEvents(t *testing.T) {
	manager := NewManager(zaptest.NewLogger(t))
	firstStarted := make(chan struct{})
	firstBlock := make(chan struct{})
	processor := &shutdownTestProcessor{firstStarted: firstStarted, firstBlock: firstBlock}
	service := &shutdownTestService{reports: make(chan shutdownFinalReport, 1)}
	conn := newShutdownTestConnection(t, manager, 1, processor, service)

	manager.HandleEvent(DataEvent{Cookie: 1, Data: []byte("first")})
	<-firstStarted
	manager.HandleEvent(DataEvent{Cookie: 1, Data: []byte("queued")})

	shutdownErr := make(chan error, 1)
	go func() { shutdownErr <- manager.Shutdown(t.Context()) }()
	waitForShutdownAdmissionBarrier(t, manager)

	manager.HandleEvent(DataEvent{Cookie: 1, Data: []byte("rejected")})
	manager.HandleEvent(OpenEvent{Cookie: 2})
	close(firstBlock)

	require.NoError(t, <-shutdownErr)
	report := <-service.reports
	require.Equal(t, int64(2), report.processed)
	require.Equal(t, uint64(2), report.dataEvents)
	require.True(t, report.streamClosed)
	require.False(t, report.naturalClose)
	require.True(t, processor.Closed())
	require.NotContains(t, manager.connections.Copy(), Cookie(2))
	require.Empty(t, manager.active)
	select {
	case <-conn.closed:
	default:
		t.Fatal("active connection did not finish before shutdown returned")
	}
	require.NoError(t, manager.Shutdown(t.Context()))
}

func TestManagerShutdownRacesNaturalClose(t *testing.T) {
	manager := NewManager(zaptest.NewLogger(t))
	firstStarted := make(chan struct{})
	firstBlock := make(chan struct{})
	processor := &shutdownTestProcessor{firstStarted: firstStarted, firstBlock: firstBlock}
	service := &shutdownTestService{reports: make(chan shutdownFinalReport, 1)}
	newShutdownTestConnection(t, manager, 1, processor, service)

	manager.HandleEvent(DataEvent{Cookie: 1, Data: []byte("first")})
	<-firstStarted
	manager.HandleEvent(DataEvent{Cookie: 1, Data: []byte("queued")})
	manager.HandleEvent(CloseEvent{Cookie: 1, WrBytes: 10, RdBytes: 20})

	shutdownErr := make(chan error, 1)
	go func() { shutdownErr <- manager.Shutdown(t.Context()) }()
	waitForShutdownAdmissionBarrier(t, manager)
	close(firstBlock)

	require.NoError(t, <-shutdownErr)
	report := <-service.reports
	require.Equal(t, int64(2), report.processed)
	require.Equal(t, uint64(2), report.dataEvents)
	require.True(t, report.naturalClose)
	require.True(t, report.streamClosed)
}

func TestManagerShutdownDeadline(t *testing.T) {
	manager := NewManager(zaptest.NewLogger(t))
	processor := &shutdownTestProcessor{}
	closeStart := make(chan struct{})
	closeBlock := make(chan struct{})
	service := &shutdownTestService{
		closeStart: closeStart,
		closeBlock: closeBlock,
		reports:    make(chan shutdownFinalReport, 1),
	}
	conn := newShutdownTestConnection(t, manager, 1, processor, service)
	released := false
	defer func() {
		if !released {
			close(closeBlock)
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	err := manager.Shutdown(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	select {
	case <-closeStart:
	default:
		t.Fatal("service closure did not start before the shutdown deadline")
	}
	select {
	case <-conn.closed:
		t.Fatal("connection finished while its service was still closing")
	default:
	}

	close(closeBlock)
	released = true
	retryCtx, retryCancel := context.WithTimeout(t.Context(), time.Second)
	defer retryCancel()
	require.NoError(t, manager.Shutdown(retryCtx))
}
