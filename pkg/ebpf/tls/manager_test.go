package tls

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestTlsManager(t *testing.T) {
	// test for panics while creating the metrics
	m := NewTlsManager(zap.NewNop())
	require.NoError(t, m.Start())
}
