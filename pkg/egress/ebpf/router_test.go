package ebpf

import (
	"errors"
	"testing"

	"github.com/cilium/ebpf/link"
	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/stretchr/testify/require"
)

func TestRouter_StartRollsBackAttachedLinks(t *testing.T) {
	attachErr := errors.New("attach IPv6")
	connect4 := &stubLink{}
	attachCalls := 0
	router := &Router{
		objs: &tap.TapObjects{},
		attachCgroup: func(link.CgroupOptions) (attachedLink, error) {
			attachCalls++
			if attachCalls == 1 {
				return connect4, nil
			}
			return nil, attachErr
		},
	}

	err := router.Start()
	require.ErrorIs(t, err, attachErr)
	require.Equal(t, 1, connect4.closeCalls)
	require.Empty(t, router.links)
}

func TestRouter_StartRollbackFailureIsTerminal(t *testing.T) {
	attachErr := errors.New("attach sockops")
	rollbackErr1 := errors.New("rollback IPv4")
	rollbackErr2 := errors.New("rollback IPv6")
	connect4 := &stubLink{closeErrors: []error{rollbackErr1}}
	connect6 := &stubLink{closeErrors: []error{rollbackErr2}}
	attachCalls := 0
	router := &Router{
		objs: &tap.TapObjects{},
		attachCgroup: func(link.CgroupOptions) (attachedLink, error) {
			attachCalls++
			switch attachCalls {
			case 1:
				return connect4, nil
			case 2:
				return connect6, nil
			}
			return nil, attachErr
		},
	}

	err := router.Start()
	require.ErrorIs(t, err, attachErr)
	require.ErrorIs(t, err, rollbackErr1)
	require.ErrorIs(t, err, rollbackErr2)
	require.Equal(t, 1, connect4.closeCalls)
	require.Equal(t, 1, connect6.closeCalls)
	require.Empty(t, router.links)

	err = router.Stop()
	require.ErrorIs(t, err, rollbackErr1)
	require.ErrorIs(t, err, rollbackErr2)
	require.Equal(t, 1, connect4.closeCalls)
	require.Equal(t, 1, connect6.closeCalls)

	err = router.Start()
	require.ErrorIs(t, err, rollbackErr1)
	require.ErrorIs(t, err, rollbackErr2)
	require.Equal(t, 3, attachCalls)
	require.Equal(t, 1, connect4.closeCalls)
	require.Equal(t, 1, connect6.closeCalls)
}

func TestRouter_StopFailureIsTerminal(t *testing.T) {
	closeErr1 := errors.New("close link 1")
	closeErr2 := errors.New("close link 2")
	link1 := &stubLink{closeErrors: []error{closeErr1}}
	link2 := &stubLink{closeErrors: []error{closeErr2}}
	attachCalls := 0
	router := &Router{
		objs:  &tap.TapObjects{},
		links: []attachedLink{link1, link2},
		attachCgroup: func(link.CgroupOptions) (attachedLink, error) {
			attachCalls++
			return &stubLink{}, nil
		},
		started: true,
	}

	err := router.Stop()
	require.ErrorIs(t, err, closeErr1)
	require.ErrorIs(t, err, closeErr2)
	require.Equal(t, 1, link1.closeCalls)
	require.Equal(t, 1, link2.closeCalls)
	require.Empty(t, router.links)

	err = router.Stop()
	require.ErrorIs(t, err, closeErr1)
	require.ErrorIs(t, err, closeErr2)
	require.Equal(t, 1, link1.closeCalls)
	require.Equal(t, 1, link2.closeCalls)

	err = router.Start()
	require.ErrorIs(t, err, closeErr1)
	require.ErrorIs(t, err, closeErr2)
	require.Zero(t, attachCalls)
	require.Equal(t, 1, link1.closeCalls)
	require.Equal(t, 1, link2.closeCalls)
}

func TestRouter_SuccessfulStartStopIsIdempotent(t *testing.T) {
	links := []*stubLink{{}, {}, {}, {}}
	attachCalls := 0
	router := &Router{
		objs: &tap.TapObjects{},
		attachCgroup: func(link.CgroupOptions) (attachedLink, error) {
			attached := links[attachCalls]
			attachCalls++
			return attached, nil
		},
	}

	require.NoError(t, router.Start())
	require.NoError(t, router.Start())
	require.Equal(t, len(links), attachCalls)

	require.NoError(t, router.Stop())
	require.NoError(t, router.Stop())
	for _, attached := range links {
		require.Equal(t, 1, attached.closeCalls)
	}
}

type stubLink struct {
	closeErrors []error
	closeCalls  int
}

func (l *stubLink) Close() error {
	l.closeCalls++
	if l.closeCalls <= len(l.closeErrors) {
		return l.closeErrors[l.closeCalls-1]
	}
	return nil
}
