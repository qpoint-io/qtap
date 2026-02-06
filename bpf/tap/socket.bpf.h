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

#pragma once

#include "tap.bpf.h"
#include "trace.bpf.h"

// A unique file descriptor key consisting of pid and fd
struct pid_fd_key {
	uint32_t pid;
	int32_t fd;
};

// Socket context for map lookups
struct socket_ctx {
	struct pid_fd_key *id;
	uint64_t pid_tgid;
	char trace_id[64];
	enum QTAP_COMPONENT trace_mod;
};

// A uprobe request for a fd from syscall socket layer
struct fd_request {
	uint32_t fd;
	bool is_ssl;
};

// Helper struct to cache input argument of connect/accept syscalls between the
// entry hook and the exit hook
struct addr_args {
	// Point to an address structure
	uintptr_t addr;
};

// Helper struct to cache input argument of read/write syscalls between the
// entry hook and the exit hook
struct data_args {
	// File descriptor
	int32_t fd;
	// Point to the byte buffer
	uintptr_t buf;
	// The number of buffers (if a buffer count system call)
	int32_t iovcnt;
	// Optional, pointer to ssl instance
	uintptr_t ssl;
	// Optional, pointer to number of bytes read/written (openssl)
	uintptr_t ex_bytes;
};

// Helper struct that hold the input arguments of the close syscall
struct close_args {
	// File descriptor
	int32_t fd;
};

// Socket events (for user space applications)
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 256 * 1024 /* 256 KB */);
} socket_events SEC(".maps");

// File descriptor requests from uprobes
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__type(key, uint64_t); // pid_tgid
	__type(value, struct fd_request);
	__uint(max_entries, 1024);
} uprobe_fd_requests SEC(".maps");

// Track connections across its system calls
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 16384);
	__type(key, struct pid_fd_key);
	__type(value, struct conn_info);
} conn_info_map SEC(".maps");

/**
 * Determines the socket type based on protocol and socket type
 *
 * @param protocol The protocol from socket() syscall (e.g. IPPROTO_TCP)
 * @param type The socket type from socket() syscall (e.g. SOCK_STREAM)
 * @return The determined socket type enum value
 */
static __always_inline enum SOCKET_TYPE determine_socket_type(int protocol, int type) {
	enum SOCKET_TYPE sock_type;

	// set according to specified protocol
	switch (protocol) {
	case IPPROTO_TCP:
		sock_type = S_TCP;
		break;
	case IPPROTO_UDP:
		sock_type = S_UDP;
		break;
	case IPPROTO_RAW:
		sock_type = S_RAW;
		break;
	case IPPROTO_ICMP:
		sock_type = S_ICMP;
		break; // Added break to fix the fallthrough
	default:
		sock_type = S_UNKNOWN;
	}

	// if protocol wasn't set, but it's a stream
	if (protocol == 0 && type == SOCK_STREAM)
		sock_type = S_TCP;

	// if protocol wasn't specified, but it's a datagram
	if (protocol == 0 && type == SOCK_DGRAM)
		sock_type = S_UDP;

	return sock_type;
}

static __noinline void process_data(struct socket_ctx *ctx, enum DIRECTION direction, const struct data_args *args, ssize_t bytes, bool ssl);

static void process_close(struct socket_ctx *ctx);
