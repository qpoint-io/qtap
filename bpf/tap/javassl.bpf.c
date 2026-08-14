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

#include "common.bpf.h"
#include "socket.bpf.h"
#include "trace.bpf.h"
#include "bpf_helpers.h"
#include "settings.bpf.h"
#include "buffers.bpf.h"

// struct to hold the read/write args
struct java_ssl_args {
	// File descriptor
	int32_t fd;
	// Point to the byte buffer
	uintptr_t buf;
	// Length of data to read/write
	int32_t len;
};

// struct to hold the engine data (for SSLEngine correlation)
struct java_ssl_engine_data {
	uint8_t *plaintext_buf;
	int32_t plaintext_len;
	uint8_t *encrypted_buf;
	int32_t encrypted_len;
	uint8_t *session_id_buf;
	int32_t session_id_len;
	enum DIRECTION direction;
};

// SSLEngine events
enum JAVA_SSL_ENGINE_EVENT {
	J_DATA          = 1ULL,
	J_CORRELATE     = 2ULL,
	J_SOCKET_CLOSED = 3ULL,
};

// SSLEngine data type (plaintext or encrypted)
enum JAVA_SSL_ENGINE_DATA_TYPE {
	J_PLAINTEXT = 1ULL,
	J_ENCRYPTED = 2ULL,
};

// SSLEngine data event (from wrap/unwrap uprobes)
struct java_ssl_engine_data_event {
	// Event type
	uint64_t type;

	// The SSLEngine data attributes
	struct java_ssl_engine_data_attr_t {
		// The time of the event
		uint64_t timestamp_ns;
		// Process PID
		uint32_t pid;
		// The SSLEngine session ID
		uint64_t session_id;
		// The SSLEngine direction
		enum DIRECTION direction;
		// The SSLEngine data type
		enum JAVA_SSL_ENGINE_DATA_TYPE data_type;
		// The SSLEngine data size
		uint32_t msg_size;
	} attr;

	// The SSLEngine data
	char msg[MAX_MSG_SIZE];
};

// SSLEngine correlate event (from syscall probes)
struct java_ssl_engine_correlate_event {
	// Event type
	uint64_t type;

	// The SSLEngine data attributes
	struct java_ssl_engine_correlate_attr_t {
		// The time of the event
		uint64_t timestamp_ns;
		// Process PID
		uint32_t pid;
		// File descriptor
		int32_t fd;
		// Socket cookie
		uint64_t cookie;
		// The syscall direction
		enum DIRECTION direction;
		// The SSLEngine data size
		uint32_t msg_size;
	} attr;

	// The SSLEngine data
	char msg[MAX_MSG_SIZE];
};

// SSLEngine socket closed event
struct java_ssl_engine_socket_closed_event {
	// Event type
	uint64_t type;
	// Process PID
	uint32_t pid;
	// File descriptor
	int32_t fd;
};

// union of all SSLEngine events
union java_ssl_engine_event {
	struct java_ssl_engine_data_event data;
	struct java_ssl_engine_correlate_event correlate;
};

// persist the read args for exit handler (SSLSocket)
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uint64_t); // pid_tgid
	__type(value, struct java_ssl_args);
	__uint(max_entries, 1024);
} java_active_ssl_read_args_map SEC(".maps");

// persist the write args for exit handler (SSLSocket)
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uint64_t); // pid_tgid
	__type(value, struct java_ssl_args);
	__uint(max_entries, 1024);
} java_active_ssl_write_args_map SEC(".maps");

// persist the args for the syscall exit handlers (SSLEngine)
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uint64_t); // pid_tgid
	__type(value, struct data_args);
	__uint(max_entries, 1024);
} java_active_ssl_syscall_args_map SEC(".maps");

// map java process pids for SSLEngine (so we know if we need to correlate)
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uint32_t); // pid
	__type(value, bool); // is java process
	__uint(max_entries, 1024);
} java_process_pid_map SEC(".maps");

