package openssl

import (
	"testing"

	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestOpenSSLManager(t *testing.T) {
	// test for panics while setting up metrics
	m := NewOpenSSLManager(zap.NewNop(), func() []*common.Uprobe {
		return []*common.Uprobe{
			common.NewUprobe("SSL_read", nil),
			common.NewUprobe("SSL_write", nil),
		}
	})
	require.NoError(t, m.Start())
}
