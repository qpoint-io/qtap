package process

import (
	"fmt"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"go.uber.org/zap"
)

// NewFromObjects creates a process event source using an already loaded QTap
// collection. The source owns its reader and tracepoint links; the caller retains
// ownership of the collection's maps and programs.
func NewFromObjects(logger *zap.Logger, objs *tap.TapObjects) (*Manager, error) {
	reader, err := ringbuf.NewReader(objs.ProcEvents)
	if err != nil {
		return nil, fmt.Errorf("creating process event reader: %w", err)
	}

	tracepoints := []*common.Tracepoint{
		common.NewTracepoint("syscalls", "sys_enter_execve", objs.SyscallProbeEntryExecve),
		common.NewTracepoint("syscalls", "sys_exit_execve", objs.SyscallProbeRetExecve),
		common.NewTracepoint("syscalls", "sys_enter_execveat", objs.SyscallProbeEntryExecveat),
		common.NewTracepoint("syscalls", "sys_exit_execveat", objs.SyscallProbeRetExecveat),
		common.NewTracepoint("syscalls", "sys_enter_exit_group", objs.SyscallProbeEntryExitGroup),
		common.NewTracepoint("sched", "sched_process_exit", objs.TracepointSchedProcessExit),
	}
	return New(logger, objs.ProcessMetaMap, reader, tracepoints), nil
}