// SSLEngine ignore map (so we can ignore sessions when qtap doesn't want)
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uint64_t); // session_id
	__type(value, bool); // ignore
	__uint(max_entries, 1024);
} java_ssl_engine_session_ignore_map SEC(".maps");

// SSLEngine uprobe correlated map (correlation data sent from uprobe)
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uint64_t); // session_id
	__type(value, bool); // correlated
	__uint(max_entries, 1024);
} java_ssl_engine_uprobe_correlated_map SEC(".maps");

// SSLEngine syscall correlated map (correlation data sent from syscall)
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, struct pid_fd_key); // pid_fd_key
	__type(value, bool); // correlated
	__uint(max_entries, 1024);
} java_ssl_engine_syscall_correlated_map SEC(".maps");

// per-cpu array heap for SSLEngine events
struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__type(key, uint32_t); // cpu
	__type(value, union java_ssl_engine_event); // event
	__uint(max_entries, 1);
} java_ssl_engine_event_heap SEC(".maps");

// SSLEngine events ringbuffer (for qtap to read)
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 256 * 1024 /* 256 KB */);
} java_ssl_engine_events SEC(".maps");

// request the fd from socket syscall layer (SSLSocket)
static void request_java_fd_from_syscall(uint64_t pid_tgid) {
	// initialize a fd request
	struct fd_request fd_request = {};
	fd_request.is_ssl            = true;

	// persist for syscall
	bpf_map_update_elem(&uprobe_fd_requests, &pid_tgid, &fd_request, BPF_ANY);
}

// retrieve fd from socket syscall layer (SSLSocket)
static int32_t get_java_fd_from_syscall(uint64_t pid_tgid) {
	// extract the pid
	uint32_t pid = pid_tgid >> 32;

	// do we have a userspace request for a fd?
	struct fd_request *fd_request = bpf_map_lookup_elem(&uprobe_fd_requests, &pid_tgid);

	// nothing to do if there's not a request
	if (fd_request == NULL) {
		TRACE_JAVASSL(pid, "javassl.get_java_fd_from_syscall (no request found)", TRACE_INT("pid", pid), TRACE_INT("pid_tgid", pid_tgid));
		return 0;
	}

	// clean the request
	bpf_map_delete_elem(&uprobe_fd_requests, &pid_tgid);

	// return the fd
	return fd_request->fd;
}

SEC("uprobe/java_ssl_read_entry")
int BPF_UPROBE(java_ssl_read_entry, void *buf, int offset, int len) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	request_java_fd_from_syscall(pid_tgid);

	// ensure length is positive
	if (len <= 0)
		return 0;

	struct java_ssl_args read_args = {};
	read_args.buf                  = (uintptr_t)buf + offset;
	read_args.len                  = len;

	bpf_map_update_elem(&java_active_ssl_read_args_map, &pid_tgid, &read_args, BPF_ANY);
	return 0;
}

SEC("uprobe/java_ssl_read_exit")
int BPF_UPROBE(java_ssl_read_exit, int bytes_count) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	struct java_ssl_args *read_args = bpf_map_lookup_elem(&java_active_ssl_read_args_map, &pid_tgid);
	if (!read_args || bytes_count <= 0)
		goto cleanup;

	read_args->fd = get_java_fd_from_syscall(pid_tgid);
	if (read_args->fd == 0)
		goto cleanup;

	// construct a pid_fd_key
	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = read_args->fd;

	// initialize a socket context
	struct socket_ctx sock_ctx = {};
	sock_ctx.id                = &id;
	sock_ctx.pid_tgid          = pid_tgid;
	sock_ctx.trace_mod         = QTAP_JAVASSL;
	bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "javassl/read");

	// convert read_args to data_args
	struct data_args data_args = {};
	data_args.fd               = read_args->fd;
	data_args.buf              = read_args->buf;

	process_data(&sock_ctx, D_INGRESS, &data_args, bytes_count, /* ssl */ true);

