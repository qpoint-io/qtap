package process

import (
	"bytes"
	"encoding/binary"
	"testing"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/process/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
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
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Create test event data
			event := execStartEvent{
				Pid:     tt.pid,
				ExeSize: uint32(len(tt.exePath) + 1),
			}

			// Create buffer with event data
			buf := new(bytes.Buffer)
			require.NoError(t, binary.Write(buf, binary.NativeEndian, event))
			require.NoError(t, binary.Write(buf, binary.NativeEndian, []byte(tt.exePath)))

			cache, err := lru.New[int32, *process.Process](cacheSize)
			if err != nil {
				panic(err)
			}
			// Create manager
			m := &Manager{
				logger: zap.NewNop(),
				cache:  cache,
			}

			if tt.receiver {
				mockRcv := mocks.NewMockReceiver(ctrl)
				m.reciever = mockRcv
			}

			// Test handler
			err = m.handleExecStartEvent(bytes.NewReader(buf.Bytes()))
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
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Create test event data
			event := execArgvEvent{
				Pid:      tt.pid,
				ArgvSize: uint32(len(tt.arg) + 1),
			}

			// Create buffer with event data
			buf := new(bytes.Buffer)
			require.NoError(t, binary.Write(buf, binary.NativeEndian, event))
			require.NoError(t, binary.Write(buf, binary.NativeEndian, []byte(tt.arg)))

			cache, err := lru.New[int32, *process.Process](cacheSize)
			if err != nil {
				panic(err)
			}

			// Create manager with receiver
			m := &Manager{
				logger:   zap.NewNop(),
				cache:    cache,
				reciever: mocks.NewMockReceiver(ctrl),
			}

			// Setup process in cache if needed
			if tt.setupPid {
				p := process.NewProcess(int(tt.pid), "/test/exe", zap.NewNop())
				p.Args = make([]string, 0) // Initialize Args slice
				m.cache.Add(tt.pid, p)
			}

			// Test handler
			err = m.handleExecArgvEvent(bytes.NewReader(buf.Bytes()))
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
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Create test event data
			event := execEndEvent{
				Pid: tt.pid,
			}

			// Create buffer with event data
			buf := new(bytes.Buffer)
			require.NoError(t, binary.Write(buf, binary.NativeEndian, event))

			cache, err := lru.New[int32, *process.Process](cacheSize)
			if err != nil {
				panic(err)
			}

			// Create mock receiver
			mockRcv := mocks.NewMockReceiver(ctrl)
			if tt.setupPid {
				mockRcv.EXPECT().RegisterProcess(gomock.Any()).Return(tt.addProcErr)
			}

			// Create manager
			m := &Manager{
				logger:   zap.NewNop(),
				cache:    cache,
				reciever: mockRcv,
			}

			// Setup process in cache if needed
			if tt.setupPid {
				m.cache.Add(tt.pid, process.NewProcess(int(tt.pid), "/test/exe", zap.NewNop()))
			}

			// Test handler
			err = m.handleExecEndEvent(bytes.NewReader(buf.Bytes()))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.setupPid {
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
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Create test event data
			event := exitEvent{
				Pid: tt.pid,
			}

			// Create buffer with event data
			buf := new(bytes.Buffer)
			require.NoError(t, binary.Write(buf, binary.NativeEndian, event))

			cache, err := lru.New[int32, *process.Process](cacheSize)
			if err != nil {
				panic(err)
			}

			// Create mock receiver
			mockRcv := mocks.NewMockReceiver(ctrl)
			mockRcv.EXPECT().UnregisterProcess(int(tt.pid), 0).Return(tt.endProcErr)

			// Create manager
			m := &Manager{
				logger:   zap.NewNop(),
				cache:    cache,
				reciever: mockRcv,
			}

			// Test handler
			err = m.handleExitEvent(bytes.NewReader(buf.Bytes()))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
