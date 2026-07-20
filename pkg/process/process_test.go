package process

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSetTlsOkRollsBackWhenNotifierFails(t *testing.T) {
	notifyErr := errors.New("map update failed")
	p := NewProcess(42, "", zap.NewNop())
	p.tlsOk = true
	p.notifier = func() error { return notifyErr }

	require.ErrorIs(t, p.SetTlsOk(false), notifyErr)
	require.True(t, p.TlsOk(), "userspace state must remain retryable when the kernel update fails")

	p.notifier = nil
	require.NoError(t, p.SetTlsOk(false))
	require.False(t, p.TlsOk())
}