cleanup:
	bpf_map_delete_elem(&java_active_ssl_read_args_map, &pid_tgid);
	return 0;
}

SEC("uprobe/java_ssl_write_entry")
int BPF_UPROBE(java_ssl_write_entry, void *buf, int offset, int len) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	request_java_fd_from_syscall(pid_tgid);

	// ensure length is positive
	if (len <= 0)
		return 0;

	struct java_ssl_args write_args = {};
	write_args.buf                  = (uintptr_t)buf + offset;
	write_args.len                  = len;

	bpf_map_update_elem(&java_active_ssl_write_args_map, &pid_tgid, &write_args, BPF_ANY);
	return 0;
}

SEC("uprobe/java_ssl_write_exit")
int BPF_UPROBE(java_ssl_write_exit) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	struct java_ssl_args *write_args = bpf_map_lookup_elem(&java_active_ssl_write_args_map, &pid_tgid);
	if (!write_args)
		return 0;

	write_args->fd = get_java_fd_from_syscall(pid_tgid);
	if (write_args->fd == 0)
		goto cleanup;

	// construct a pid_fd_key
	struct pid_fd_key id = {};
	id.pid               = pid_tgid >> 32;
	id.fd                = write_args->fd;

	// initialize a socket context
	struct socket_ctx sock_ctx = {};
	sock_ctx.id                = &id;
	sock_ctx.pid_tgid          = pid_tgid;
	sock_ctx.trace_mod         = QTAP_JAVASSL;
	bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "javassl/write");

	// convert write_args to data_args
	struct data_args data_args = {};
	data_args.fd               = write_args->fd;
	data_args.buf              = write_args->buf;

	process_data(&sock_ctx, D_EGRESS, &data_args, write_args->len, /* ssl */ true);

cleanup:
	bpf_map_delete_elem(&java_active_ssl_write_args_map, &pid_tgid);
	return 0;
}

// compute hash of session ID for consistent key generation
static __always_inline uint64_t compute_session_hash(void *session_id, int len) {
	if (len <= 0) {
		return 0;
	}

	// Simple hash function - use first 8 bytes if available, otherwise pad
	uint64_t hash = 0;
	int copy_len  = len;
	if (copy_len > 8) {
		copy_len = 8;
	}

	if (bpf_probe_read_user(&hash, copy_len, session_id) != 0) {
		return 0;
	}

	// XOR with length to make it more unique
	hash ^= (uint64_t)len;

	return hash;
}

// submit a single chunk from the buffer to the ringbuffer
static __always_inline void submit_engine_data_chunk(const void *buf, size_t buf_size, struct java_ssl_engine_data_event *event) {
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
		// submit the event to the ring buffer
		bpf_ringbuf_output(&java_ssl_engine_events, event, sizeof(event->type) + sizeof(event->attr) + amount_copied, 0);
	}
}

// submit the entire buffer to the ringbuffer
static __always_inline void submit_engine_data(const void *buf, const size_t size, struct java_ssl_engine_data_event *ev) {
	int bytes_sent = 0;
	unsigned int i;

	// we have to break the buffer into chunks and submit the chunks
	for (i = 0; i < BUF_CHUNK_LIMIT; ++i) {
		const int bytes_remaining = size - bytes_sent;
		const size_t current_size = (bytes_remaining > MAX_MSG_SIZE && (i != BUF_CHUNK_LIMIT - 1)) ? MAX_MSG_SIZE : bytes_remaining;

		// submit the chunk
		submit_engine_data_chunk(buf + bytes_sent, current_size, ev);

		// determine progress
		bytes_sent += current_size;
		if (size == bytes_sent) {
			return;
		}
	}
}

