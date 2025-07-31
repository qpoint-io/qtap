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

static __always_inline struct process_meta *get_process_meta(__u32 pid) {
	return bpf_map_lookup_elem(&process_meta_map, &pid);
}

// mmap flags
#define MAP_PRIVATE   0x02
#define MAP_DENYWRITE 0x0080

#define MAX_EXEC_PATH_SIZE 1024
#define MAX_ARGV_COUNT     20
#define MAX_ARGV_SIZE      1024
#define MAX_ENV_SIZE       1024

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
};

// ring buffer to broadcast process events
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 64 * 1024 /* 64 KB */);
} proc_events SEC(".maps");

static int process_exec_ret() {
	struct exec_info_event *info_event;
	int pid = bpf_get_current_pid_tgid() >> 32;

	// reserve space in the ring buffer for the event
	info_event = bpf_ringbuf_reserve(&proc_events, sizeof(struct exec_info_event), 0);
	if (!info_event) {
		bpf_printk("process_exec_ret: failed to reserve space in ring buffer, pid=%d", pid);
		return 0;
	}

	// set the event type and pid
	info_event->type = PROC_EXEC_END;
	info_event->pid  = bpf_get_current_pid_tgid() >> 32;

	// notify
	bpf_ringbuf_submit(info_event, 0);

	return 0;
}

static int process_exec_entry(struct trace_event_raw_sys_enter *ctx) {
	// get the pid
	int pid = bpf_get_current_pid_tgid() >> 32;

	// reserve space in the ring buffer for the event
	struct exec_start_event *start_event;
	start_event = bpf_ringbuf_reserve(&proc_events, sizeof(struct exec_start_event), 0);
	if (!start_event) {
		bpf_printk("process_exec_entry: failed to reserve space in ring buffer for start_event, pid=%d", pid);
		return 0;
	}

	// initialize event
	start_event->type = PROC_EXEC_START;
	start_event->pid  = pid;

	// attempt to read the executable path
	int read_result = bpf_probe_read_str(start_event->exe, sizeof(start_event->exe), (void *)ctx->args[0]);
	if (read_result > 0) {
		// set the path size
		start_event->exe_size = read_result;
	} else {
		// failed to read executable path
		bpf_printk("process_exec_entry: failed to read executable path, pid=%d, read_result=%d", pid, read_result);
		start_event->exe_size = 0;
	}
	bpf_ringbuf_submit(start_event, 0);

	// extract arguments
	const char **argv = (const char **)(ctx->args[1]);
	for (int i = 1; i < MAX_ARGV_COUNT; i++) {
		const char *arg;
		if (bpf_probe_read(&arg, sizeof(arg), &argv[i]) != 0 || !arg) {
			bpf_printk("process_exec_entry: failed to read argv pointer or reached end, pid=%d, arg_index=%d", pid, i);
			break;
		}

		// reserve space in the ring buffer for the exec argv event
		struct exec_argv_event *argv_event;
		argv_event = bpf_ringbuf_reserve(&proc_events, sizeof(struct exec_argv_event), 0);
		if (!argv_event) {
			bpf_printk("process_exec_entry: failed to reserve space in ring buffer for argv_event, pid=%d, arg_index=%d", pid, i);
			return 0;
		}

		// initialize event
		argv_event->type = PROC_EXEC_ARGV;
		argv_event->pid  = pid;

		// set the argument
		int read_result = bpf_probe_read_str(argv_event->argv, sizeof(argv_event->argv), arg);

		// ensure the argument was read successfully
		if (read_result > 0) {
			// set the argument size
			argv_event->argv_size = read_result;

			// submit the event
			bpf_ringbuf_submit(argv_event, 0);
		} else {
			// discard the event
			bpf_printk("process_exec_entry: failed to read argument string, pid=%d, arg_index=%d, read_result=%d", pid, i, read_result);
			bpf_ringbuf_discard(argv_event, 0);
		}
	}

	return 0;
}

SEC("tracepoint/syscalls/sys_enter_execve")
int syscall__probe_entry_execve(struct trace_event_raw_sys_enter *ctx) {
	return process_exec_entry(ctx);
}

SEC("tracepoint/syscalls/sys_enter_execveat")
int syscall__probe_entry_execveat(struct trace_event_raw_sys_enter *ctx) {
	return process_exec_entry(ctx);
}

// bpftrace -e 'tracepoint:syscalls:sys_enter_exec*{ printf("pid: %d, comm: %s, args: ", pid, comm); join(args->argv); }'
SEC("tracepoint/syscalls/sys_exit_execve")
int syscall__probe_ret_execve() {
	return process_exec_ret();
}

// bpftrace -e 'tracepoint:syscalls:sys_enter_exec*{ printf("pid: %d, comm: %s, args: ", pid, comm); join(args->argv); }'
SEC("tracepoint/syscalls/sys_exit_execveat")
int syscall__probe_ret_execveat() {
	return process_exec_ret();
}

// bpftrace -e 'tracepoint:sched:sched_process_exit { printf("pid: %d, comm: %s\n", args->pid, args->comm); }'
SEC("tracepoint/sched/sched_process_exit")
int tracepoint__sched__process_exit(struct trace_event_raw_sched_process_template *ctx) {
	struct exit_info_event *exit_event;

	exit_event = bpf_ringbuf_reserve(&proc_events, sizeof(struct exit_info_event), 0);
	if (!exit_event) {
		bpf_printk("tracepoint__sched__process_exit: failed to reserve space in ring buffer, pid=%d", ctx->pid);
		return 0;
	}

	exit_event->type = PROC_EXIT;
	exit_event->pid  = ctx->pid;

	// debug
	// bpf_printk("tracepoint__sched__process_exit = pid: %u\n", exit_event->pid);

	bpf_ringbuf_submit(exit_event, 0);

	return 0;
}
