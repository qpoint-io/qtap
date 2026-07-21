package status

import (
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestServeReportsUnexpectedErrors(t *testing.T) {
	expected := errors.New("accept failed")
	statusServer := NewBaseStatusServer("127.0.0.1:0", zap.NewNop(), func() bool { return true })
	require.Equal(t, 1, cap(statusServer.Errors()))

	statusServer.serve(&http.Server{}, errorListener{err: expected})

	select {
	case err := <-statusServer.Errors():
		require.ErrorIs(t, err, expected)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for serve error")
	}
}

type errorListener struct {
	err error
}

func (l errorListener) Accept() (net.Conn, error) { return nil, l.err }
func (errorListener) Close() error                { return nil }
func (errorListener) Addr() net.Addr              { return &net.TCPAddr{} }