// common helper for engine uprobe logic with session-based correlation
static __always_inline int handle_engine_data(struct java_ssl_engine_data *ctx) {
	// validate requiredparameters
	if (!ctx || !ctx->plaintext_buf || !ctx->session_id_buf || ctx->plaintext_len == 0 || ctx->session_id_len == 0) {
		return 0;
	}

	// extract the pid
	uint32_t pid = bpf_get_current_userspace_pid();

	// compute session hash for correlation
	uint64_t session_hash = compute_session_hash(ctx->session_id_buf, ctx->session_id_len);
	if (session_hash == 0) {
		return 0;
	}

	// check to see if we should ignore this session
	bool ignore = bpf_map_lookup_elem(&java_ssl_engine_session_ignore_map, &session_hash);
	if (ignore) {
		return 0;
	}

	// initialize a data event struct on the CPU heap
	uint32_t kZero                           = 0;
	struct java_ssl_engine_data_event *event = (struct java_ssl_engine_data_event *)bpf_map_lookup_elem(&java_ssl_engine_event_heap, &kZero);

	// ensure we have the heap value
	if (event == NULL)
		return 0;

	// check to see if we have a correlation for this session
	bool correlated = bpf_map_lookup_elem(&java_ssl_engine_uprobe_correlated_map, &session_hash);

	// we need to correlate this session by sending the encrypted data to the user space application if we haven't already
	if (!correlated && ctx->encrypted_buf && ctx->encrypted_len > 0) {
		event->type              = J_DATA;
		event->attr.timestamp_ns = bpf_ktime_get_ns();
		event->attr.pid          = pid;
		event->attr.session_id   = session_hash;
		event->attr.direction    = ctx->direction;
		event->attr.data_type    = J_ENCRYPTED;

		// submit the encrypted data
		submit_engine_data(ctx->encrypted_buf, ctx->encrypted_len, event);
	}

	// prepare the event for plaintext data
	event->type              = J_DATA;
	event->attr.timestamp_ns = bpf_ktime_get_ns();
	event->attr.session_id   = session_hash;
	event->attr.direction    = ctx->direction;
	event->attr.data_type    = J_PLAINTEXT;

	// submit the plaintext data
	submit_engine_data(ctx->plaintext_buf, ctx->plaintext_len, event);

	return 0;
}

SEC("uprobe/java_ssl_engine_wrap_exit")
int BPF_UPROBE(java_ssl_engine_wrap_exit, void *plaintext_buf, int plaintext_len, void *encrypted_buf, int encrypted_len, void *session_id_buf,
	int session_id_len) {
	// wrap = plaintext going out (egress)
	struct java_ssl_engine_data engine_ctx = {
		.plaintext_buf  = plaintext_buf,
		.plaintext_len  = plaintext_len,
		.encrypted_buf  = encrypted_buf,
		.encrypted_len  = encrypted_len,
		.session_id_buf = session_id_buf,
		.session_id_len = session_id_len,
		.direction      = D_EGRESS,
	};

	// handle the engine data
	return handle_engine_data(&engine_ctx);
}

SEC("uprobe/java_ssl_engine_unwrap_exit")
int BPF_UPROBE(java_ssl_engine_unwrap_exit, void *encrypted_buf, int encrypted_len, void *plaintext_buf, int plaintext_len, void *session_id_buf,
	int session_id_len) {
	// unwrap = plaintext coming in (ingress)
	struct java_ssl_engine_data engine_ctx = {
		.plaintext_buf  = plaintext_buf,
		.plaintext_len  = plaintext_len,
		.encrypted_buf  = encrypted_buf,
		.encrypted_len  = encrypted_len,
		.session_id_buf = session_id_buf,
		.session_id_len = session_id_len,
		.direction      = D_INGRESS,
	};

	// handle the engine data
	return handle_engine_data(&engine_ctx);
}

// detect if a buffer is a TLS app data record
static __always_inline int detect_tls_app_data(struct buf_info *buf_info, size_t count) {
	if (count < 5) {
		return 0; // Not enough data for TLS header
	}

	char header[5];
	if (buf_read(header, 5, buf_info, 0) != 5) {
		return 0; // Failed to read header
	}

	// Check for TLS app data: type=0x17
	if (header[0] == 0x17) {
		return 1; // TLS app data detected
	}

	return 0; // Not TLS app data
}

