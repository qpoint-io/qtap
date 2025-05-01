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
#include "tap.bpf.h"
#include "common.bpf.h"
#include "protocol.bpf.h"
#include "socket.bpf.h"
#include "bpf_tracing.h"
#include "bpf_endian.h"
#include "bpf_helpers.h"
#include "bpf_core_read.h"
#include "trace.bpf.h"
#include "settings.bpf.h"
#include "sock_pid_fd.bpf.h"
#include "sock_utils.bpf.h"
#include "net.bpf.h"
#include "process.bpf.h"

// This defines how many chunks a perf_submit can support.
// This applies to messages that are over MAX_MSG_SIZE,
// and effectively makes the maximum message size to be CHUNK_LIMIT*MAX_MSG_SIZE.
#define CHUNK_LIMIT 4

#define socklen_t size_t

// Define a minimal structure to read the address family.
// This assumes you're interested in IPv4 and IPv6, for example.
struct minimal_sockaddr {
	unsigned short sa_family; // Address family, AF_INET or AF_INET6
};

// The syscall kernel functions we're observing
enum SYSCALL_OP {
	SYS_ACCEPT,
	SYS_ACCEPT4,
	SYS_CONNECT,
	SYS_CLOSE,
	SYS_READ,
	SYS_READV,
	SYS_RECVFROM,
	SYS_WRITE,
	SYS_WRITEV,
	SYS_SENDTO,
};

// A unique key combination of pid_tgid and syscall function names
struct socket_op_key {
	uint64_t pid_tgid;
	enum SYSCALL_OP func_name;
};

// ip v4/v6 socket state
struct inet_sock_state {
	uint64_t pid;
	struct net_addr local;
	struct net_addr remote;
};

// A composite key for syscall probes to lookup the socket state
struct sock_state_key {
	uint64_t pid;
	uint8_t addr[16];
	uint16_t port;
};

// Persist the socket type for the exit handler
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 16384);
	__type(key, uint64_t); // pid_tgid
	__type(value, int); // the socket type
} active_socket_args_map SEC(".maps");

// Track socket types
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 16384);
	__type(key, struct pid_fd_key);
	__type(value, int); // the socket type
} active_socket_types SEC(".maps");

// Persist the fd for the exit handler
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 16384);
	__type(key, struct socket_op_key);
	__type(value, int32_t); // the file descriptor
} active_fd_args_map SEC(".maps");

// Track address information across system calls
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 16384);
	__type(key, struct pid_fd_key);
	__type(value, struct addr_args);
} active_addr_args_map SEC(".maps");

// Data to be written
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 16384);
	__type(key, struct pid_fd_key);
	__type(value, struct data_args);
} active_write_args_map SEC(".maps");

// Data to be read
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 16384);
	__type(key, struct pid_fd_key);
	__type(value, struct data_args);
} active_read_args_map SEC(".maps");

// Args for when closing a file descriptor
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 16384);
	__type(key, struct pid_fd_key);
	__type(value, struct close_args);
} active_close_args_map SEC(".maps");

// Heap memory for temporary storing data events
struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__uint(max_entries, 1);
	__type(key, uint32_t);
	__type(value, struct socket_data_event);
} socket_data_event_buffer_heap SEC(".maps");

// Heap memory for temporary storing hostnames
struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__uint(max_entries, 1);
	__type(key, uint32_t);
	__type(value, struct socket_hostname_event);
} socket_hostname_event_heap SEC(".maps");

// Heap memory for temporary storing tls client hello events
struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__uint(max_entries, 1);
	__type(key, uint32_t);
	__type(value, struct socket_tls_client_hello_event);
} socket_tls_client_hello_event_heap SEC(".maps");

