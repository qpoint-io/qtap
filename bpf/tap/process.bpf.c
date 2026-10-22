/*
 * This code runs using libbpf in the Linux kernel.
 * Copyright 2025 - The Qpoint Authors
 *
 * This program is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation; either version 2 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License along
 * with this program; if not, write to the Free Software Foundation, Inc.,
 * 51 Franklin Street, Fifth Floor, Boston, MA 02110-1301 USA.
 *
 * SPDX-License-Identifier: GPL-2.0
 */

#include "vmlinux.h"
#include "common.bpf.h"
#include "bpf_tracing.h"
#include "bpf_helpers.h"
#include "process.bpf.h"
#include "bpf_core_read.h"

// process meta pushed down from Qtap
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, __u32); // pid
	__type(value, struct process_meta);
	__uint(max_entries, 5000);
} process_meta_map SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 4096);
	__type(key, int32_t); // pid
	__type(value, int32_t); // exit code
} exit_code_map SEC(".maps");

static __always_inline struct process_meta *get_process_meta(__u32 pid) {
	return bpf_map_lookup_elem(&process_meta_map, &pid);
}

// mmap flags
#define MAP_PRIVATE   0x02
#define MAP_DENYWRITE 0x0080

#define MAX_EXEC_PATH_SIZE 1024
#define MAX_ARGV_COUNT     20
#define MAX_ARGV_SIZE      1024

// proc events
// this must align with the events enum in process/event.go
enum PROC_EVENT {
	PROC_EXEC_START = 1U,
	PROC_EXEC_ARGV,
	PROC_EXEC_END,
	PROC_EXIT,
	PROC_MMAP,
	PROC_RENAME,
};

struct exec_start_event {
	uint64_t type;
	int32_t pid;
	uint32_t exe_size;
	char exe[MAX_EXEC_PATH_SIZE];
};

struct exec_argv_event {
	uint64_t type;
	int32_t pid;
	uint32_t argv_size;
	char argv[MAX_ARGV_SIZE];
};

struct exec_end_event {
	uint64_t type;
	int32_t pid;
};

// process coming online w/exec syscall
struct exec_info_event {
	// Event type
	uint64_t type;
	// Process PID
	int32_t pid;
};

// process exit
struct exit_info_event {
	// Event type
	uint64_t type;
	// Process PID
	int32_t pid;
	// Exit code
	int32_t exit_code;
};

// ring buffer to broadcast process events
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 64 * 1024 /* 64 KB */);
} proc_events SEC(".maps");

// sched_process_exec fires once per successful exec, after the new image is
// installed and before the task returns to user space. It reports only execs
// that succeed, and when a non-leader thread execs it reports the thread-group
// id. The sys_enter_execve(at) tracepoints are not used: on some kernels
// (Ubuntu 7.0.0-38-generic and 7.0.0-1017-azure, LP#2170225) BPF programs on
// syscall-enter tracepoints with user-pointer arguments run only on the CPU
// the perf event was opened on, so most execs went unreported.
// bpftrace -e 'tracepoint:sched:sched_process_exec { printf("pid: %d, file: %s\n", args->pid, str(args->filename)); }'
SEC("tracepoint/sched/sched_process_exec")
int tracepoint__sched__process_exec(struct trace_event_raw_sched_process_exec *ctx) {
	int pid = bpf_get_current_pid_tgid() >> 32;

	struct exec_start_event *start_event;
	start_event = bpf_ringbuf_reserve(&proc_events, sizeof(struct exec_start_event), 0);
	if (!start_event)
		return 0;

	start_event->type     = PROC_EXEC_START;
	start_event->pid      = pid;
	start_event->exe_size = 0;

	// the path passed to exec, stored in the tracepoint record
	unsigned short filename_offset = ctx->__data_loc_filename & 0xFFFF;
	int read_result                = bpf_probe_read_kernel_str(start_event->exe, sizeof(start_event->exe), (void *)ctx + filename_offset);
	if (read_result > 0) {
		start_event->exe_size = read_result;
	}
	bpf_ringbuf_submit(start_event, 0);

	// The new image's arguments are NUL-separated strings between arg_start and
	// arg_end, the same memory /proc/<pid>/cmdline reads. argv[0] is skipped
	// because the executable is reported separately.
	struct task_struct *task = (struct task_struct *)bpf_get_current_task();
	unsigned long arg        = BPF_CORE_READ(task, mm, arg_start);
	unsigned long arg_end    = BPF_CORE_READ(task, mm, arg_end);
	bool skip                = true;

	for (int i = 0; i < MAX_ARGV_COUNT && arg < arg_end; i++) {
		struct exec_argv_event *argv_event;
		argv_event = bpf_ringbuf_reserve(&proc_events, sizeof(struct exec_argv_event), 0);
		if (!argv_event)
			break;

		argv_event->type      = PROC_EXEC_ARGV;
		argv_event->pid       = pid;
		argv_event->argv_size = 0;

		long read_result = bpf_probe_read_user_str(argv_event->argv, sizeof(argv_event->argv), (void *)arg);
		if (read_result < 0) {
			bpf_ringbuf_discard(argv_event, 0);
			break;
		}
		// some kernels return 0 rather than 1 for an empty string
		if (read_result == 0) {
			argv_event->argv[0] = '\0';
			read_result         = 1;
		}

		if (skip) {
			bpf_ringbuf_discard(argv_event, 0);
		} else {
			argv_event->argv_size = read_result;
			bpf_ringbuf_submit(argv_event, 0);
		}

		// A full buffer may hold only the start of a longer argument. The next
		// read resumes at its last byte, the terminator if the argument fit
		// exactly, and is skipped like any read continuing an argument.
		skip = read_result == MAX_ARGV_SIZE;
		arg += skip ? read_result - 1 : read_result;
	}

	struct exec_info_event *info_event;
	info_event = bpf_ringbuf_reserve(&proc_events, sizeof(struct exec_info_event), 0);
	if (!info_event)
		return 0;

	info_event->type = PROC_EXEC_END;
	info_event->pid  = pid;
	bpf_ringbuf_submit(info_event, 0);

	return 0;
}

