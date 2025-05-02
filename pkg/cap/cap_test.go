//go:build linux

package cap

import (
	"errors"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanBpfProbeWriteUser(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping: only runs on Linux")
	}

	// Backup and restore original function
	origReadFile := readFile
	defer func() { readFile = origReadFile }()

	tests := []struct {
		name    string
		content string
		err     error
		expect  error
	}{
		{
			name:    "lockdown none",
			content: "[none]",
			expect:  nil,
		},
		{
			name:    "lockdown enabled",
			content: "[integrity]",
			expect:  ErrBpfProbeWriteUser,
		},
		{
			name:   "read error",
			err:    errors.New("fail"),
			expect: errors.New("failed to read lockdown file: fail"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			readFile = func(path string) ([]byte, error) {
				if tt.err != nil {
					return nil, tt.err
				}
				return []byte(tt.content), nil
			}
			err := CanBpfProbeWriteUser()
			if tt.expect == nil {
				require.NoError(t, err)
			} else {
				if errors.Is(tt.expect, ErrBpfProbeWriteUser) {
					require.ErrorIs(t, err, ErrBpfProbeWriteUser)
				} else {
					require.Error(t, err)
					require.Contains(t, err.Error(), "failed to read lockdown file")
				}
			}
		})
	}
}

func TestIsModernKernel(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping: only runs on Linux")
	}

	origReadFile := readFile
	defer func() { readFile = origReadFile }()

	tests := []struct {
		name   string
		ver    string
		err    error
		expect error
	}{
		{"modern kernel 5.10.0", "5.10.0", nil, nil},
		{"modern kernel 5.10", "5.10", nil, nil},
		{"old kernel", "5.4.0", nil, ErrModernKernel},
		{"very old kernel", "4.19.0", nil, ErrModernKernel},
		{"future kernel", "6.0.0", nil, nil},
		{"invalid format", "foo.bar", nil, errors.New("failed to parse kernel version: major version (foo) not an integer")},
		{"read error", "", errors.New("fail"), errors.New("failed to read kernel version")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			readFile = func(path string) ([]byte, error) {
				if tt.err != nil {
					return nil, tt.err
				}
				return []byte(tt.ver), nil
			}
			err := IsModernKernel()
			switch {
			case tt.expect == nil:
				require.NoError(t, err)
			case errors.Is(tt.expect, ErrModernKernel):
				require.ErrorIs(t, err, ErrModernKernel)
			default:
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expect.Error())
			}
		})
	}
}

func TestHasCgroupsV2(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping: only runs on Linux")
	}

	origStatfs := statfs
	defer func() { statfs = origStatfs }()

	tests := []struct {
		name   string
		fsType int64
		err    error
		expect error
	}{
		{"cgroups v2", CGROUP2_SUPER_MAGIC, nil, nil},
		{"cgroups v1", 0x12345678, nil, ErrCgroupsV2NotEnabled},
		{"statfs error", 0, errors.New("fail"), errors.New("failed to statfs /sys/fs/cgroup")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statfs = func(path string, buf *syscall.Statfs_t) error {
				if tt.err != nil {
					return tt.err
				}
				buf.Type = tt.fsType
				return nil
			}
			err := HasCgroupsV2()
			switch {
			case tt.expect == nil:
				require.NoError(t, err)
			case errors.Is(tt.expect, ErrCgroupsV2NotEnabled):
				require.ErrorIs(t, err, ErrCgroupsV2NotEnabled)
			default:
				require.Error(t, err)
				require.Contains(t, err.Error(), "failed to statfs /sys/fs/cgroup")
			}
		})
	}
}