// submit the open event …
static void submit_open_event(struct socket_ctx *ctx, struct conn_info *conn_info) {
	// don't submit more than once
	if (conn_info->is_open)
		return;

	// fetch the process meta
	struct process_meta *meta = get_process_meta(ctx->id->pid);

	// if the qpoint strategy is to ignore, don't submit
	if (meta != NULL && meta->qpoint_strategy == QP_IGNORE) {
		conn_info->ignore = true;
		return;
	}

	// reserve space in the ring buffer
	struct socket_open_event *open_event;
	open_event = bpf_ringbuf_reserve(&socket_events, sizeof(struct socket_open_event), 0);
	if (!open_event)
		return;

	// init open_event
	open_event->type         = S_OPEN;
	open_event->timestamp_ns = conn_info->conn_pid_id.tsid;
	open_event->conn_pid_id  = conn_info->conn_pid_id;
	open_event->pid          = ctx->id->pid;
	open_event->tgid         = (uint32_t)(ctx->pid_tgid & 0xFFFFFFFF);
	open_event->socket_type  = S_UNKNOWN;

	// try to fetch the socket type
	int *type_ptr = bpf_map_lookup_elem(&active_socket_types, ctx->id);
	if (type_ptr != NULL)
		open_event->socket_type = (enum SOCKET_TYPE) * type_ptr;

	// create a pid_fd_key
	struct pid_fd_key fd_sock_key = {
		.pid = ctx->id->pid,
		.fd  = ctx->id->fd,
	};

	// try to fetch the socket pointer
	uintptr_t *sock_ptr = bpf_map_lookup_elem(&pid_fd_to_sock_map, &fd_sock_key);
	if (sock_ptr != NULL) {
		const struct socket *sock = (struct socket *)*sock_ptr;

		struct sock *sk;
		bpf_probe_read(&sk, sizeof(sk), &sock->sk);

		// read socket cookie
		uint64_t cookie;
		bpf_probe_read(&cookie, sizeof(cookie), &sk->__sk_common.skc_cookie);
		conn_info->cookie  = cookie;
		open_event->cookie = cookie;
		// bpf_printk("submit_open_event, cookie: %lu", cookie);

		if (cookie <= 0) {
			// If we don't have a valid cookie, ignore the connection
			// this is either an error state or premature and will
			// be called again.
			// TODO(Jon): Investigate ways to tighten up the trigger
			// points for this event.
			conn_info->ignore = true;
		}

		// read the local and remote ports
		__be16 local_port;
		bpf_probe_read(&local_port, sizeof(local_port), &sk->__sk_common.skc_num);
		__be16 remote_port;
		bpf_probe_read(&remote_port, sizeof(remote_port), &sk->__sk_common.skc_dport);

		// the local port is being retrieved from the kernel data, where it stores
		// the port in host order, so we need to convert it to network order
		local_port = bpf_htons(local_port);

		// read the address family
		sa_family_t family;
		bpf_probe_read(&family, sizeof(family), &sk->__sk_common.skc_family);

		// create local net_addr
		struct net_addr local_addr = {};
		local_addr.sa_family       = family;
		local_addr.port            = local_port;

		// create remote net_addr
		struct net_addr remote_addr = {};
		remote_addr.sa_family       = family;
		remote_addr.port            = remote_port;

		// parse ipv4 address
		if (family == AF_INET) {
			// read the local and remote addresses
			__be32 local_ip;
			bpf_probe_read(&local_ip, sizeof(local_ip), &sk->__sk_common.skc_rcv_saddr);
			__be32 remote_ip;
			bpf_probe_read(&remote_ip, sizeof(remote_ip), &sk->__sk_common.skc_daddr);

			// copy the local and remote addresses
			__builtin_memcpy(local_addr.addr, &local_ip, sizeof(local_ip));
			__builtin_memcpy(remote_addr.addr, &remote_ip, sizeof(remote_ip));
		}

		// parse ipv6 address
		if (family == AF_INET6) {
			// read the local and remote addresses
			struct in6_addr local_ip;
			bpf_probe_read(&local_ip, sizeof(local_ip), &sk->__sk_common.skc_v6_rcv_saddr);
			struct in6_addr remote_ip;
			bpf_probe_read(&remote_ip, sizeof(remote_ip), &sk->__sk_common.skc_v6_daddr);

			// copy the local and remote addresses
			__builtin_memcpy(local_addr.addr, local_ip.in6_u.u6_addr8, sizeof(local_addr.addr));
			__builtin_memcpy(remote_addr.addr, remote_ip.in6_u.u6_addr8, sizeof(remote_addr.addr));
		}

		if (type_ptr == NULL) {
			// bpf_printk("type_ptr is NULL; trying fallback");

			// Extract the protocol and socket type from the socket structure
			int proto = 0;
			bpf_probe_read(&proto, sizeof(proto), &sk->sk_protocol);

			// Get socket type from sk_type (SOCK_STREAM, SOCK_DGRAM, etc.)
			int sock_type = 0;
			bpf_probe_read(&sock_type, sizeof(sock_type), &sk->sk_type);

			// Determine socket type using our inline function
			open_event->socket_type = determine_socket_type(proto, sock_type);

			// bpf_printk("fallback: pid: %u, proto: %d, sock_type: %d, socket_type: %d", ctx->id->pid, proto, sock_type, open_event->socket_type);
		}

		// Set the local and remote addresses on the open_event
		open_event->local  = local_addr;
		open_event->remote = remote_addr;
	}

	// if remote isn't set yet for some reason, fallback to conn_info->addr
	if (open_event->remote.sa_family == 0)
		open_event->remote = conn_info->addr;

	// determine if this connections destination is a management address
	// which means it has been redirected and it going to be handled
	// by an external service such as a transparent proxy
	open_event->is_redirected = is_management_address(&open_event->remote);

	// get the capture direction from settings
	enum DIRECTION capture_direction = get_direction_setting();

	// get ignore_loopback from settings
	bool ignore_loopback = get_ignore_loopback_setting();

	// determine if this is a loopback IP
	bool is_loopback = is_local_ip(&open_event->remote);

	// ignore local/loopback
	if (is_loopback && ignore_loopback) {
		conn_info->ignore = true;
	}

	// are we capturing ingress?
	if (conn_info->conn_pid_id.function == C_SERVER && !(capture_direction == D_INGRESS || capture_direction == D_ALL))
		conn_info->ignore = true;

	// are we capturing egress?
	if (conn_info->conn_pid_id.function == C_CLIENT) {
		// ignore if explicit direction is ingress
		if (capture_direction == D_INGRESS)
			conn_info->ignore = true;

		// determine if this is an internal service
		bool is_internal_service = is_private_ip(&open_event->remote);

		// ignore internal services when only looking for external
		if (is_internal_service && capture_direction == D_EGRESS_EXTERNAL && !open_event->is_redirected)
			conn_info->ignore = true;

		// ignore external services when only looking for internal
		if (!is_internal_service && capture_direction == D_EGRESS_INTERNAL)
			conn_info->ignore = true;
	}

	// except DNS queries, don't ignore them
	if (conn_info->conn_pid_id.function == C_CLIENT && is_dns(conn_info))
		conn_info->ignore = true; // TODO: remove this when we have a better way to decode DNS

	// check if we should ignore this connection from filter
	if (SKIP_ALL(ctx->id->pid))
		conn_info->ignore = true;

	// if we're ignoring, discard the event and return early
	if (conn_info->ignore) {
		bpf_ringbuf_discard(open_event, 0);
		return;
	}

	// submit event
	bpf_ringbuf_submit(open_event, 0);

	// mark connection as open so we don't submit again
	conn_info->is_open = true;
}

// submit the protocol event
static void submit_proto_event(struct socket_ctx *ctx, struct conn_info *conn_info) {
	// if we don't know the protocol, don't submit
	if (conn_info->protocol == P_UNKNOWN)
		return;

	// reserve space in the ring buffer
	struct socket_proto_event *proto_event;
	proto_event = bpf_ringbuf_reserve(&socket_events, sizeof(struct socket_proto_event), 0);
	if (!proto_event)
		return;

	// init proto_event
	proto_event->type         = S_PROTO;
	proto_event->timestamp_ns = bpf_ktime_get_ns();
	proto_event->conn_pid_id  = conn_info->conn_pid_id;
	proto_event->cookie       = conn_info->cookie;
	proto_event->protocol     = conn_info->protocol;
	proto_event->is_ssl       = conn_info->is_ssl;

	// debug
	// bpf_printk("socket_proto_event = pid: %u, fd: %u, protocol: %s, is_ssl: %d\n", ctx->id->pid, ctx->id->fd, proto_event->protocol_string,
	// proto_event->is_ssl);

	// submit event
	bpf_ringbuf_submit(proto_event, 0);
}

// common addr handler for multiple syscall probes
static void process_syscall_addr(struct socket_ctx *ctx, struct addr_args *addr, enum CONNECTION_TYPE function) {
	// extract the sock address
	struct sockaddr *sockaddr = (struct sockaddr *)addr->addr;

	// read the socket family
	sa_family_t family;
	bpf_probe_read_user(&family, sizeof(family), (const void *)&sockaddr->sa_family);

	// we're only concerned with network sockets
	if (!(family == AF_INET || family == AF_INET6))
		return;

	// initialize connection info
	struct conn_info conn_info     = {};
	conn_info.rd_bytes             = 0;
	conn_info.wr_bytes             = 0;
	conn_info.is_open              = false;
	conn_info.is_ssl               = false;
	conn_info.protocol             = P_UNKNOWN;
	conn_info.conn_pid_id.pid      = ctx->id->pid;
	conn_info.conn_pid_id.tgid     = (uint32_t)(ctx->pid_tgid & 0xFFFFFFFF);
	conn_info.conn_pid_id.fd       = ctx->id->fd;
	conn_info.conn_pid_id.tsid     = bpf_ktime_get_ns();
	conn_info.conn_pid_id.function = function;

	// set the address family
	conn_info.addr.sa_family = family;

	// extract the address
	if (family == AF_INET) {
		struct sockaddr_in *sa = (struct sockaddr_in *)sockaddr;
		bpf_probe_read_user(&conn_info.addr.addr, 4, &sa->sin_addr);
		bpf_probe_read_user(&conn_info.addr.port, sizeof(conn_info.addr.port), &sa->sin_port);
	} else {
		struct sockaddr_in6 *sa = (struct sockaddr_in6 *)sockaddr;
		bpf_probe_read_user(&conn_info.addr.addr, 16, &sa->sin6_addr);
		bpf_probe_read_user(&conn_info.addr.port, sizeof(conn_info.addr.port), &sa->sin6_port);
	}

	submit_open_event(ctx, &conn_info);

	// persist
	bpf_map_update_elem(&conn_info_map, ctx->id, &conn_info, BPF_ANY);
}

