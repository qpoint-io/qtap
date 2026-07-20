//go:build linux

package egress

import (
	"errors"
	"net"
	"strconv"
	"testing"

	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestEgressManagerCertRemovedRevokesTLSOk(t *testing.T) {
	manager := NewEgressManager(nil, zap.NewNop(), nil, TLSOkStrategyOnCertInject)
	p := process.NewProcess(42, "", zap.NewNop())
	require.NoError(t, p.SetTlsOk(true))

	require.NoError(t, manager.CertRemoved(p, "/ca.pem", 1))
	require.False(t, p.TlsOk())
}

func TestEgressManager_StartRollsBackForwarderWhenRouterStartFails(t *testing.T) {
	startErr := errors.New("router start")
	router := &lifecycleRouter{startErr: startErr}
	manager := NewEgressManager(nil, zap.NewNop(), router, TLSOkStrategyOnCertInject)
	t.Cleanup(func() { _ = manager.Stop() })

	var forwarderAddr string
	router.onStart = func() {
		require.NotNil(t, manager.forwarder)
		addr := manager.forwarder.listener4.Addr().(*net.TCPAddr)
		forwarderAddr = net.JoinHostPort("127.0.0.1", strconv.Itoa(addr.Port))
	}

	err := manager.Start()
	require.ErrorIs(t, err, startErr)
	require.Equal(t, 1, router.stopCalls)
	require.NotEmpty(t, forwarderAddr)

	conn, err := net.Dial("tcp4", forwarderAddr)
	if err == nil {
		_ = conn.Close()
	}
	require.Error(t, err, "the forwarder must be stopped when router startup fails")
}

func TestEgressManager_StopRetriesRouterRollbackAfterStartFailure(t *testing.T) {
	startErr := errors.New("router start")
	rollbackErr := errors.New("router rollback")
	router := &lifecycleRouter{
		startErr:   startErr,
		stopErrors: []error{rollbackErr, nil},
	}
	manager := NewEgressManager(nil, zap.NewNop(), router, TLSOkStrategyOnCertInject)

	err := manager.Start()
	require.ErrorIs(t, err, startErr)
	require.ErrorIs(t, err, rollbackErr)
	require.Equal(t, 1, router.stopCalls)

	require.NoError(t, manager.Stop())
	require.Equal(t, 2, router.stopCalls)
}

func TestEgressManager_StopHandlesRouterOnlyState(t *testing.T) {
	router := &lifecycleRouter{}
	manager := NewEgressManager(nil, zap.NewNop(), router, TLSOkStrategyOnCertInject)

	require.NoError(t, manager.Stop())
	require.Equal(t, 1, router.stopCalls)
}

func TestEgressManager_StopHandlesForwarderOnlyState(t *testing.T) {
	forwarder, err := NewForwarder(t.Context(), zap.NewNop(), "", "", nil, nil)
	require.NoError(t, err)
	require.NoError(t, forwarder.Start())
	manager := NewEgressManager(nil, zap.NewNop(), nil, TLSOkStrategyOnCertInject)
	manager.forwarder = forwarder

	require.NoError(t, manager.Stop())
	require.True(t, forwarder.stopped)
}

func TestEgressManager_StopHandlesNilReceiver(t *testing.T) {
	var manager *EgressManager
	require.NoError(t, manager.Stop())
}

func TestEgressManager_StopIsIdempotent(t *testing.T) {
	router := &lifecycleRouter{}
	manager := NewEgressManager(nil, zap.NewNop(), router, TLSOkStrategyOnCertInject)

	require.NoError(t, manager.Stop())
	require.NoError(t, manager.Stop())
	require.Equal(t, 1, router.stopCalls)
}

func TestEgressManager_StopRetriesFailedCleanup(t *testing.T) {
	stopErr := errors.New("router stop")
	router := &lifecycleRouter{stopErrors: []error{stopErr, nil}}
	manager := NewEgressManager(nil, zap.NewNop(), router, TLSOkStrategyOnCertInject)

	require.ErrorIs(t, manager.Stop(), stopErr)
	require.NoError(t, manager.Stop())
	require.NoError(t, manager.Stop())
	require.Equal(t, 2, router.stopCalls)
}

func TestEgressManager_StartIsIdempotent(t *testing.T) {
	router := &lifecycleRouter{}
	manager := NewEgressManager(nil, zap.NewNop(), router, TLSOkStrategyOnCertInject)
	var forwarders []*Forwarder
	router.onStart = func() { forwarders = append(forwarders, manager.forwarder) }
	t.Cleanup(func() {
		_ = manager.Stop()
		for _, forwarder := range forwarders {
			_ = forwarder.Stop()
		}
	})

	require.NoError(t, manager.Start())
	require.NoError(t, manager.Start())
	require.Equal(t, 1, router.startCalls)
	require.NoError(t, manager.Stop())
	require.Error(t, manager.Start())
}

func TestEgressManager_StopKeepsForwarderWhenRouterDetachFails(t *testing.T) {
	forwarderErr := errors.New("forwarder stop")
	routerErr := errors.New("router stop")
	forwarder, err := NewForwarder(t.Context(), zap.NewNop(), "", "", nil, nil)
	require.NoError(t, err)
	listener := &closeErrorListener{err: forwarderErr}
	forwarder.listener4 = listener
	forwarder.started = true
	router := &lifecycleRouter{stopErr: routerErr}
	manager := NewEgressManager(nil, zap.NewNop(), router, TLSOkStrategyOnCertInject)
	manager.forwarder = forwarder

	err = manager.Stop()
	require.ErrorIs(t, err, routerErr)
	require.Equal(t, 1, router.stopCalls)
	require.Zero(t, listener.closeCalls, "forwarder must remain available while routing may still redirect traffic")
}

type lifecycleRouter struct {
	startErr   error
	stopErr    error
	stopErrors []error
	onStart    func()
	startCalls int
	stopCalls  int
}

func (r *lifecycleRouter) SetMgmtAddrs(net.IP, net.IP, int) error { return nil }

func (r *lifecycleRouter) Start() error {
	r.startCalls++
	if r.onStart != nil {
		r.onStart()
	}
	return r.startErr
}

func (r *lifecycleRouter) Stop() error {
	r.stopCalls++
	if r.stopCalls <= len(r.stopErrors) {
		return r.stopErrors[r.stopCalls-1]
	}
	return r.stopErr
}
