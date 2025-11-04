package process

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"

	"github.com/qpoint-io/qtap/pkg/process"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

var (
	execStartEventPool = sync.Pool{
		New: func() interface{} {
			return new(execStartEvent)
		},
	}
	execArgvEventPool = sync.Pool{
		New: func() interface{} {
			return new(execArgvEvent)
		},
	}
	execEndEventPool = sync.Pool{
		New: func() interface{} {
			return new(execEndEvent)
		},
	}
	exitEventPool = sync.Pool{
		New: func() interface{} {
			return new(exitEvent)
		},
	}
)

// handleExecStartEvent handles detecting when a process is being started
// and adds it to the init procs map
func (m *Manager) handleExecStartEvent(ctx context.Context, r *bytes.Reader) error {
	_, span := tracer.Start(ctx, "handleExecStartEvent",
		trace.WithLinks(trace.LinkFromContext(ctx)),
		trace.WithNewRoot(),
	)
	defer span.End()

	e := execStartEventPool.Get().(*execStartEvent)
	defer execStartEventPool.Put(e)

	if err := binary.Read(r, binary.NativeEndian, e); err != nil {
		m.logger.Error("failed to parse proc exec event (attr)", zap.Error(err))
		return nil
	}

	span.SetAttributes(attribute.Int("pid", int(e.Pid)))

	// attempt to read the executable path
	// in some cases, the executable path is not available and so
	// the event will have an empty exe with an exe_size of 0.
	//
	// the process manager will discover the executable path from the /proc
	// filesystem.
	var exe string
	if e.ExeSize > 0 {
		// Create a buffer to store the exe path
		exeBuf := bytes.NewBuffer(make([]byte, e.ExeSize-1))

		// read the exe path into the byte slice
		if _, err := r.Read(exeBuf.Bytes()); err != nil {
			m.logger.Error("failed to read exe path", zap.Error(err))
			return nil
		}

		exe = exeBuf.String()
		span.SetAttributes(attribute.String("exe", exe))
	}

	if m.reciever != nil {
		// create process
		p := process.NewProcess(int(e.Pid), exe, m.logger)

		// set the notifier so the process can indicate when it's changed
		// and when we should collect data for the ebpf meta map
		p.SetNotifier(func() error {
			return m.SetMeta(p)
		})

		m.cache.Add(int32(e.Pid), p)
	}

	return nil
}

// handleExecArgvEvent handles detecting when a process's arguments are being set
// and adds them to the proc init
func (m *Manager) handleExecArgvEvent(ctx context.Context, r *bytes.Reader) error {
	_, span := tracer.Start(ctx, "handleExecArgvEvent",
		trace.WithLinks(trace.LinkFromContext(ctx)),
		trace.WithNewRoot(),
	)
	defer span.End()

	e := execArgvEventPool.Get().(*execArgvEvent)
	defer execArgvEventPool.Put(e)

	if err := binary.Read(r, binary.NativeEndian, e); err != nil {
		m.logger.Error("failed to parse proc exec event (argv)", zap.Error(err))
		return nil
	}

	// Create a buffer to store the argv
	argv := bytes.NewBuffer(make([]byte, e.ArgvSize-1))
	span.SetAttributes(attribute.Int("pid", int(e.Pid)))

	// read the argv into the byte slice
	if _, err := r.Read(argv.Bytes()); err != nil {
		m.logger.Error("failed to read argv", zap.Error(err))
		return nil
	}

	if m.reciever != nil {
		p, ok := m.cache.Get(int32(e.Pid))
		if !ok {
			// process not found in cache, skip
			return nil
		}

		p.Lock()
		p.Args = append(p.Args, argv.String())
		p.Unlock()

		m.cache.Add(e.Pid, p)
	}

	return nil
}

// handleExecEndEvent handles detecting when a process exec is complete
// and applies the changes to the process
func (m *Manager) handleExecEndEvent(ctx context.Context, r *bytes.Reader) error {
	ctx, span := tracer.Start(ctx, "handleExecEndEvent",
		trace.WithLinks(trace.LinkFromContext(ctx)),
		trace.WithNewRoot(),
	)
	defer span.End()

	e := execEndEventPool.Get().(*execEndEvent)
	defer execEndEventPool.Put(e)

	if err := binary.Read(r, binary.NativeEndian, e); err != nil {
		m.logger.Error("failed to parse proc exec event (end)", zap.Error(err))
		return nil
	}

	if m.reciever != nil {
		p, ok := m.cache.Get(int32(e.Pid))
		if !ok {
			// process not found in cache, skip
			return nil
		}
		span.SetAttributes(attribute.Int("pid", int(e.Pid)))

		if err := m.reciever.RegisterProcess(ctx, p); err != nil {
			m.logger.Error("failed to add process", zap.Error(err))
		}

		m.cache.Remove(e.Pid)
	}

	return nil
}

// handleExitEvent handles detecting when a process has exited
// and removes it from the system
func (m *Manager) handleExitEvent(ctx context.Context, r *bytes.Reader) error {
	ctx, span := tracer.Start(ctx, "handleExitEvent",
		trace.WithLinks(trace.LinkFromContext(ctx)),
		trace.WithNewRoot(),
	)
	defer span.End()

	e := exitEventPool.Get().(*exitEvent)
	defer exitEventPool.Put(e)

	if err := binary.Read(r, binary.NativeEndian, e); err != nil {
		m.logger.Error("failed to parse proc exit event (attr)", zap.Error(err))
		return nil
	}

	if m.reciever != nil {
		span.SetAttributes(
			attribute.Int("pid", int(e.Pid)),
			attribute.Int("exit_code", int(e.ExitCode)),
		)
		if err := m.reciever.UnregisterProcess(ctx, int(e.Pid), int(e.ExitCode)); err != nil {
			m.logger.Error("failed to end process", zap.Error(err))
		}
	}

	return nil
}
