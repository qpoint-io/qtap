package http1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSessionTransactionEndDoesNotSkipClose(t *testing.T) {
	s := NewSession(t.Context(), nil, "example.com", nil, nil)
	t.Cleanup(func() {
		_ = s.Close()
	})

	require.False(t, s.Closed())
	require.False(t, s.closed.Load())
	require.False(t, s.parser.state.closed.Load())

	s.OnTransactionEnd()

	require.True(t, s.Closed())
	require.False(t, s.closed.Load(), "transaction end should not mark teardown complete")
	require.False(t, s.parser.state.closed.Load(), "parser should remain open until Close runs")

	require.NoError(t, s.Close())
	require.True(t, s.closed.Load())
	require.True(t, s.parser.state.closed.Load())
}
