package tls

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestTlsManager(t *testing.T) {
	require.NotPanics(t, func() {
		NewTlsManager(zap.NewNop(), NewTargetScanner(zap.NewNop(), nil))
	})
}
