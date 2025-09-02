package http1

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBufferedReader_Read(t *testing.T) {
	tests := []struct {
		name        string
		writeData   [][]byte
		readSizes   []int
		closeAfter  int // number of reads after which to close
		expectData  [][]byte
		expectError string
	}{
		{
			name:       "simple read matches write",
			writeData:  [][]byte{[]byte("hello")},
			readSizes:  []int{5},
			expectData: [][]byte{[]byte("hello")},
		},
		{
			name:       "multiple writes single read",
			writeData:  [][]byte{[]byte("hello"), []byte(" world")},
			readSizes:  []int{11},
			expectData: [][]byte{[]byte("hello world")},
		},
		{
			name:       "single write multiple reads",
			writeData:  [][]byte{[]byte("hello world")},
			readSizes:  []int{5, 6},
			expectData: [][]byte{[]byte("hello"), []byte(" world")},
		},
		{
			name:        "read after close returns error",
			writeData:   [][]byte{[]byte("hello")},
			readSizes:   []int{5, 5},
			closeAfter:  1,
			expectData:  [][]byte{[]byte("hello")},
			expectError: io.ErrUnexpectedEOF.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewBufferedReader(t.Context())
			require.NotNil(t, r)

			// Write test data
			for _, data := range tt.writeData {
				n, err := r.Write(data)
				require.NoError(t, err)
				require.Equal(t, len(data), n)
			}

			// Read and verify data
			for i, size := range tt.readSizes {
				if tt.closeAfter > 0 && i == tt.closeAfter {
					require.NoError(t, r.Close())
				}

				buf := make([]byte, size)
				n, err := r.Read(buf)

				if i < len(tt.expectData) {
					require.Equal(t, len(tt.expectData[i]), n)
					assert.Equal(t, tt.expectData[i], buf[:n])
				}

				switch {
				case tt.expectError != "" && i >= tt.closeAfter:
					require.Error(t, err)
					assert.Contains(t, err.Error(), tt.expectError)
					return
				case errors.Is(err, io.EOF):
					require.Equal(t, 0, n)
					return
				default:
					require.NoError(t, err)
				}
			}
		})
	}
}

func TestBufferedReader_Write(t *testing.T) {
	tests := []struct {
		name        string
		writeData   []byte
		closed      bool
		expectN     int
		expectError string
	}{
		{
			name:      "write to open reader",
			writeData: []byte("hello"),
			expectN:   5,
		},
		{
			name:        "write to closed reader",
			writeData:   []byte("hello"),
			closed:      true,
			expectError: "closed pipe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewBufferedReader(t.Context())
			require.NotNil(t, r)

			if tt.closed {
				require.NoError(t, r.Close())
			}

			n, err := r.Write(tt.writeData)

			if tt.expectError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectN, n)
			}
		})
	}
}

func TestBufferedReader_BlockingRead(t *testing.T) {
	r := NewBufferedReader(t.Context())
	require.NotNil(t, r)

	done := make(chan struct{})
	go func() {
		buf := make([]byte, 5)
		n, err := r.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, []byte("hello"), buf[:n])
		close(done)
	}()

	// Give the goroutine time to block on read
	time.Sleep(100 * time.Millisecond)

	n, err := r.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("read did not unblock")
	}
}
