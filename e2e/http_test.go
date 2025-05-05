//go:build e2e

package e2e

import (
	"testing"

	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTP(t *testing.T) {
	ctx := e2ectx.TestCtx(t)

	// exec a process that makes an http request
	example := ctx.exec("curl", "https://example.com")
	require.NoError(t, example.err)

	// ensure we captured the connection
	require.Len(t, example.events().Connections, 1)
	conn := example.events().Connections[0]
	assert.Equal(t, eventstore.SocketProtocol_TCP, conn.SocketProtocol)
}