// common close handler for multiple syscall probes
static void process_close(struct socket_ctx *ctx) {
	// lookup the connection
	struct conn_info *conn_info = bpf_map_lookup_elem(&conn_info_map, ctx->id);
	if (conn_info == NULL)
		return;

	// submit the close event if it the connection was reported
	if (conn_info->is_open) {
		// reserve space in the ring buffer
		struct socket_close_event *close_event;
		close_event = bpf_ringbuf_reserve(&socket_events, sizeof(struct socket_close_event), 0);
		if (close_event) {
			close_event->type         = S_CLOSE;
			close_event->timestamp_ns = bpf_ktime_get_ns();
			close_event->conn_pid_id  = conn_info->conn_pid_id;
			close_event->cookie       = conn_info->cookie;
			close_event->rd_bytes     = conn_info->rd_bytes;
			close_event->wr_bytes     = conn_info->wr_bytes;
			close_event->pid          = ctx->id->pid;
			close_event->tgid         = (uint32_t)(ctx->pid_tgid & 0xFFFFFFFF);

			bpf_ringbuf_submit(close_event, 0);
		}
	}

	// delete from conn_info map
	bpf_map_delete_elem(&conn_info_map, ctx->id);
}

// submit a single chunk from the buffer to the ringbuffer
static __always_inline void submit_buffer_chunk(const void *buf, size_t buf_size, struct socket_data_event *event) {
	// again, ensure the compiler doesn't confuse the verifier
	size_t buf_size_minus_1 = buf_size - 1;
	asm volatile("" : "+r"(buf_size_minus_1) :);
	buf_size = buf_size_minus_1 + 1;

	// read from the buffer up to the maximum message size
	size_t amount_copied = (buf_size < MAX_MSG_SIZE) ? buf_size : MAX_MSG_SIZE;
	if (bpf_probe_read_user(&event->msg, amount_copied, buf) != 0) {
		return;
	}

	// submit to the ring buffer
	if (amount_copied > 0) {
		event->attr.msg_size = amount_copied;
		bpf_ringbuf_output(&socket_events, event, sizeof(event->type) + sizeof(event->attr) + amount_copied, 0);
	}
}

// submit the entire buffer to the ringbuffer
static void submit_buffer(const enum DIRECTION direction, const void *buf, const size_t size, struct conn_info *conn, struct socket_data_event *ev) {
	int bytes_sent = 0;
	unsigned int i;

	// we have to break the buffer into chunks and submit the chunks
	for (i = 0; i < CHUNK_LIMIT; ++i) {
		const int bytes_remaining = size - bytes_sent;
		const size_t current_size = (bytes_remaining > MAX_MSG_SIZE && (i != CHUNK_LIMIT - 1)) ? MAX_MSG_SIZE : bytes_remaining;

		// advance the position
		switch (direction) {
		case D_EGRESS:
			ev->attr.pos = conn->wr_bytes + bytes_sent;
			break;
		case D_INGRESS:
			ev->attr.pos = conn->rd_bytes + bytes_sent;
			break;
		default:
			break;
		}

		// submit the chunk
		submit_buffer_chunk(buf + bytes_sent, current_size, ev);

		// determine progress
		bytes_sent += current_size;
		if (size == bytes_sent) {
			return;
		}
	}
}

// update the tracking data
static __always_inline void update_bandwidth_tracking(struct conn_info *conn_info, enum DIRECTION direction, ssize_t bytes) {
	// update the tracking data
	switch (direction) {
	case D_EGRESS:
		conn_info->wr_bytes += bytes;
		break;
	case D_INGRESS:
		conn_info->rd_bytes += bytes;
		break;
	default:
		break;
	}
}

// common data handler for multiple syscall probes
static void init_conn(struct socket_ctx *ctx, enum DIRECTION direction, const struct data_args *args, ssize_t bytes) {
	// nothing to do if the buffer is null
	if ((void *)args->buf == NULL) {
		return;
	}

	// nothing to do if bytes is empty
	if (bytes <= 0) {
		return;
	}

	// lookup the connection
	struct conn_info *conn_info = bpf_map_lookup_elem(&conn_info_map, ctx->id);

	// we need a connection
	if (conn_info == NULL) {
		TRACE_IF_ENABLED(ctx->trace_mod, ctx->id->pid, "init_conn (conn_info = NULL)", TRACE_STRING("caller", ctx->trace_id),
			TRACE_INT("pid", ctx->id->pid), TRACE_INT("fd", ctx->id->fd), TRACE_INT("direction", direction), TRACE_INT("bytes", bytes),
			TRACE_BOOL("open", false));
		return;
	}

	// lookup the process meta
	struct process_meta *meta = get_process_meta(ctx->id->pid);

	// if the qpoint strategy is to ignore, don't do anything
	if (meta != NULL && meta->qpoint_strategy == QP_IGNORE) {
		conn_info->ignore = true;
		return;
	}

	// checks that no ingress reads or egress writes have happened yet, indicating a new connection
	bool is_new_connection = (direction == D_INGRESS && conn_info->rd_bytes == 0) || (direction == D_EGRESS && conn_info->wr_bytes == 0);
	if (!is_new_connection)
		return;

	// initialize the buf_info struct
	struct buf_info buf_info = {
		.buf    = (const void *)(uintptr_t)args->buf,
		.iovcnt = args->iovcnt,
	};

	// submit the open event if it's not already
	if (!conn_info->is_open) {
		// detect tls if not already detected and the connection is new
		detect_tls(conn_info, &buf_info, bytes);

		// if open didn't succeed, return
		if (!conn_info->is_open) {
			TRACE_IF_ENABLED(ctx->trace_mod, ctx->id->pid, "init_conn (open = false)", TRACE_STRING("caller", ctx->trace_id),
				TRACE_INT("pid", ctx->id->pid), TRACE_INT("fd", ctx->id->fd), TRACE_INT("direction", direction), TRACE_INT("bytes", bytes));
			return;
		}
	}

	if (!conn_info->is_ssl)
		return;

	// if this is a forwarded connection, we're done here
	if (meta != NULL && (meta->qpoint_strategy == QP_FORWARD || meta->qpoint_strategy == QP_PROXY)) {
		return;
	}

	// if we're ssl, extract the tls handshake
	uint32_t key                                = 0;
	struct socket_tls_client_hello_event *hello = bpf_map_lookup_elem(&socket_tls_client_hello_event_heap, &key);
	if (hello == NULL) {
		return;
	}

	if (capture_tls_client_hello(hello, &buf_info, bytes)) {
		hello->type        = S_TLS_CLIENT_HELLO;
		hello->attr.cookie = conn_info->cookie;
		// handshake.data now contains the complete ClientHello
		// handshake.size contains the actual size of the data

		bpf_ringbuf_output(&socket_events, hello, sizeof(*hello), 0);
	}
}

