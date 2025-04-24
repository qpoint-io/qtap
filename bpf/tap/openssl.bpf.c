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
#include "openssl.bpf.h"

// are we reading or writing
enum SSL_DIRECTION {
	SSL_READ,
	SSL_WRITE,
};

// persist the read args for exit handler
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uint64_t); // pid_tgid
	__type(value, struct data_args);
	__uint(max_entries, 1024);
} active_ssl_read_args_map SEC(".maps");

// persist the write args for exit handler
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uint64_t); // pid_tgid
	__type(value, struct data_args);
	__uint(max_entries, 1024);
} active_ssl_write_args_map SEC(".maps");

// persist ssl pointer to fd
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__type(key, uintptr_t); // ssl
	__type(value, int32_t); // fd
	__uint(max_entries, 4096);
} ssl_to_fd_map SEC(".maps");

// request the fd from socket syscall layer
static void request_fd_from_syscall(uint64_t pid_tgid) {
	// initialize a fd request
	struct fd_request fd_request = {};
	fd_request.is_ssl            = true;

	// persist for syscall
	bpf_map_update_elem(&uprobe_fd_requests, &pid_tgid, &fd_request, BPF_ANY);
}

// retrieve fd from socket syscall layer
static int32_t get_fd_from_syscall(uint64_t pid_tgid) {
	// extract the pid
	uint32_t pid = pid_tgid >> 32;

	// do we have a userspace request for a fd?
	struct fd_request *fd_request = bpf_map_lookup_elem(&uprobe_fd_requests, &pid_tgid);

	// nothing to do if there's not a request
	if (fd_request == NULL) {
		TRACE_OPENSSL(pid, "openssl.get_fd_from_syscall (no request found)", TRACE_INT("pid", pid), TRACE_INT("pid_tgid", pid_tgid));
		return 0;
	}

	// clean the request
	bpf_map_delete_elem(&uprobe_fd_requests, &pid_tgid);

	// return the fd
	return fd_request->fd;
}

// retrieve fd from cache
static int32_t get_fd_from_cache(uintptr_t ssl) {
	int32_t *fd = bpf_map_lookup_elem(&ssl_to_fd_map, &ssl);

	// nothing to do if there's not a request
	if (fd == NULL) {
		return 0;
	}

	// return the fd
	return *fd;
}

// retrieve the fd from various locations
static int32_t get_fd(uint64_t pid_tgid, uintptr_t ssl) {
	// extract the pid
	uint32_t pid = pid_tgid >> 32;

	// first try any registered TLS helper
	int32_t fd = ssl_get_fd(pid_tgid, ssl);

	// if we have a valid fd, return it
	if (fd > 0) {
		return fd;
	}

	// otherwise, try the socket layer
	fd = get_fd_from_syscall(pid_tgid);

	// if we have a valid fd, return it
	if (fd > 0) {
		return fd;
	}

	// otherwise, try the ssl map
	fd = get_fd_from_cache(ssl);

	// if we have a valid fd, return it
	if (fd > 0) {
		return fd;
	}

	// otherwise, return 0
	return 0;
}

SEC("uretprobe/SSL_new")
int BPF_URETPROBE(openssl_probe_ret_SSL_new) {
	// get the pid
	uint32_t pid = bpf_get_current_pid_tgid() >> 32;

	// get the pointer to ssl (first argument)
	uintptr_t ssl = (uintptr_t)PT_REGS_RC(ctx);

	// trace
	TRACE_OPENSSL(pid, "openssl/new", TRACE_INT("pid", pid), TRACE_POINTER("ssl", (void *)ssl));

	// Notify the TLS helper about the new SSL object
	ssl_register_handle(ssl);

	return 0;
}

