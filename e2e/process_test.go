//go:build e2e

package e2e

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessFiltering(t *testing.T) {
	ctx := e2ectx.TestCtx(t)

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(targetServer.Close)

	// should not be filtered out by default
	ctx.WithConfig(t, func(c *config.Config) {
		c.Tap.IgnoreLoopback = false
		c.Tap.Direction = config.TrafficDirection_EGRESS
	}, func(t *testing.T) {
		// exec a process that makes an http request
		example := ctx.Exec("curl", targetServer.URL)
		require.NoError(t, example.Err)
		require.Equal(t, 0, example.Code)
		require.NotEmpty(t, example.Output)

		// ensure we captured the connection
		events := example.AwaitEvents(1)
		conn := events.Connections[0]
		assert.Equal(t, eventstore.SocketProtocol_TCP, conn.SocketProtocol)
	})

	// should be filtered out by the custom filter
	ctx.WithConfig(t, func(c *config.Config) {
		c.Tap.IgnoreLoopback = false
		c.Tap.Direction = config.TrafficDirection_EGRESS
		c.Tap.Filters.Custom = append(c.Tap.Filters.Custom, config.TapFilter{
			Exe:      "/curl",
			Strategy: config.MatchStrategy_SUFFIX,
		})
	}, func(t *testing.T) {
		example := ctx.Exec("curl", targetServer.URL)
		require.NoError(t, example.Err)
		require.Equal(t, 0, example.Code)
		require.NotEmpty(t, example.Output)

		// expect to timeout because we should never capture the connection
		events, err := e2ectx.EventStore.AwaitByCtxID(example.ID, 1, 3*time.Second)
		require.NotNil(t, err)
		require.Nil(t, events)
	})
}