// submit a single chunk from the buffer to the ringbuffer
static __always_inline void submit_engine_correlation_chunk(const void *buf, size_t buf_size, struct java_ssl_engine_correlate_event *event) {
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
		bpf_ringbuf_output(&java_ssl_engine_events, event, sizeof(event->type) + sizeof(event->attr) + amount_copied, 0);
	}
}

// submit the entire buffer to the ringbuffer
static void submit_engine_correlation(const void *buf, const size_t size, struct java_ssl_engine_correlate_event *ev) {
	int bytes_sent = 0;
	unsigned int i;

	// we have to break the buffer into chunks and submit the chunks
	for (i = 0; i < BUF_CHUNK_LIMIT; ++i) {
		const int bytes_remaining = size - bytes_sent;
		const size_t current_size = (bytes_remaining > MAX_MSG_SIZE && (i != BUF_CHUNK_LIMIT - 1)) ? MAX_MSG_SIZE : bytes_remaining;

		// submit the chunk
		submit_engine_correlation_chunk(buf + bytes_sent, current_size, ev);

		// determine progress
		bytes_sent += current_size;
		if (size == bytes_sent) {
			return;
		}
	}
}

// handle SSLEngine correlation during syscalls with simple buffering approach
static __always_inline int handle_engine_correlation(int fd, struct buf_info *buf_info, size_t count, uint8_t direction, char *syscall_name) {
	// extract the pid
	uint32_t pid = bpf_get_current_userspace_pid();

	// ensure this is a java process
	bool is_java = bpf_map_lookup_elem(&java_process_pid_map, &pid);
	if (!is_java) {
		return 0;
	}

	// skip stdout/stderr/stdin (FD 0, 1, 2) - focus on network sockets
	if (fd <= 2) {
		return 0;
	}

	// check to see if we already have a correlation for this pid + fd
	struct pid_fd_key key = {
		.pid = pid,
		.fd  = fd,
	};
	bool correlated = bpf_map_lookup_elem(&java_ssl_engine_syscall_correlated_map, &key);
	if (correlated) {
		return 0;
	}

	// we're looking for TLS app data records for this pid + fd to correlate with the uprobe
	if (!detect_tls_app_data(buf_info, count)) {
		return 0;
	}

	// load the conn_info for this pid + fd
	struct conn_info *conn_info = bpf_map_lookup_elem(&conn_info_map, &key);
	if (!conn_info) {
		return 0;
	}

	// initialize a correlate event struct on the CPU heap
	uint32_t kZero = 0;
	struct java_ssl_engine_correlate_event *event =
		(struct java_ssl_engine_correlate_event *)bpf_map_lookup_elem(&java_ssl_engine_event_heap, &kZero);

	// ensure we have the heap value
	if (event == NULL)
		return 0;

	// set the meta
	event->type              = J_CORRELATE;
	event->attr.timestamp_ns = bpf_ktime_get_ns();
	event->attr.pid          = pid;
	event->attr.fd           = fd;
	event->attr.cookie       = conn_info->cookie;
	event->attr.direction    = direction;

	// submit the data
	if (buf_info->iovcnt > 0) {
		// buffers provided as iovec, submit each individually
		size_t bytes_sent       = 0;
		const struct iovec *iov = (struct iovec *)buf_info->buf;

		// iterate through each of the iovec buffers
		for (int i = 0; (i < LOOP_LIMIT) && (i < buf_info->iovcnt) && (bytes_sent < count); ++i) {
			// read iovec
			struct iovec iov_cpy;
			bpf_probe_read_user(&iov_cpy, sizeof(struct iovec), &iov[i]);

			// calculate remaining and size
			const int bytes_remaining = count - bytes_sent;
			const size_t iov_size     = iov_cpy.iov_len < bytes_remaining ? iov_cpy.iov_len : bytes_remaining;

			// submit
			submit_engine_correlation(iov_cpy.iov_base, iov_size, event);

			// tally
			bytes_sent += iov_size;
		}
	} else {
		submit_engine_correlation(buf_info->buf, count, event);
	}

	return 0;
}

