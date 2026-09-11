package process

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHandleExecStartEvent(t *testing.T) {
	tests := []struct {
		name     string
		pid      int32
		exePath  string
		wantErr  bool
		receiver bool
	}{
		{
			name:     "successful exec start",
			pid:      1234,
			exePath:  "/usr/bin/test",
			wantErr:  false,
			receiver: true,
		},
		{
			name:     "exec start without receiver",
			pid:      5678,
			exePath:  "/usr/bin/test2",
			wantErr:  false,
			receiver: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test event data
			event := execStartEvent{
				Pid:     tt.pid,
				ExeSize: uint32(len(tt.exePath) + 1),
			}

			// Create buffer with event data
			buf := new(bytes.Buffer)
			require.NoError(t, binary.Write(buf, binary.NativeEndian, event))
			require.NoError(t, binary.Write(buf, binary.NativeEndian, []byte(tt.exePath)))

			cache, err := lru.New[int32, *Process](cacheSize)
			if err != nil {
				panic(err)
			}
			// Create manager
			m := &eventSource{
				logger: zap.NewNop(),
				cache:  cache,
			}

			if tt.receiver {
				mockRcv := &testReceiver{t: t}
				m.reciever = mockRcv
			}

			// Test handler
			err = m.handleExecStartEvent(t.Context(), bytes.NewReader(buf.Bytes()))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.receiver {
				// Verify process was added to cache
				proc, exists := m.cache.Get(tt.pid)
				require.True(t, exists)
				require.Equal(t, tt.exePath, proc.ExeFilename)
			}
		})
	}
}

func TestHandleExecArgvEvent(t *testing.T) {
	tests := []struct {
		name     string
		pid      int32
		arg      string
		setupPid bool
		wantErr  bool
	}{
		{
			name:     "add argument to existing process",
			pid:      1234,
			arg:      "--test-flag",
			setupPid: true,
			wantErr:  false,
		},
		{
			name:     "process not in cache",
			pid:      5678,
			arg:      "--missing",
			setupPid: false,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test event data
			event := execArgvEvent{
				Pid:      tt.pid,
				ArgvSize: uint32(len(tt.arg) + 1),
			}

			// Create buffer with event data
			buf := new(bytes.Buffer)
			require.NoError(t, binary.Write(buf, binary.NativeEndian, event))
			require.NoError(t, binary.Write(buf, binary.NativeEndian, []byte(tt.arg)))

			cache, err := lru.New[int32, *Process](cacheSize)
			if err != nil {
				panic(err)
			}

			// Create manager with receiver
			m := &eventSource{
				logger:   zap.NewNop(),
				cache:    cache,
				reciever: &testReceiver{t: t},
			}

			// Setup process in cache if needed
			if tt.setupPid {
				p := NewProcess(int(tt.pid), "/test/exe", zap.NewNop())
				p.Args = make([]string, 0) // Initialize Args slice
				m.cache.Add(tt.pid, p)
			}

			// Test handler
			err = m.handleExecArgvEvent(t.Context(), bytes.NewReader(buf.Bytes()))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.setupPid {
				// Verify argument was added
				proc, exists := m.cache.Get(tt.pid)
				require.True(t, exists)
				require.Contains(t, proc.Args, tt.arg)
			}
		})
	}
}

func TestHandleExecEndEvent(t *testing.T) {
	tests := []struct {
		name       string
		pid        int32
		setupPid   bool
		wantErr    bool
		addProcErr error
	}{
		{
			name:     "successful exec end",
			pid:      1234,
			setupPid: true,
			wantErr:  false,
		},
		{
			name:     "process not in cache",
			pid:      5678,
			setupPid: false,
			wantErr:  false,
		},
		{
			name:       "receiver error",
			pid:        1234,
			setupPid:   true,
			wantErr:    false,
			addProcErr: bytes.ErrTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test event data
			event := execEndEvent{
				Pid: tt.pid,
			}

			// Create buffer with event data
			buf := new(bytes.Buffer)
			require.NoError(t, binary.Write(buf, binary.NativeEndian, event))

			cache, err := lru.New[int32, *Process](cacheSize)
			if err != nil {
				panic(err)
			}

			// Expect one registration only when the process is cached.
			registered := 0
			mockRcv := &testReceiver{t: t}
			if tt.setupPid {
				mockRcv.register = func(p *Process) error {
					registered++
					require.Equal(t, int(tt.pid), p.Pid)
					return tt.addProcErr
				}
			}

			// Create manager
			m := &eventSource{
				logger:   zap.NewNop(),
				cache:    cache,
				reciever: mockRcv,
			}

			// Setup process in cache if needed
			if tt.setupPid {
				m.cache.Add(tt.pid, NewProcess(int(tt.pid), "/test/exe", zap.NewNop()))
			}

			// Test handler
			err = m.handleExecEndEvent(t.Context(), bytes.NewReader(buf.Bytes()))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.setupPid {
				require.Equal(t, 1, registered)
				// Verify process was removed from cache
				_, exists := m.cache.Get(tt.pid)
				require.False(t, exists)
			}
		})
	}
}

func TestHandleExitEvent(t *testing.T) {
	tests := []struct {
		name       string
		pid        int32
		wantErr    bool
		endProcErr error
	}{
		{
			name:    "successful exit",
			pid:     1234,
			wantErr: false,
		},
		{
			name:       "receiver error",
			pid:        5678,
			wantErr:    false,
			endProcErr: bytes.ErrTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test event data
			event := exitEvent{
				Pid: tt.pid,
			}

			// Create buffer with event data
			buf := new(bytes.Buffer)
			require.NoError(t, binary.Write(buf, binary.NativeEndian, event))

			cache, err := lru.New[int32, *Process](cacheSize)
			if err != nil {
				panic(err)
			}

			unregistered := 0
			mockRcv := &testReceiver{
				t: t,
				unregister: func(pid, exitCode int) error {
					unregistered++
					require.Equal(t, int(tt.pid), pid)
					require.Zero(t, exitCode)
					return tt.endProcErr
				},
			}

			// Create manager
			m := &eventSource{
				logger:   zap.NewNop(),
				cache:    cache,
				reciever: mockRcv,
			}

			// Test handler
			err = m.handleExitEvent(t.Context(), bytes.NewReader(buf.Bytes()))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, unregistered)
		})
	}
}

// testReceiver rejects unexpected calls without importing mocks that depend on
// this package. Each test checks its expected calls after handling the event.
type testReceiver struct {
	t          *testing.T
	register   func(*Process) error
	unregister func(pid, exitCode int) error
}

func (r *testReceiver) RegisterProcess(_ context.Context, p *Process) error {
	r.t.Helper()
	require.NotNil(r.t, r.register, "unexpected process registration")
	return r.register(p)
}

func (r *testReceiver) UnregisterProcess(_ context.Context, pid, exitCode int) error {
	r.t.Helper()
	require.NotNil(r.t, r.unregister, "unexpected process unregistration")
	return r.unregister(pid, exitCode)
}