SEC("uprobe/SSL_free")
int BPF_UPROBE(openssl_probe_entry_SSL_free) {
	// extract the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// extract the pid
	uint32_t pid = pid_tgid >> 32;

	// get the pointer to ssl (first argument)
	uintptr_t ssl = (uintptr_t)PT_REGS_RC(ctx);

	// trace
	TRACE_OPENSSL(pid, "openssl/free", TRACE_INT("pid", pid), TRACE_POINTER("ssl", (void *)ssl));

	// Notify the TLS helper about the freed SSL object
	ssl_remove_handle(ssl);

	// remove the ssl pointer from the fd map
	bpf_map_delete_elem(&ssl_to_fd_map, &ssl);

	return 0;
}

SEC("uprobe/SSL_read")
int BPF_UPROBE(openssl__probe_entry_SSL_read, void *ssl, void *buf, int num) {
	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// extract the pid
	uint32_t pid = bpf_get_current_pid_tgid() >> 32;

	// Notify the TLS helper about the SSL object
	ssl_register_handle((uintptr_t)ssl);

	// request from syscall
	request_fd_from_syscall(pid_tgid);

	// initialize state
	struct data_args read_args = {};
	read_args.buf              = (uintptr_t)buf;
	read_args.ssl              = (uintptr_t)ssl;

	// persist the state
	bpf_map_update_elem(&active_ssl_read_args_map, &pid_tgid, &read_args, BPF_ANY);

	return 0;
}

SEC("uretprobe/SSL_read")
int BPF_URETPROBE(openssl__probe_ret_SSL_read) {
	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// extract the pid
	uint32_t pid = pid_tgid >> 32;

	// extract the return value
	int bytes_count = PT_REGS_RC(ctx);

	// allocate a read_args key
	uint32_t key = pid;

	// grab the read args
	struct data_args *read_args = bpf_map_lookup_elem(&active_ssl_read_args_map, &pid_tgid);

	// nothing to do if we don't have read args
	if (read_args == NULL) {
		return 0;
	}

	if (bytes_count > 0) {
		// fill in fd with syscall response
		read_args->fd = get_fd(pid_tgid, read_args->ssl);

		// ensure we have a valid fd
		if (read_args->fd == 0) {
			TRACE_OPENSSL(pid, "openssl/read (fd == 0)", TRACE_INT("pid", pid), TRACE_INT("fd", read_args->fd), TRACE_INT("bytes", bytes_count));
			return 0;
		}

		// persist the fd to the ssl map
		bpf_map_update_elem(&ssl_to_fd_map, &read_args->ssl, &read_args->fd, BPF_ANY);

		// trace
		TRACE_OPENSSL(pid, "openssl/read", TRACE_INT("pid", pid), TRACE_INT("fd", read_args->fd), TRACE_INT("bytes", bytes_count));

		// construct a pid_fd_key
		struct pid_fd_key id = {};
		id.pid               = pid;
		id.fd                = read_args->fd;

		// initialize a socket context
		struct socket_ctx ctx = {};
		ctx.id                = &id;
		ctx.pid_tgid          = pid_tgid;
		ctx.trace_mod         = QTAP_OPENSSL;
		bpf_probe_read_str(ctx.trace_id, sizeof(ctx.trace_id), "openssl/read");

		// process the data
		process_data(&ctx, D_INGRESS, read_args, bytes_count, /* ssl */ true);
	}

	// clean the entry
	bpf_map_delete_elem(&active_ssl_read_args_map, &pid_tgid);

	return 0;
}

SEC("uprobe/SSL_read_ex")
int BPF_UPROBE(openssl__probe_entry_SSL_read_ex, void *ssl, void *buf, size_t num, size_t *readbytes) {
	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// extract the pid
	uint32_t pid = bpf_get_current_pid_tgid() >> 32;

	// Notify the TLS helper about the SSL object
	ssl_register_handle((uintptr_t)ssl);

	// request from syscall
	request_fd_from_syscall(pid_tgid);

	// initialize state
	struct data_args read_args = {};
	read_args.buf              = (uintptr_t)buf;
	read_args.ssl              = (uintptr_t)ssl;
	read_args.ex_bytes         = (uintptr_t)readbytes;

	// persist the state
	bpf_map_update_elem(&active_ssl_read_args_map, &pid_tgid, &read_args, BPF_ANY);

	return 0;
}