// common data handler for multiple syscall probes
static void process_data(struct socket_ctx *ctx, enum DIRECTION direction, const struct data_args *args, ssize_t bytes, bool ssl) {
	// nothing to do if the buffer is null
	if ((void *)args->buf == NULL) {
		return;
	}

	// nothing to do if bytes is empty
	if (bytes <= 0) {
		return;
	}

	// lookup the connection
	struct conn_info *conn_info = bpf_map_lookup_elem(&conn_info_map, ctx->id);

	// we need a connection
	if (conn_info == NULL) {
		TRACE_IF_ENABLED(ctx->trace_mod, ctx->id->pid, "process_data (conn_info = NULL)", TRACE_STRING("caller", ctx->trace_id),
			TRACE_INT("pid", ctx->id->pid), TRACE_INT("fd", ctx->id->fd), TRACE_INT("direction", direction), TRACE_INT("bytes", bytes),
			TRACE_BOOL("ssl", ssl), TRACE_BOOL("open", false));
		return;
	}

	// the connection should be open at this point
	if (!conn_info->is_open) {
		TRACE_IF_ENABLED(ctx->trace_mod, ctx->id->pid, "process_data (conn_info->is_open = false)", TRACE_STRING("caller", ctx->trace_id),
			TRACE_INT("pid", ctx->id->pid), TRACE_INT("fd", ctx->id->fd), TRACE_INT("direction", direction), TRACE_INT("bytes", bytes),
			TRACE_BOOL("ssl", ssl), TRACE_BOOL("open", false));
		return;
	}

	// initialize the buf_info struct
	struct buf_info buf_info = {
		.buf    = (const void *)(uintptr_t)args->buf,
		.iovcnt = args->iovcnt,
	};

	// update the bandwidth tracking if this is not being called from a ssl function
	if (!ssl)
		update_bandwidth_tracking(conn_info, direction, bytes);

	// if ignore is set, don't waste any time
	if (conn_info->ignore) {
		TRACE_IF_ENABLED(ctx->trace_mod, ctx->id->pid, "process_data (ignore = true)", TRACE_STRING("caller", ctx->trace_id),
			TRACE_INT("pid", ctx->id->pid), TRACE_INT("fd", ctx->id->fd), TRACE_INT("direction", direction), TRACE_INT("bytes", bytes),
			TRACE_BOOL("ssl", ssl));
		return;
	}

	// lookup the process meta
	struct process_meta *meta = get_process_meta(ctx->id->pid);

	// if the qpoint strategy is to ignore, don't do anything
	if (meta != NULL && meta->qpoint_strategy == QP_IGNORE) {
		conn_info->ignore = true;
		return;
	}

	// if this is a forwarded connection, don't process
	if (meta != NULL && (meta->qpoint_strategy == QP_FORWARD || meta->qpoint_strategy == QP_PROXY)) {
		return;
	}

	// set ssl right away if provided
	if (ssl && !conn_info->is_ssl)
		conn_info->is_ssl = true;

	// once we're ssl, don't process unless from ssl functions
	if (!ssl && conn_info->is_ssl) {
		TRACE_IF_ENABLED(ctx->trace_mod, ctx->id->pid, "process_data (not ssl)", TRACE_STRING("caller", ctx->trace_id),
			TRACE_INT("pid", ctx->id->pid), TRACE_INT("fd", ctx->id->fd), TRACE_INT("direction", direction), TRACE_INT("bytes", bytes),
			TRACE_BOOL("ssl", ssl));
		return;
	}

	// stash the known protocol at this point
	enum PROTOCOL protocol = conn_info->protocol;

	TRACE_IF_ENABLED(ctx->trace_mod, ctx->id->pid, "process_data (pre-protocol)", TRACE_STRING("caller", ctx->trace_id),
		TRACE_INT("pid", ctx->id->pid), TRACE_INT("fd", ctx->id->fd), TRACE_INT("direction", direction), TRACE_INT("bytes", bytes),
		TRACE_BOOL("ssl", ssl), TRACE_INT("protocol", protocol));

	// NOTE:
	//
	// Order is important here! We need to try to detect the protocol before we submit the open event
	// because some connections (dns queries) are always reported even with directional filters set

	// if we don't know the protocol, try to detect it
	if (protocol == P_UNKNOWN)
		detect_protocol(conn_info, &buf_info, bytes);

	bool skip = false;

	// if we're skipping data ignore
	if (SKIP_DATA(ctx->id->pid))
		skip = true;

	// check if we're skipping dns
	if (conn_info->protocol == P_DNS && SKIP_DNS(ctx->id->pid))
		skip = true;

	// check if we're skipping ssl
	if (conn_info->is_ssl && SKIP_TLS(ctx->id->pid))
		skip = true;

	// check if we're skipping plaintext http
	if (!conn_info->is_ssl && SKIP_HTTP(ctx->id->pid))
		skip = true;

	if (skip) {
		conn_info->ignore = true;
		return;
	}

	// if we successfully detected the protocol, submit the protocol event
	if (protocol == P_UNKNOWN && conn_info->protocol != P_UNKNOWN)
		submit_proto_event(ctx, conn_info);

	// we only stream data if we know the protocol
	if (conn_info->protocol == P_UNKNOWN) {
		TRACE_IF_ENABLED(ctx->trace_mod, ctx->id->pid, "process_data (protocol = UNKNOWN, ignore = true)", TRACE_STRING("caller", ctx->trace_id),
			TRACE_INT("pid", ctx->id->pid), TRACE_INT("fd", ctx->id->fd), TRACE_INT("direction", direction), TRACE_INT("bytes", bytes),
			TRACE_BOOL("ssl", ssl));
		return;
	}

	// determine if we should stream
	bool stream = false;

	// if this is a DNS query, we always stream
	if (conn_info->conn_pid_id.function == C_CLIENT && conn_info->protocol == P_DNS)
		stream = true;

	// check if this is HTTP and we're configured to stream
	if ((conn_info->protocol == P_HTTP1 || conn_info->protocol == P_HTTP2) && get_stream_http_setting())
		stream = true;

	// return if we're not streaming, set to ignore and return
	if (!stream) {
		TRACE_IF_ENABLED(ctx->trace_mod, ctx->id->pid, "process_data (settings = IGNORE, ignore = true)", TRACE_STRING("caller", ctx->trace_id),
			TRACE_INT("pid", ctx->id->pid), TRACE_INT("fd", ctx->id->fd), TRACE_INT("direction", direction), TRACE_INT("bytes", bytes),
			TRACE_BOOL("ssl", ssl));

		// set ignore and return
		conn_info->ignore = true;
		return;
	}

	// initialize a socket event struct on the CPU heap
	uint32_t kZero                  = 0;
	struct socket_data_event *event = bpf_map_lookup_elem(&socket_data_event_buffer_heap, &kZero);

	// ensure we have the heap value
	if (event == NULL)
		return;

	// initialize the data event
	event->type              = S_DATA;
	event->attr.timestamp_ns = bpf_ktime_get_ns();
	event->attr.direction    = direction;
	event->attr.conn_pid_id  = conn_info->conn_pid_id;
	event->attr.cookie       = conn_info->cookie;
	event->attr.pid          = ctx->id->pid;
	event->attr.tgid         = (uint32_t)(ctx->pid_tgid & 0xFFFFFFFF);

	// If we have an iovcnt then the buffer is a 'struct iovec *':
	// struct iovec {
	//   void *iov_base; /* Starting address */
	// 	 size_t iov_len; /* Size of the memory pointed to by iov_base.
	// }

	// submit buffers
	if (args->iovcnt > 0) {
		// buffers provided as iovec, submit each individually
		size_t bytes_sent       = 0;
		const struct iovec *iov = (struct iovec *)args->buf;

		// iterate through each of the iovec buffers
		for (int i = 0; (i < LOOP_LIMIT) && (i < args->iovcnt) && (bytes_sent < bytes); ++i) {
			// read iovec
			struct iovec iov_cpy;
			bpf_probe_read_user(&iov_cpy, sizeof(struct iovec), &iov[i]);

			// calculate remaining and size
			const int bytes_remaining = bytes - bytes_sent;
			const size_t iov_size     = iov_cpy.iov_len < bytes_remaining ? iov_cpy.iov_len : bytes_remaining;

			// submit
			submit_buffer(direction, iov_cpy.iov_base, iov_size, conn_info, event);

			// tally
			bytes_sent += iov_size;
			event->attr.pos += iov_size;
		}
	} else {
		// no iovec, just pass the buffer and buffer count directly
		submit_buffer(direction, (const void *)args->buf, bytes, conn_info, event);
	}
}