// Syscall tracepoint correlation probes for SSLEngine

// These run alongside existing syscall tracepoints to handle engine correlation
SEC("tracepoint/syscalls/sys_enter_write")
int java_ssl_engine_sys_enter_write(struct trace_event_raw_sys_enter *ctx) {
	// extract the args
	int fd       = (int)ctx->args[0];
	void *buf    = (void *)ctx->args[1];
	size_t count = (size_t)ctx->args[2];

	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// construct the data_args
	struct data_args data_args = {};
	data_args.fd               = fd;
	data_args.buf              = (uintptr_t)buf;
	data_args.iovcnt           = 0;

	bpf_map_update_elem(&java_active_ssl_syscall_args_map, &pid_tgid, &data_args, BPF_ANY);

	return 0;
}

SEC("tracepoint/syscalls/sys_exit_write")
int java_ssl_engine_sys_exit_write(struct trace_event_raw_sys_exit *ctx) {
	// extract the write length returned by the syscall
	ssize_t bytes_count = ctx->ret;

	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// get the data_args
	struct data_args *data_args = bpf_map_lookup_elem(&java_active_ssl_syscall_args_map, &pid_tgid);
	if (!data_args) {
		return 0;
	}

	// construct the buf_info
	struct buf_info buf_info = {
		.buf    = (const void *)(uintptr_t)data_args->buf,
		.iovcnt = data_args->iovcnt,
	};

	if (bytes_count > 0) {
		// handle the engine correlation
		handle_engine_correlation(data_args->fd, &buf_info, bytes_count, D_EGRESS, "sys_exit_write");
	}

	// delete the data_args
	bpf_map_delete_elem(&java_active_ssl_syscall_args_map, &pid_tgid);

	return 0;
}

SEC("tracepoint/syscalls/sys_enter_read")
int java_ssl_engine_sys_enter_read(struct trace_event_raw_sys_enter *ctx) {
	// extract the args
	int fd       = (int)ctx->args[0];
	void *buf    = (void *)ctx->args[1];
	size_t count = (size_t)ctx->args[2];

	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// construct the data_args
	struct data_args data_args = {};
	data_args.fd               = fd;
	data_args.buf              = (uintptr_t)buf;
	data_args.iovcnt           = 0;

	// persist the data_args
	bpf_map_update_elem(&java_active_ssl_syscall_args_map, &pid_tgid, &data_args, BPF_ANY);

	return 0;
}

SEC("tracepoint/syscalls/sys_exit_read")
int java_ssl_engine_sys_exit_read(struct trace_event_raw_sys_exit *ctx) {
	// extract the read length returned by the syscall
	ssize_t bytes_count = ctx->ret;

	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// get the data_args
	struct data_args *data_args = bpf_map_lookup_elem(&java_active_ssl_syscall_args_map, &pid_tgid);
	if (!data_args) {
		return 0;
	}

	// construct the buf_info
	struct buf_info buf_info = {
		.buf    = (const void *)(uintptr_t)data_args->buf,
		.iovcnt = data_args->iovcnt,
	};

	if (bytes_count > 0) {
		// handle the engine correlation
		handle_engine_correlation(data_args->fd, &buf_info, bytes_count, D_INGRESS, "sys_exit_read");
	}

	// delete the data_args
	bpf_map_delete_elem(&java_active_ssl_syscall_args_map, &pid_tgid);

	return 0;
}

