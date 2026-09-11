//go:build integration && linux

package process

import (
	"os"
	"testing"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestMap(t *testing.T, spec *ebpf.MapSpec) *ebpf.Map {
	t.Helper()
	m, err := ebpf.NewMap(spec)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func newTestProgram(t *testing.T, typ ebpf.ProgramType) *ebpf.Program {
	t.Helper()
	p, err := ebpf.NewProgram(&ebpf.ProgramSpec{
		Type: typ, License: "GPL",
		Instructions: asm.Instructions{asm.Mov.Imm(asm.R0, 0), asm.Return()},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func requireMapReleased(t *testing.T, m *ebpf.Map) {
	t.Helper()
	require.Equal(t, -1, m.FD(), "owned map descriptor remains open")
}

func TestReaderFailureReleasesCollection(t *testing.T) {
	// This is a valid loaded map but cannot back a ring-buffer reader. Failure
	// occurs after resource acquisition and before creating a process manager.
	m := newTestMap(t, &ebpf.MapSpec{Type: ebpf.Array, KeySize: 4, ValueSize: 4, MaxEntries: 1})
	p := newTestProgram(t, ebpf.TracePoint)
	objects := &tap.TapObjects{
		TapMaps:     tap.TapMaps{ProcEvents: m},
		TapPrograms: tap.TapPrograms{SyscallProbeEntryExecve: p},
	}
	monitor, err := newMonitor(zap.NewNop(), objects)
	require.ErrorContains(t, err, "creating process event reader")
	require.Nil(t, monitor)
	requireMapReleased(t, m)
	require.Equal(t, -1, p.FD(), "owned program descriptor remains open")
}

func TestStartupFailureReleasesAttachedProbe(t *testing.T) {
	// The first tracepoint attaches successfully. A program of the wrong type
	// makes the second attachment fail, exercising actual partial startup.
	first := newTestProgram(t, ebpf.TracePoint)
	invalid := newTestProgram(t, ebpf.SocketFilter)
	info, err := first.Info()
	require.NoError(t, err)
	id, ok := info.ID()
	require.True(t, ok)
	readerMap := newTestMap(t, &ebpf.MapSpec{Type: ebpf.RingBuf, MaxEntries: uint32(os.Getpagesize())})
	metadata := newTestMap(t, &ebpf.MapSpec{Type: ebpf.Hash, KeySize: 4, ValueSize: 32, MaxEntries: 5000})
	objects := &tap.TapObjects{
		TapMaps:     tap.TapMaps{ProcEvents: readerMap, ProcessMetaMap: metadata},
		TapPrograms: tap.TapPrograms{SyscallProbeEntryExecve: first, SyscallProbeRetExecve: invalid},
	}
	m, err := newMonitor(zap.NewNop(), objects)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, m.Stop()) })
	err = m.Start()
	require.ErrorContains(t, err, "syscalls/sys_exit_execve", "failure must reach the second attachment")
	requireMapReleased(t, readerMap)
	requireMapReleased(t, metadata)
	require.Equal(t, -1, first.FD())
	require.Equal(t, -1, invalid.FD())
	// A leaked first attachment would keep its program alive even after its
	// original descriptor was closed. Check the kernel, not just Go handles.
	remaining, err := ebpf.NewProgramFromID(id)
	if remaining != nil {
		_ = remaining.Close()
	}
	require.ErrorIs(t, err, os.ErrNotExist, "attached program survived cleanup")
	require.NoError(t, m.Stop(), "deferred cleanup after failed startup must be safe")
}