// In some cases, uprobe handlers (like openssl) don't have access to the
// underlying file descriptor from the socket layer, which is needed to correlate
// data in transit with the connection information. If a request for a fd has
// been issued from a uprobe, we will oblige and update the reference with the fd.
static void respond_to_fd_request(uint64_t pid_tgid, uint32_t fd) {
	// do we have a uprobe request for a fd?
	struct fd_request *fd_request = bpf_map_lookup_elem(&uprobe_fd_requests, &pid_tgid);

	// nothing to do if there's not a request
	if (fd_request == NULL)
		return;

	// set the fd
	fd_request->fd = fd;

	// if the request is for ssl, lookup the connection info and set the ssl flag
	if (fd_request->is_ssl) {
		// init a pid_fd_key
		struct pid_fd_key id = {};
		id.pid               = pid_tgid;
		id.fd                = fd;

		// lookup the connection info
		struct conn_info *conn_info = bpf_map_lookup_elem(&conn_info_map, &id);

		// set ssl to prevent unecessary processing to detect the protocol
		if (conn_info != NULL)
			conn_info->is_ssl = true;
	}
}

// hooks
SEC("tracepoint/syscalls/sys_enter_socket")
int syscall__probe_entry_socket(struct trace_event_raw_sys_enter *ctx) {
	// extract context
	int domain   = (int)ctx->args[0];
	int type     = (int)ctx->args[1] & 0x7; // isolate lower 3 bits (standard types)
	int protocol = (int)ctx->args[2];

	// extract the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// we only care if this is a network socket
	if (!(domain == AF_INET || domain == AF_INET6))
		return 0;

	// we only care if this is a stream or datagram
	if (!(type == SOCK_STREAM || type == SOCK_DGRAM))
		return 0;

	// determine the socket type using inline function
	enum SOCKET_TYPE sock_type = determine_socket_type(protocol, type);

	// cast to an int for storage
	int type_as_int = (int)sock_type;

	// debug
	// bpf_printk("syscall__probe_entry_socket = pid: %lu, domain: %u, type: %u, protocol: %u, new type: %u\n", pid_tgid >> 32, domain, type,
	// protocol, 	type_as_int);

	// persist for the exit handler
	bpf_map_update_elem(&active_socket_args_map, &pid_tgid, &type_as_int, BPF_ANY);

	return 0;
}

SEC("tracepoint/syscalls/sys_exit_socket")
int syscall__probe_ret_socket(struct trace_event_raw_sys_exit *ctx) {
	// extract the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// extract the fd from the context
	int fd = ctx->ret;

	// grab the type from the args map
	int *type_ptr = bpf_map_lookup_elem(&active_socket_args_map, &pid_tgid);

	// if it doesn't exist there's no point continuing
	if (type_ptr == NULL)
		return 0;

	// extract type
	int type = *type_ptr;

	// clean the entry
	bpf_map_delete_elem(&active_socket_args_map, &pid_tgid);

	// we only care about successful socket creation
	if (fd <= 0)
		return 0;

	// extract the pid
	uint32_t pid = bpf_get_current_pid_tgid() >> 32;

	// debug
	// bpf_printk("syscall__probe_ret_socket = pid: %lu, fd: %d, type: %d\n", pid, fd, type);

	// generate a pid key
	struct pid_fd_key key = {};
	key.pid               = pid;
	key.fd                = fd;

	// persist the mapping
	bpf_map_update_elem(&active_socket_types, &key, &type, BPF_ANY);

	return 0;
}

SEC("tracepoint/syscalls/sys_enter_accept")
int syscall__probe_entry_accept(struct trace_event_raw_sys_enter *ctx) {
	int32_t fd        = (int)ctx->args[0];
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	struct socket_op_key s_key = {};
	s_key.pid_tgid             = pid_tgid;
	s_key.func_name            = SYS_ACCEPT;
	bpf_map_update_elem(&active_fd_args_map, &s_key, &fd, BPF_ANY);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = fd;

	struct addr_args addr_args = {};
	addr_args.addr             = (uintptr_t)ctx->args[1];

	return bpf_map_update_elem(&active_addr_args_map, &id, &addr_args, BPF_ANY);
}

SEC("tracepoint/syscalls/sys_exit_accept")
int syscall__probe_ret_accept(struct trace_event_raw_sys_exit *ctx) {
	int32_t ret_val   = ctx->ret;
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid      = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_ACCEPT;
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	bpf_map_delete_elem(&active_fd_args_map, &key);

	if (ret_val <= 0) {
		return 0;
	}

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct addr_args *addr_args = bpf_map_lookup_elem(&active_addr_args_map, &id);
	if (addr_args != NULL) {
		bpf_map_delete_elem(&active_addr_args_map, &id);

		// ensure we have a file descriptor
		if (ret_val <= 0)
			return 0;

		// set the file descriptor
		id.fd = ret_val;

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_SOCKET;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/accept");

		process_syscall_addr(&sock_ctx, addr_args, C_SERVER);
	}

	return 0;
}

SEC("tracepoint/syscalls/sys_enter_accept4")
int syscall__probe_entry_accept4(struct trace_event_raw_sys_enter *ctx) {
	int32_t fd        = (int)ctx->args[0];
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	struct socket_op_key s_key = {};
	s_key.pid_tgid             = pid_tgid;
	s_key.func_name            = SYS_ACCEPT4;
	bpf_map_update_elem(&active_fd_args_map, &s_key, &fd, BPF_ANY);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = fd;

	struct addr_args addr_args = {};
	addr_args.addr             = (uintptr_t)ctx->args[1];

	return bpf_map_update_elem(&active_addr_args_map, &id, &addr_args, BPF_ANY);
}