// writev/readv syscalls - vectored I/O operations common in Java NIO
SEC("tracepoint/syscalls/sys_enter_writev")
int java_ssl_engine_sys_enter_writev(struct trace_event_raw_sys_enter *ctx) {
	// extract the args: writev(int fd, const struct iovec *iov, int iovcnt)
	int fd                  = (int)ctx->args[0];
	const struct iovec *iov = (const struct iovec *)ctx->args[1];
	int iovcnt              = (int)ctx->args[2];

	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// construct the data_args
	struct data_args data_args = {};
	data_args.fd               = fd;
	data_args.buf              = (uintptr_t)iov;
	data_args.iovcnt           = iovcnt;

	bpf_map_update_elem(&java_active_ssl_syscall_args_map, &pid_tgid, &data_args, BPF_ANY);

	return 0;
}

SEC("tracepoint/syscalls/sys_exit_writev")
int java_ssl_engine_sys_exit_writev(struct trace_event_raw_sys_exit *ctx) {
	// extract the writev length returned by the syscall
	ssize_t bytes_count = ctx->ret;

	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// get the data_args
	struct data_args *data_args = bpf_map_lookup_elem(&java_active_ssl_syscall_args_map, &pid_tgid);
	if (!data_args) {
		return 0;
	}

	// construct the buf_info
	struct buf_info buf_info = {
		.buf    = (const void *)(uintptr_t)data_args->buf,
		.iovcnt = data_args->iovcnt,
	};

	if (bytes_count > 0) {
		// handle the engine correlation
		handle_engine_correlation(data_args->fd, &buf_info, bytes_count, D_EGRESS, "sys_exit_writev");
	}

	// delete the data_args
	bpf_map_delete_elem(&java_active_ssl_syscall_args_map, &pid_tgid);

	return 0;
}

SEC("tracepoint/syscalls/sys_enter_readv")
int java_ssl_engine_sys_enter_readv(struct trace_event_raw_sys_enter *ctx) {
	// extract the args: readv(int fd, const struct iovec *iov, int iovcnt)
	int fd                  = (int)ctx->args[0];
	const struct iovec *iov = (const struct iovec *)ctx->args[1];
	int iovcnt              = (int)ctx->args[2];

	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// construct the data_args
	struct data_args data_args = {};
	data_args.fd               = fd;
	data_args.buf              = (uintptr_t)iov;
	data_args.iovcnt           = iovcnt;

	// persist the data_args
	bpf_map_update_elem(&java_active_ssl_syscall_args_map, &pid_tgid, &data_args, BPF_ANY);

	return 0;
}

SEC("tracepoint/syscalls/sys_exit_readv")
int java_ssl_engine_sys_exit_readv(struct trace_event_raw_sys_exit *ctx) {
	// extract the readv length returned by the syscall
	ssize_t bytes_count = ctx->ret;

	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// get the data_args
	struct data_args *data_args = bpf_map_lookup_elem(&java_active_ssl_syscall_args_map, &pid_tgid);
	if (!data_args) {
		return 0;
	}

	// construct the buf_info
	struct buf_info buf_info = {
		.buf    = (const void *)(uintptr_t)data_args->buf,
		.iovcnt = data_args->iovcnt,
	};

	if (bytes_count > 0) {
		// handle the engine correlation
		handle_engine_correlation(data_args->fd, &buf_info, bytes_count, D_INGRESS, "sys_exit_readv");
	}

	// delete the data_args
	bpf_map_delete_elem(&java_active_ssl_syscall_args_map, &pid_tgid);

	return 0;
}

// sendto/recvfrom syscalls - additional network I/O patterns
SEC("tracepoint/syscalls/sys_enter_sendto")
int java_ssl_engine_sys_enter_sendto(struct trace_event_raw_sys_enter *ctx) {
	// extract the args: sendto(int sockfd, const void *buf, size_t len, int flags, ...)
	int fd     = (int)ctx->args[0];
	void *buf  = (void *)ctx->args[1];
	size_t len = (size_t)ctx->args[2];

	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// construct the data_args
	struct data_args data_args = {};
	data_args.fd               = fd;
	data_args.buf              = (uintptr_t)buf;
	data_args.iovcnt           = 0;

	// persist the data_args
	bpf_map_update_elem(&java_active_ssl_syscall_args_map, &pid_tgid, &data_args, BPF_ANY);

	return 0;
}

