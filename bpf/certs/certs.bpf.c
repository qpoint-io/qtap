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

// File open flags
#define O_RDONLY 00000000
#define O_WRONLY 00000001
#define O_RDWR   00000002
#define O_CREAT  00000100
#define O_TRUNC  00001000
#define O_APPEND 00002000

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
	__type(value, char[256]); // file path of the custom cert to use
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

	// get the file open flags
	struct open_how how;
	bpf_probe_read(&how, sizeof(how), (void *)PT_REGS_PARM3(ctx));

	// initilize the cert key
	struct cert_key key = {};

	// set the pid in the key
	key.pid = pid;

	// copy the filename to the key
	long filename_size = bpf_probe_read_user_str(key.file_path, sizeof(key.file_path), filename);

	// ensure the filename length/size is within bounds
	if (filename_size <= 0 || filename_size > 256) {
		return 0;
	}

	// ensure null-termination
	key.file_path[sizeof(key.file_path) - 1] = '\0';

	// get the custom cert from the map
	char *custom_cert = bpf_map_lookup_elem(&pid_cert_map, &key);
	if (!custom_cert) {
		return 0;
	}

	// check if the file is being opened for any write operation
	bool is_write_op = false;
	u64 flags        = how.flags;

	// look for any write flags
	if ((flags & O_WRONLY) || (flags & O_RDWR) || (flags & O_TRUNC) || (flags & O_APPEND) || (flags & O_CREAT)) {
		is_write_op = true;
	}

	// if this is a write operation, allow the write to occur on the original file
	if (is_write_op) {
		return 0;
	}

	// todo: generate an event that this pid is writing the file and to re-create the injection

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

	// replace the filename with the custom file location (must be same length or shorter)
	bpf_probe_write_user((void *)filename, custom_cert, filename_size);

	return 0;
}

SEC("kprobe/vfs_fstatat")
int BPF_KPROBE(monitor_cert_stat_entry) {
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
	long filename_size = bpf_probe_read_user_str(key.file_path, sizeof(key.file_path), filename);

	// ensure the filename length/size is within bounds
	if (filename_size <= 0 || filename_size > 256) {
		return 0;
	}

	// ensure null-termination
	key.file_path[sizeof(key.file_path) - 1] = '\0';

	// get the custom cert from the map
	char *custom_cert = bpf_map_lookup_elem(&pid_cert_map, &key);
	if (!custom_cert) {
		return 0;
	}

	// replace the filename with the custom file location (must be same length or shorter)
	bpf_probe_write_user((void *)filename, custom_cert, filename_size);

	return 0;
}