SEC("tracepoint/syscalls/sys_exit_accept4")
int syscall__probe_ret_accept4(struct trace_event_raw_sys_exit *ctx) {
	int32_t ret_val   = ctx->ret;
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid      = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_ACCEPT4;
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	bpf_map_delete_elem(&active_fd_args_map, &key);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct addr_args *addr_args = bpf_map_lookup_elem(&active_addr_args_map, &id);
	if (addr_args != NULL) {
		bpf_map_delete_elem(&active_addr_args_map, &id);

		// ensure we have a file descriptor
		if (ret_val <= 0) {
			return 0;
		}

		// set the file descriptor
		id.fd = ret_val;

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_SOCKET;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/accept4");

		process_syscall_addr(&sock_ctx, addr_args, C_SERVER);
	}

	return 0;
}

SEC("tracepoint/syscalls/sys_enter_connect")
int syscall__probe_entry_connect(struct trace_event_raw_sys_enter *ctx) {
	int32_t fd        = (int)ctx->args[0];
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	struct socket_op_key s_key = {};
	s_key.pid_tgid             = pid_tgid;
	s_key.func_name            = SYS_CONNECT;
	bpf_map_update_elem(&active_fd_args_map, &s_key, &fd, BPF_ANY);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = fd;

	struct addr_args addr_args = {};
	addr_args.addr             = (uintptr_t)ctx->args[1];

	return bpf_map_update_elem(&active_addr_args_map, &id, &addr_args, BPF_ANY);
}

SEC("tracepoint/syscalls/sys_exit_connect")
int syscall__probe_ret_connect(struct trace_event_raw_sys_exit *ctx) {
	int32_t ret_val   = ctx->ret;
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid      = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_CONNECT;
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	// remove the entry from the map
	bpf_map_delete_elem(&active_fd_args_map, &key);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct addr_args *addr_args = bpf_map_lookup_elem(&active_addr_args_map, &id);
	if (addr_args != NULL) {
		bpf_map_delete_elem(&active_addr_args_map, &id);

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_SOCKET;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/connect");

		process_syscall_addr(&sock_ctx, addr_args, C_CLIENT);
	}

	return 0;
}

SEC("tracepoint/syscalls/sys_enter_sendto")
int syscall__probe_entry_sendto(struct trace_event_raw_sys_enter *ctx) {
	int32_t fd        = (int)ctx->args[0];
	char *buf         = (char *)ctx->args[1];
	size_t count      = (size_t)ctx->args[2];
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	struct socket_op_key s_key = {};
	s_key.pid_tgid             = pid_tgid;
	s_key.func_name            = SYS_SENDTO;
	bpf_map_update_elem(&active_fd_args_map, &s_key, &fd, BPF_ANY);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = fd;

	struct data_args write_args = {};
	write_args.fd               = fd;
	write_args.buf              = (uintptr_t)buf;
	write_args.iovcnt           = 0;

	// share fd if uprobe has requested
	respond_to_fd_request(pid_tgid, fd);

	return bpf_map_update_elem(&active_write_args_map, &id, &write_args, BPF_ANY);
}

SEC("tracepoint/syscalls/sys_exit_sendto")
int syscall__probe_ret_sendto(struct trace_event_raw_sys_exit *ctx) {
	ssize_t bytes_count = ctx->ret;
	uint64_t pid_tgid   = bpf_get_current_pid_tgid();
	uint32_t pid        = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_SENDTO;
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	bpf_map_delete_elem(&active_fd_args_map, &key);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct data_args *write_args = bpf_map_lookup_elem(&active_write_args_map, &id);

	// nothing to do if we don't have write args
	if (write_args == NULL)
		return 0;

	if (bytes_count > 0) {
		// trace the data
		TRACE_SOCKET(pid, "syscall/sendto", TRACE_INT("pid", pid), TRACE_INT("fd", write_args->fd), TRACE_INT("bytes", bytes_count));

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_SOCKET;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/sendto");

		// process the data
		process_data(&sock_ctx, D_EGRESS, write_args, bytes_count, /* ssl */ false);
	}

	bpf_map_delete_elem(&active_write_args_map, &id);

	return 0;
}

SEC("tracepoint/syscalls/sys_exit_sendto")
int syscall__probe_ret_sendto_init(struct trace_event_raw_sys_exit *ctx) {
	ssize_t bytes_count = ctx->ret;
	uint64_t pid_tgid   = bpf_get_current_pid_tgid();
	uint32_t pid        = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_SENDTO;
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct data_args *write_args = bpf_map_lookup_elem(&active_write_args_map, &id);

	// nothing to do if we don't have write args
	if (write_args == NULL)
		return 0;

	if (bytes_count > 0) {
		// trace the data
		TRACE_SOCKET(pid, "syscall/sendto (init)", TRACE_INT("pid", pid), TRACE_INT("fd", write_args->fd), TRACE_INT("bytes", bytes_count));

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/sendto");

		// process the data
		init_conn(&sock_ctx, D_EGRESS, write_args, bytes_count);
	}

	return 0;
}

SEC("tracepoint/syscalls/sys_enter_write")
int syscall__probe_entry_write(struct trace_event_raw_sys_enter *ctx) {
	int32_t fd        = (int)ctx->args[0];
	char *buf         = (char *)ctx->args[1];
	size_t count      = (size_t)ctx->args[2];
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	struct socket_op_key s_key = {};
	s_key.pid_tgid             = pid_tgid;
	s_key.func_name            = SYS_WRITE;
	bpf_map_update_elem(&active_fd_args_map, &s_key, &fd, BPF_ANY);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = fd;

	struct data_args write_args = {};
	write_args.fd               = fd;
	write_args.buf              = (uintptr_t)buf;
	write_args.iovcnt           = 0;

	// share fd if uprobe has requested
	respond_to_fd_request(pid_tgid, fd);

	return bpf_map_update_elem(&active_write_args_map, &id, &write_args, BPF_ANY);
}

SEC("tracepoint/syscalls/sys_exit_write")
int syscall__probe_ret_write(struct trace_event_raw_sys_exit *ctx) {
	ssize_t bytes_count = ctx->ret;
	uint64_t pid_tgid   = bpf_get_current_pid_tgid();
	uint32_t pid        = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_WRITE;
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	bpf_map_delete_elem(&active_fd_args_map, &key);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct data_args *write_args = bpf_map_lookup_elem(&active_write_args_map, &id);

	// nothing to do if we don't have write args
	if (write_args == NULL)
		return 0;

	if (bytes_count > 0) {
		// trace
		TRACE_SOCKET(pid, "syscall/write", TRACE_INT("pid", pid), TRACE_INT("fd", write_args->fd), TRACE_INT("bytes", bytes_count));

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_SOCKET;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/write");

		// process the data
		process_data(&sock_ctx, D_EGRESS, write_args, bytes_count, /* ssl */ false);
	}

	bpf_map_delete_elem(&active_write_args_map, &id);

	return 0;
}