// sys_enter_exit_group is called when the process is being removed from the system
// bpftrace -e 'tracepoint:syscalls:sys_enter_exit_group { printf("Process exit: pid=%d comm=%s exit_code=%d\n", pid, comm, args->error_code);
// @exit_codes[comm] = args->error_code; }'
SEC("tracepoint/syscalls/sys_enter_exit_group")
int syscall__probe_entry_exit_group(struct trace_event_raw_sys_enter *ctx) {
	int pid           = bpf_get_current_userspace_pid();
	int32_t exit_code = (int32_t)ctx->args[0];

	// debug
	// bpf_printk("syscall__probe_entry_exit_group = pid: %u, exit_code: %d\n", pid, exit_code);

	bpf_map_update_elem(&exit_code_map, &pid, &exit_code, BPF_ANY);

	return 0;
}

// sched_process_exit is called when a task (thread) is being removed from the scheduler
// bpftrace -e 'tracepoint:sched:sched_process_exit { printf("pid: %d, comm: %s\n", args->pid, args->comm); }'
SEC("tracepoint/sched/sched_process_exit")
int tracepoint__sched__process_exit(struct trace_event_raw_sched_process_template *ctx) {
	// The tracepoint fires once per exiting thread, after do_exit has decremented
	// signal->live. Report only the exit that ends the thread group: when a
	// non-leader thread calls execve, the kernel kills the old leader (tid == tgid)
	// while the process lives on under the same pid with the new image.
	struct task_struct *task = (struct task_struct *)bpf_get_current_task();
	if (BPF_CORE_READ(task, signal, live.counter) != 0)
		return 0;

	struct exit_info_event *exit_event;

	exit_event = bpf_ringbuf_reserve(&proc_events, sizeof(struct exit_info_event), 0);
	if (!exit_event)
		return 0;

	// The last thread to exit is not necessarily the leader, so report the tgid.
	int32_t exit_code      = 0;
	int32_t pid            = bpf_get_current_userspace_pid();
	int32_t *exit_code_ptr = bpf_map_lookup_elem(&exit_code_map, &pid);
	if (exit_code_ptr != NULL) {
		exit_code = *exit_code_ptr;
		bpf_map_delete_elem(&exit_code_map, &pid);
	}

	exit_event->type      = PROC_EXIT;
	exit_event->pid       = pid;
	exit_event->exit_code = exit_code;

	// debug
	// bpf_printk("tracepoint__sched__process_exit = pid: %u, exit_code: %d\n", exit_event->pid, exit_code);

	bpf_ringbuf_submit(exit_event, 0);

	return 0;
}
