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

	ctx.WithConfig(t, nil, func(t *testing.T) {
		// exec a process that makes an http request
		example := ctx.Exec("curl", "http://example.com")
		require.NoError(t, example.Err)

		// ensure we captured the connection
		events := example.AwaitEvents(1)
		conn := events.Connections[0]
		assert.Equal(t, eventstore.SocketProtocol_TCP, conn.SocketProtocol)
	})
}