SEC("tracepoint/syscalls/sys_exit_write")
int syscall__probe_ret_write_init(struct trace_event_raw_sys_exit *ctx) {
	ssize_t bytes_count = ctx->ret;
	uint64_t pid_tgid   = bpf_get_current_pid_tgid();
	uint32_t pid        = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_WRITE;
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct data_args *write_args = bpf_map_lookup_elem(&active_write_args_map, &id);

	// nothing to do if we don't have write args
	if (write_args == NULL)
		return 0;

	if (bytes_count > 0) {
		// trace
		TRACE_SOCKET(pid, "syscall/write (init)", TRACE_INT("pid", pid), TRACE_INT("fd", write_args->fd), TRACE_INT("bytes", bytes_count));

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/write");

		// process the data
		init_conn(&sock_ctx, D_EGRESS, write_args, bytes_count);
	}

	return 0;
}

SEC("tracepoint/syscalls/sys_enter_writev")
int syscall__probe_entry_writev(struct trace_event_raw_sys_enter *ctx) {
	int32_t fd              = (int)ctx->args[0];
	const struct iovec *iov = (const struct iovec *)ctx->args[1];
	int iovcnt              = (int)ctx->args[2];
	uint64_t pid_tgid       = bpf_get_current_pid_tgid();

	struct socket_op_key s_key = {};
	s_key.pid_tgid             = pid_tgid;
	s_key.func_name            = SYS_WRITEV; // Make sure this is defined appropriately
	bpf_map_update_elem(&active_fd_args_map, &s_key, &fd, BPF_ANY);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = fd;

	struct data_args writev_args = {};
	writev_args.fd               = fd;
	writev_args.buf              = (uintptr_t)iov; // Storing the pointer to the iovec array directly
	writev_args.iovcnt           = iovcnt; // Storing iovcnt for future use in exit tracepoint

	// share fd if uprobe has requested
	respond_to_fd_request(pid_tgid, fd);

	return bpf_map_update_elem(&active_write_args_map, &id, &writev_args, BPF_ANY);
}

SEC("tracepoint/syscalls/sys_exit_writev")
int syscall__probe_ret_writev(struct trace_event_raw_sys_exit *ctx) {
	ssize_t bytes_count = ctx->ret;
	uint64_t pid_tgid   = bpf_get_current_pid_tgid();
	uint32_t pid        = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_WRITEV; // Ensure consistency with entry tracepoint
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	bpf_map_delete_elem(&active_fd_args_map, &key);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct data_args *writev_args = bpf_map_lookup_elem(&active_write_args_map, &id);

	// nothing to do if we don't have writev args
	if (writev_args == NULL)
		return 0;

	if (bytes_count > 0) {
		// trace
		TRACE_SOCKET(pid, "syscall/writev", TRACE_INT("pid", pid), TRACE_INT("fd", writev_args->fd), TRACE_INT("bytes", bytes_count));

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_SOCKET;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/writev");

		// process the data
		process_data(&sock_ctx, D_EGRESS, writev_args, bytes_count, /* ssl */ false);
	}

	bpf_map_delete_elem(&active_write_args_map, &id);

	return 0;
}

SEC("tracepoint/syscalls/sys_exit_writev")
int syscall__probe_ret_writev_init(struct trace_event_raw_sys_exit *ctx) {
	ssize_t bytes_count = ctx->ret;
	uint64_t pid_tgid   = bpf_get_current_pid_tgid();
	uint32_t pid        = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_WRITEV; // Ensure consistency with entry tracepoint
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct data_args *writev_args = bpf_map_lookup_elem(&active_write_args_map, &id);

	// nothing to do if we don't have writev args
	if (writev_args == NULL)
		return 0;

	if (bytes_count > 0) {
		// trace
		TRACE_SOCKET(pid, "syscall/writev (init)", TRACE_INT("pid", pid), TRACE_INT("fd", writev_args->fd), TRACE_INT("bytes", bytes_count));

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_SOCKET;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/writev");

		// process the data
		init_conn(&sock_ctx, D_EGRESS, writev_args, bytes_count);
	}

	return 0;
}

SEC("tracepoint/syscalls/sys_enter_recvfrom")
int syscall__probe_entry_recvfrom(struct trace_event_raw_sys_enter *ctx) {
	int32_t fd        = (int)ctx->args[0];
	char *buf         = (char *)ctx->args[1];
	size_t count      = (size_t)ctx->args[2];
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	struct socket_op_key s_key = {};
	s_key.pid_tgid             = pid_tgid;
	s_key.func_name            = SYS_RECVFROM;
	bpf_map_update_elem(&active_fd_args_map, &s_key, &fd, BPF_ANY);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = fd;

	struct data_args read_args = {};
	read_args.fd               = fd;
	read_args.buf              = (uintptr_t)buf;
	read_args.iovcnt           = 0;

	// share fd if uprobe has requested
	respond_to_fd_request(pid_tgid, fd);

	return bpf_map_update_elem(&active_read_args_map, &id, &read_args, BPF_ANY);
}

SEC("tracepoint/syscalls/sys_exit_recvfrom")
int syscall__probe_ret_recvfrom(struct trace_event_raw_sys_exit *ctx) {
	ssize_t bytes_count = ctx->ret;
	uint64_t pid_tgid   = bpf_get_current_pid_tgid();
	uint32_t pid        = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_RECVFROM;
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	bpf_map_delete_elem(&active_fd_args_map, &key);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct data_args *read_args = bpf_map_lookup_elem(&active_read_args_map, &id);

	// nothing to do if we don't have read args
	if (read_args == NULL)
		return 0;

	if (bytes_count > 0) {
		// trace
		TRACE_SOCKET(pid, "syscall/recvfrom", TRACE_INT("pid", pid), TRACE_INT("fd", read_args->fd), TRACE_INT("bytes", bytes_count));

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_SOCKET;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/recvfrom");

		// process the data
		process_data(&sock_ctx, D_INGRESS, read_args, bytes_count, /* ssl */ false);
	}

	bpf_map_delete_elem(&active_read_args_map, &id);

	return 0;
}

SEC("tracepoint/syscalls/sys_exit_recvfrom")
int syscall__probe_ret_recvfrom_init(struct trace_event_raw_sys_exit *ctx) {
	ssize_t bytes_count = ctx->ret;
	uint64_t pid_tgid   = bpf_get_current_pid_tgid();
	uint32_t pid        = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_RECVFROM;
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct data_args *read_args = bpf_map_lookup_elem(&active_read_args_map, &id);

	// nothing to do if we don't have read args
	if (read_args == NULL)
		return 0;

	if (bytes_count > 0) {
		// trace
		TRACE_SOCKET(pid, "syscall/recvfrom (init)", TRACE_INT("pid", pid), TRACE_INT("fd", read_args->fd), TRACE_INT("bytes", bytes_count));

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/recvfrom");

		// process the data
		init_conn(&sock_ctx, D_INGRESS, read_args, bytes_count);
	}

	return 0;
}

SEC("tracepoint/syscalls/sys_enter_read")
int syscall__probe_entry_read(struct trace_event_raw_sys_enter *ctx) {
	int32_t fd        = (int)ctx->args[0];
	char *buf         = (char *)ctx->args[1];
	size_t count      = (size_t)ctx->args[2];
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	struct socket_op_key s_key = {};
	s_key.pid_tgid             = pid_tgid;
	s_key.func_name            = SYS_READ;
	bpf_map_update_elem(&active_fd_args_map, &s_key, &fd, BPF_ANY);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = fd;

	struct data_args read_args = {};
	read_args.fd               = fd;
	read_args.buf              = (uintptr_t)buf;
	read_args.iovcnt           = 0;

	return bpf_map_update_elem(&active_read_args_map, &id, &read_args, BPF_ANY);
}