SEC("tracepoint/syscalls/sys_exit_sendto")
int java_ssl_engine_sys_exit_sendto(struct trace_event_raw_sys_exit *ctx) {
	// extract the sendto length returned by the syscall
	ssize_t bytes_count = ctx->ret;

	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// get the data_args
	struct data_args *data_args = bpf_map_lookup_elem(&java_active_ssl_syscall_args_map, &pid_tgid);
	if (!data_args) {
		return 0;
	}

	// construct the buf_info
	struct buf_info buf_info = {
		.buf    = (const void *)(uintptr_t)data_args->buf,
		.iovcnt = data_args->iovcnt,
	};

	if (bytes_count > 0) {
		// handle the engine correlation
		handle_engine_correlation(data_args->fd, &buf_info, bytes_count, D_EGRESS, "sys_exit_sendto");
	}

	// delete the data_args
	bpf_map_delete_elem(&java_active_ssl_syscall_args_map, &pid_tgid);

	return 0;
}

SEC("tracepoint/syscalls/sys_enter_recvfrom")
int java_ssl_engine_sys_enter_recvfrom(struct trace_event_raw_sys_enter *ctx) {
	// extract the args: recvfrom(int sockfd, void *buf, size_t len, int flags, ...)
	int fd     = (int)ctx->args[0];
	void *buf  = (void *)ctx->args[1];
	size_t len = (size_t)ctx->args[2];

	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// construct the data_args
	struct data_args data_args = {};
	data_args.fd               = fd;
	data_args.buf              = (uintptr_t)buf;
	data_args.iovcnt           = 0;

	bpf_map_update_elem(&java_active_ssl_syscall_args_map, &pid_tgid, &data_args, BPF_ANY);

	return 0;
}

SEC("tracepoint/syscalls/sys_exit_recvfrom")
int java_ssl_engine_sys_exit_recvfrom(struct trace_event_raw_sys_exit *ctx) {
	// extract the recvfrom length returned by the syscall
	ssize_t bytes_count = ctx->ret;

	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// get the data_args
	struct data_args *data_args = bpf_map_lookup_elem(&java_active_ssl_syscall_args_map, &pid_tgid);
	if (!data_args) {
		return 0;
	}

	// construct the buf_info
	struct buf_info buf_info = {
		.buf    = (const void *)(uintptr_t)data_args->buf,
		.iovcnt = data_args->iovcnt,
	};

	if (bytes_count > 0) {
		// handle the engine correlation
		handle_engine_correlation(data_args->fd, &buf_info, bytes_count, D_INGRESS, "sys_exit_recvfrom");
	}

	// delete the data_args
	bpf_map_delete_elem(&java_active_ssl_syscall_args_map, &pid_tgid);

	return 0;
}

// SSLEngine socket closed event handler
SEC("tracepoint/syscalls/sys_enter_close")
int java_ssl_engine_sys_enter_close(struct trace_event_raw_sys_enter *ctx) {
	// extract the close length returned by the syscall
	int fd = (int)ctx->args[0];

	// get the pid
	uint32_t pid = bpf_get_current_userspace_pid();

	// is this a java process?
	bool is_java = bpf_map_lookup_elem(&java_process_pid_map, &pid);
	if (!is_java) {
		return 0;
	}

	// construct the socket_closed_event
	struct java_ssl_engine_socket_closed_event socket_closed_event = {
		.type = J_SOCKET_CLOSED,
		.pid  = pid,
		.fd   = fd,
	};

	// submit the socket_closed_event
	bpf_ringbuf_output(&java_ssl_engine_events, &socket_closed_event, sizeof(socket_closed_event), 0);

	return 0;
}