SEC("uretprobe/SSL_read_ex")
int BPF_URETPROBE(openssl__probe_ret_SSL_read_ex) {
	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// extract the pid
	uint32_t pid = pid_tgid >> 32;

	// if we have a valid return value, return
	int funcRet = PT_REGS_RC(ctx);
	if (funcRet < 1) {
		TRACE_OPENSSL(pid, "openssl/read_ex (funcRet < 1)", TRACE_INT("pid", pid), TRACE_INT("funcRet", funcRet));
		return 0;
	}

	// allocate a read_args key
	uint32_t key = pid;

	// grab the read args
	struct data_args *read_args = bpf_map_lookup_elem(&active_ssl_read_args_map, &pid_tgid);

	// nothing to do if we don't have read args
	if (read_args == NULL)
		return 0;

	// pull ex_bytes and cast back
	size_t *ex_bytes = (size_t *)read_args->ex_bytes;

	// read the number of bytes actually read (safely)
	size_t bytes_read = 0;
	if (ex_bytes != NULL) {
		bpf_probe_read(&bytes_read, sizeof(bytes_read), ex_bytes);
	}

	if (bytes_read > 0) {
		// fill in fd with syscall response
		read_args->fd = get_fd(pid_tgid, read_args->ssl);

		// ensure we have a valid fd
		if (read_args->fd == 0) {
			TRACE_OPENSSL(pid, "openssl/read_ex (fd == 0)", TRACE_INT("pid", pid), TRACE_INT("fd", read_args->fd), TRACE_INT("bytes", bytes_read));
			return 0;
		}

		// persist the fd to the ssl map
		bpf_map_update_elem(&ssl_to_fd_map, &read_args->ssl, &read_args->fd, BPF_ANY);

		// trace
		TRACE_OPENSSL(pid, "openssl/read_ex", TRACE_INT("pid", pid), TRACE_INT("fd", read_args->fd), TRACE_INT("bytes", bytes_read));

		// construct a pid_fd_key
		struct pid_fd_key id = {};
		id.pid               = pid;
		id.fd                = read_args->fd;

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_OPENSSL;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "openssl/read_ex");

		// process the data
		process_data(&sock_ctx, D_INGRESS, read_args, bytes_read, /* ssl */ true);
	}

	// clean the entry
	bpf_map_delete_elem(&active_ssl_read_args_map, &pid_tgid);

	return 0;
}

SEC("uprobe/SSL_write")
int BPF_UPROBE(openssl__probe_entry_SSL_write, void *ssl, void *buf, int num) {
	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// extract the pid
	uint32_t pid = bpf_get_current_pid_tgid() >> 32;

	// Notify the TLS helper about the SSL object
	ssl_register_handle((uintptr_t)ssl);

	// request from syscall
	request_fd_from_syscall(pid_tgid);

	// initialize state
	struct data_args write_args = {};
	write_args.buf              = (uintptr_t)buf;
	write_args.ssl              = (uintptr_t)ssl;

	// persist the state
	bpf_map_update_elem(&active_ssl_write_args_map, &pid_tgid, &write_args, BPF_ANY);

	return 0;
}