SEC("tracepoint/syscalls/sys_exit_read")
int syscall__probe_ret_read(struct trace_event_raw_sys_exit *ctx) {
	ssize_t bytes_count = ctx->ret;
	uint64_t pid_tgid   = bpf_get_current_pid_tgid();
	uint32_t pid        = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_READ;
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	bpf_map_delete_elem(&active_fd_args_map, &key);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct data_args *read_args = bpf_map_lookup_elem(&active_read_args_map, &id);

	// nothing to do if we don't have read args
	if (read_args == NULL)
		return 0;

	if (bytes_count > 0) {
		// trace
		TRACE_SOCKET(pid, "syscall/read", TRACE_INT("pid", pid), TRACE_INT("fd", read_args->fd), TRACE_INT("bytes", bytes_count));

		// share fd if uprobe has requested
		respond_to_fd_request(pid_tgid, *fd);

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_SOCKET;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/read");

		// process the data
		process_data(&sock_ctx, D_INGRESS, read_args, bytes_count, /* ssl */ false);
	}

	bpf_map_delete_elem(&active_read_args_map, &id);

	return 0;
}

SEC("tracepoint/syscalls/sys_exit_read")
int syscall__probe_ret_read_init(struct trace_event_raw_sys_exit *ctx) {
	ssize_t bytes_count = ctx->ret;
	uint64_t pid_tgid   = bpf_get_current_pid_tgid();
	uint32_t pid        = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_READ;
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct data_args *read_args = bpf_map_lookup_elem(&active_read_args_map, &id);

	// nothing to do if we don't have read args
	if (read_args == NULL)
		return 0;

	if (bytes_count > 0) {
		// trace
		TRACE_SOCKET(pid, "syscall/read (init)", TRACE_INT("pid", pid), TRACE_INT("fd", read_args->fd), TRACE_INT("bytes", bytes_count));

		// share fd if uprobe has requested
		respond_to_fd_request(pid_tgid, *fd);

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/read");

		// process the data
		init_conn(&sock_ctx, D_INGRESS, read_args, bytes_count);
	}

	return 0;
}

SEC("tracepoint/syscalls/sys_enter_readv")
int syscall__probe_entry_readv(struct trace_event_raw_sys_enter *ctx) {
	int32_t fd              = (int)ctx->args[0];
	const struct iovec *iov = (const struct iovec *)ctx->args[1];
	int iovcnt              = (int)ctx->args[2];
	uint64_t pid_tgid       = bpf_get_current_pid_tgid();

	struct socket_op_key s_key = {};
	s_key.pid_tgid             = pid_tgid;
	s_key.func_name            = SYS_READV; // Ensure this is defined in your enum or constants
	bpf_map_update_elem(&active_fd_args_map, &s_key, &fd, BPF_ANY);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = fd;

	struct data_args readv_args = {};
	readv_args.fd               = fd;
	readv_args.buf              = (uintptr_t)iov; // Storing the pointer to the iovec array directly
	readv_args.iovcnt           = iovcnt; // Storing iovcnt for potential use in exit tracepoint

	return bpf_map_update_elem(&active_read_args_map, &id, &readv_args, BPF_ANY);
}

SEC("tracepoint/syscalls/sys_exit_readv")
int syscall__probe_ret_readv(struct trace_event_raw_sys_exit *ctx) {
	ssize_t bytes_count = ctx->ret;
	uint64_t pid_tgid   = bpf_get_current_pid_tgid();
	uint32_t pid        = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_READV; // Match with entry tracepoint
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	bpf_map_delete_elem(&active_fd_args_map, &key);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct data_args *readv_args = bpf_map_lookup_elem(&active_read_args_map, &id);

	// nothing to do if we don't have readv args
	if (readv_args == NULL)
		return 0;

	if (bytes_count > 0) {
		// trace
		TRACE_SOCKET(pid, "syscall/readv", TRACE_INT("pid", pid), TRACE_INT("fd", readv_args->fd), TRACE_INT("bytes", bytes_count));

		respond_to_fd_request(pid_tgid, *fd);

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_SOCKET;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/readv");

		// process the data
		process_data(&sock_ctx, D_INGRESS, readv_args, bytes_count, /* ssl */ false);
	}

	bpf_map_delete_elem(&active_read_args_map, &id);

	return 0;
}

SEC("tracepoint/syscalls/sys_exit_readv")
int syscall__probe_ret_readv_init(struct trace_event_raw_sys_exit *ctx) {
	ssize_t bytes_count = ctx->ret;
	uint64_t pid_tgid   = bpf_get_current_pid_tgid();
	uint32_t pid        = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_READV; // Match with entry tracepoint
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct data_args *readv_args = bpf_map_lookup_elem(&active_read_args_map, &id);

	// nothing to do if we don't have readv args
	if (readv_args == NULL)
		return 0;

	if (bytes_count > 0) {
		// trace
		TRACE_SOCKET(pid, "syscall/readv (init)", TRACE_INT("pid", pid), TRACE_INT("fd", readv_args->fd), TRACE_INT("bytes", bytes_count));

		respond_to_fd_request(pid_tgid, *fd);

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_SOCKET;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/readv");

		// process the data
		init_conn(&sock_ctx, D_INGRESS, readv_args, bytes_count);
	}

	return 0;
}

SEC("tracepoint/syscalls/sys_enter_close")
int syscall__probe_entry_close(struct trace_event_raw_sys_enter *ctx) {
	int32_t fd        = (int)ctx->args[0];
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid      = pid_tgid >> 32;

	struct socket_op_key s_key = {};
	s_key.pid_tgid             = pid_tgid;
	s_key.func_name            = SYS_CLOSE;
	bpf_map_update_elem(&active_fd_args_map, &s_key, &fd, BPF_ANY);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = fd;

	struct close_args close_args = {};
	close_args.fd                = fd;

	// remove the socket type map entry (set during 'socket()')
	bpf_map_delete_elem(&active_socket_types, &id);

	// persist the close args
	bpf_map_update_elem(&active_close_args_map, &id, &close_args, BPF_ANY);

	return 0;
}

SEC("tracepoint/syscalls/sys_exit_close")
int syscall__probe_ret_close(struct trace_event_raw_sys_exit *ctx) {
	int ret_val       = ctx->ret;
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid      = pid_tgid >> 32;

	struct socket_op_key key = {};
	key.pid_tgid             = pid_tgid;
	key.func_name            = SYS_CLOSE;
	int32_t *fd              = bpf_map_lookup_elem(&active_fd_args_map, &key);

	if (fd == NULL) {
		return 0;
	}

	bpf_map_delete_elem(&active_fd_args_map, &key);

	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = *fd;

	struct close_args *close_args = bpf_map_lookup_elem(&active_close_args_map, &id);
	if (close_args != NULL) {
		bpf_map_delete_elem(&active_close_args_map, &id);

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_SOCKET;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "syscall/close");

		process_close(&sock_ctx);
	}

	return 0;
}
