//go:build integration && linux

package process_test

import (
	"os"
	"testing"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSourceStopPreservesSharedObjects(t *testing.T) {
	program, err := ebpf.NewProgram(&ebpf.ProgramSpec{
		Type: ebpf.TracePoint, License: "GPL",
		Instructions: asm.Instructions{asm.Mov.Imm(asm.R0, 0), asm.Return()},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = program.Close() })
	programInfo, err := program.Info()
	require.NoError(t, err)
	programID, ok := programInfo.ID()
	require.True(t, ok)
	readerMap, err := ebpf.NewMap(&ebpf.MapSpec{Type: ebpf.RingBuf, MaxEntries: uint32(os.Getpagesize())})
	require.NoError(t, err)
	t.Cleanup(func() { _ = readerMap.Close() })
	metadata, err := ebpf.NewMap(&ebpf.MapSpec{Type: ebpf.Hash, KeySize: 4, ValueSize: 4, MaxEntries: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = metadata.Close() })
	objects := &tap.TapObjects{
		TapMaps: tap.TapMaps{ProcEvents: readerMap, ProcessMetaMap: metadata},
		TapPrograms: tap.TapPrograms{
			SyscallProbeEntryExecve: program, SyscallProbeRetExecve: program,
			SyscallProbeEntryExecveat: program, SyscallProbeRetExecveat: program,
			SyscallProbeEntryExitGroup: program, TracepointSchedProcessExit: program,
		},
	}
	source, err := process.NewEventSource(zap.NewNop(), objects)
	require.NoError(t, err)
	t.Cleanup(func() { _ = source.Stop() })
	require.NoError(t, source.Start(t.Context()))
	require.NoError(t, source.Stop())

	// QTap still owns these objects after stopping process discovery. Exercise
	// the maps rather than relying only on a nonnegative descriptor number.
	require.NoError(t, metadata.Put(uint32(1), uint32(42)))
	var value uint32
	require.NoError(t, metadata.Lookup(uint32(1), &value))
	require.Equal(t, uint32(42), value)
	_, err = readerMap.Info()
	require.NoError(t, err)
	_, err = program.Info()
	require.NoError(t, err)
	require.NoError(t, objects.Close())
	remaining, err := ebpf.NewProgramFromID(programID)
	if remaining != nil {
		_ = remaining.Close()
	}
	require.ErrorIs(t, err, os.ErrNotExist, "source retained an attachment after stop")
}

func TestSourceConstructionFailurePreservesSharedObjects(t *testing.T) {
	// A map that cannot back a reader makes construction fail. Ownership still
	// belongs to the QTap caller, so the map remains usable afterward.
	m, err := ebpf.NewMap(&ebpf.MapSpec{Type: ebpf.Array, KeySize: 4, ValueSize: 4, MaxEntries: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })
	source, err := process.NewEventSource(zap.NewNop(), &tap.TapObjects{TapMaps: tap.TapMaps{ProcEvents: m}})
	require.ErrorContains(t, err, "creating process event reader")
	require.Nil(t, source)
	require.NoError(t, m.Put(uint32(0), uint32(42)))
}