SEC("uretprobe/SSL_write")
int BPF_URETPROBE(openssl__probe_ret_SSL_write) {
	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// extract the pid
	uint32_t pid = pid_tgid >> 32;

	// extract the return value
	int bytes_count = PT_REGS_RC(ctx);

	// allocate a read_args key
	uint32_t key = pid;

	// grab the write args
	struct data_args *write_args = bpf_map_lookup_elem(&active_ssl_write_args_map, &pid_tgid);

	// nothing to do if we don't have write args
	if (write_args == NULL) {
		return 0;
	}

	if (bytes_count > 0) {
		// fill in fd with syscall response
		write_args->fd = get_fd(pid_tgid, write_args->ssl);

		// ensure we have a valid fd
		if (write_args->fd == 0) {
			TRACE_OPENSSL(pid, "openssl/write (fd == 0)", TRACE_INT("pid", pid), TRACE_INT("fd", write_args->fd), TRACE_INT("bytes", bytes_count));
			return 0;
		}

		// persist the fd to the ssl map
		bpf_map_update_elem(&ssl_to_fd_map, &write_args->ssl, &write_args->fd, BPF_ANY);

		// trace
		TRACE_OPENSSL(pid, "openssl/write", TRACE_INT("pid", pid), TRACE_INT("fd", write_args->fd), TRACE_INT("bytes", bytes_count));

		// construct a pid_fd_key
		struct pid_fd_key id = {};
		id.pid               = pid;
		id.fd                = write_args->fd;

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_OPENSSL;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "openssl/write");

		// process the data
		process_data(&sock_ctx, D_EGRESS, write_args, bytes_count, /* ssl */ true);
	}

	// clean the entry
	bpf_map_delete_elem(&active_ssl_write_args_map, &pid_tgid);

	return 0;
}

SEC("uprobe/SSL_write_ex")
int BPF_UPROBE(openssl__probe_entry_SSL_write_ex, void *ssl, void *buf, size_t num, size_t *writebytes) {
	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// extract the pid
	uint32_t pid = bpf_get_current_pid_tgid() >> 32;

	// Notify the TLS helper about the SSL object
	ssl_register_handle((uintptr_t)ssl);

	// request from syscall
	request_fd_from_syscall(pid_tgid);

	// initialize state
	struct data_args write_args = {};
	write_args.buf              = (uintptr_t)buf;
	write_args.ssl              = (uintptr_t)ssl;
	write_args.ex_bytes         = (uintptr_t)writebytes;

	// persist the state
	bpf_map_update_elem(&active_ssl_write_args_map, &pid_tgid, &write_args, BPF_ANY);

	return 0;
}

SEC("uretprobe/SSL_write_ex")
int BPF_URETPROBE(openssl__probe_ret_SSL_write_ex) {
	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// extract the pid
	uint32_t pid = pid_tgid >> 32;

	// allocate a read_args key
	uint32_t key = pid;

	// grab the write args
	struct data_args *write_args = bpf_map_lookup_elem(&active_ssl_write_args_map, &pid_tgid);

	// nothing to do if we don't have write args
	if (write_args == NULL)
		return 0;

	// pull ex_bytes and cast back
	size_t *ex_bytes = (size_t *)write_args->ex_bytes;

	// read the number of bytes actually written
	size_t bytes_written = 0;
	if (ex_bytes != NULL) {
		bpf_probe_read_user(&bytes_written, sizeof(bytes_written), ex_bytes);
	}

	if (bytes_written > 0) {
		// fill in fd with syscall response
		write_args->fd = get_fd(pid_tgid, write_args->ssl);

		// ensure we have a valid fd
		if (write_args->fd == 0) {
			TRACE_OPENSSL(
				pid, "openssl/write_ex (fd == 0)", TRACE_INT("pid", pid), TRACE_INT("fd", write_args->fd), TRACE_INT("bytes", bytes_written));
			return 0;
		}

		// persist the fd to the ssl map
		bpf_map_update_elem(&ssl_to_fd_map, &write_args->ssl, &write_args->fd, BPF_ANY);

		// trace
		TRACE_OPENSSL(pid, "openssl/write_ex", TRACE_INT("pid", pid), TRACE_INT("fd", write_args->fd), TRACE_INT("bytes", bytes_written));

		// construct a pid_fd_key
		struct pid_fd_key id = {};
		id.pid               = pid;
		id.fd                = write_args->fd;

		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = &id;
		sock_ctx.pid_tgid          = pid_tgid;
		sock_ctx.trace_mod         = QTAP_OPENSSL;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "openssl/write_ex");
		// process the data
		process_data(&sock_ctx, D_EGRESS, write_args, bytes_written, /* ssl */ true);
	}

	// clean the entry
	bpf_map_delete_elem(&active_ssl_write_args_map, &pid_tgid);

	return 0;
}
