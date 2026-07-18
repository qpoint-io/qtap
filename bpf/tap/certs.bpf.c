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
#include "bpf_core_read.h"
#include "bpf_helpers.h"
#include "bpf_tracing.h"

enum CERT_EVENT {
	CERT_READ = 1ULL, // new cert read
};

// an injected cert read event
struct cert_read_event {
	uint64_t type; // CERT_READ
	int64_t pid; // pid of the process that read the cert
	uint32_t file_size; // size of the file char array
	char file[256]; // file path
};

// composite key for the container cert map
struct cert_key {
	__u32 pid; // the pid of the process
	char file_path[256]; // 255 + null terminator
};

// container cert map pushed down from Qtap
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, struct cert_key); // pid + file path
	__type(value, bool); // whether to send the cert read event to the agent
	__uint(max_entries, 1024);
} pid_cert_map SEC(".maps");

// ring buffer for cert events
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 256 * 1024); // 256 KB buffer
} cert_events SEC(".maps");

SEC("kprobe/do_sys_openat2")
int BPF_KPROBE(monitor_cert_open_entry) {
	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// get the pid
	pid_t pid = pid_tgid >> 32;

	// get the filename from the user space
	const char *filename = (const char *)PT_REGS_PARM2(ctx);

	// initilize the cert key
	struct cert_key key = {};

	// set the pid in the key
	key.pid = pid;

	// copy the filename to the key
	bpf_probe_read_user_str(key.file_path, sizeof(key.file_path), filename);

	// ensure null-termination
	key.file_path[sizeof(key.file_path) - 1] = '\0';

	// see if we're watching this cert
	bool *watched = bpf_map_lookup_elem(&pid_cert_map, &key);
	if (!watched) {
		return 0;
	}

	// reserve a cert read event
	struct cert_read_event *event = bpf_ringbuf_reserve(&cert_events, sizeof(struct cert_read_event), 0);
	if (!event) {
		return 0;
	}

	// set the event
	event->type = CERT_READ;
	event->pid  = pid;

	// read the filename
	event->file_size = bpf_probe_read_user_str(event->file, sizeof(event->file), filename);

	// submit the event
	bpf_ringbuf_submit(event, 0);

	return 0;
}
