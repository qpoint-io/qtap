package process

// events represents process event types
type events uint64

// this must align with the events enum in bpf/capture/process.bpf.c
const (
	EVENT_EXEC_START events = iota + 1
	EVENT_EXEC_ARGV
	EVENT_EXEC_END
	EVENT_EXIT
	EVENT_MMAP
	EVENT_RENAME
)

// event is the base struct for all events
type event struct {
	Type events
}

// execStartEvent corresponds to exec_start_event
type execStartEvent struct {
	Pid     int32
	ExeSize uint32
}

// execArgvEvent corresponds to exec_argv_event
type execArgvEvent struct {
	Pid      int32
	ArgvSize uint32
}

// execEndEvent corresponds to exec_end_event
type execEndEvent struct {
	Pid int32
}

// exitEvent corresponds to exit_info_event
type exitEvent struct {
	Pid      int32
	ExitCode int32
}
