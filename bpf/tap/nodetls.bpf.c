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
#include "trace.bpf.h"
#include "openssl.bpf.h"

// the symbol offsets for node's tlswrap that get's us to the fd
struct node_tls_symaddr {
	int32_t tls_wrap_stream_listener_offset;
	int32_t stream_listener_stream_offset;
	int32_t stream_base_stream_resource_offset;
	int32_t libuv_stream_wrap_stream_base_offset;
	int32_t libuv_stream_wrap_stream_offset;
	int32_t uv_stream_s_io_watcher_offset;
	int32_t uv_io_s_fd_offset;
};

// map of node tlswrap symbol offsets, keyed by pid (managed by user-space qtap)
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__type(key, uint32_t); // pid
	__type(value, struct node_tls_symaddr);
	__uint(max_entries, 4096);
} node_tls_symaddrs_map SEC(".maps");

// map to associate an ssl instance with a tlswrap instance
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uintptr_t); // ssl pointer
	__type(value, uintptr_t); // tlswrap pointer
	__uint(max_entries, 1024);
} node_ssl_to_tlswrap_map SEC(".maps");

// map to associate a tlswrap instance with an ssl instance
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uintptr_t); // tlswrap pointer
	__type(value, uintptr_t); // ssl pointer
	__uint(max_entries, 1024);
} node_tlswrap_to_ssl_map SEC(".maps");

// map to hold record of tlswraps that exist, this map is important because
// it allows us to know which ssl pointers are valid from nodejs, otherwise
// they could be from other language runtimes.
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uintptr_t); // tlswrap pointer
	__type(value, bool); // exists
	__uint(max_entries, 2048);
} node_tlswrap_exists_map SEC(".maps");

// get the tlswrap pointer from the ssl pointer
static uintptr_t get_tlswrap_from_ssl(uintptr_t ssl) {
	uintptr_t *tlswrap = bpf_map_lookup_elem(&node_ssl_to_tlswrap_map, &ssl);
	if (tlswrap == NULL) {
		return 0;
	}
	return *tlswrap;
}

// deprecated -- not used, but still called by Qtap
// leaving in until the new ssl registration approach
// has been proven to be stable.
int update_node_ssl_tls_wrap_map(uintptr_t ssl) {
	return 0;
}

// deprecated -- not used, but still called by Qtap
// leaving in until the new ssl registration approach
// has been proven to be stable.
int remove_node_ssl_tls_wrap_map(uintptr_t ssl) {
	return 0;
}

// walk the tlswrap instance to find the fd
static int32_t get_fd_from_tlswrap(const struct node_tls_symaddr *symaddrs, void *tlswrap) {
	// extract the pid
	uint32_t pid = bpf_get_current_pid_tgid() >> 32;

	// first portal to the stream
	void *stream_ptr = tlswrap + symaddrs->tls_wrap_stream_listener_offset + symaddrs->stream_listener_stream_offset;
	void *stream     = NULL;

	// read the stream from the pointer offset
	bpf_probe_read(&stream, sizeof(stream), stream_ptr);

	// if the stream is null, we're done
	if (stream == NULL) {
		TRACE_NODETLS(pid, "nodetls/get_fd_from_tlswrap (no stream found)", TRACE_INT("pid", pid), TRACE_POINTER("tlswrap", (void *)tlswrap));
		return 0;
	}

	// calculate the uv_stream pointer
	void *uv_stream_ptr = stream - symaddrs->stream_base_stream_resource_offset - symaddrs->libuv_stream_wrap_stream_base_offset +
						  symaddrs->libuv_stream_wrap_stream_offset;

	// read the uv_stream from the pointer offset
	void *uv_stream = NULL;
	bpf_probe_read(&uv_stream, sizeof(uv_stream), uv_stream_ptr);

	// if the uv_stream is null, we're done
	if (uv_stream == NULL) {
		TRACE_NODETLS(pid, "nodetls/get_fd_from_tlswrap (no uv_stream found)", TRACE_INT("pid", pid), TRACE_POINTER("tlswrap", (void *)tlswrap));
		return 0;
	}

	// calculate the fd pointer
	int32_t *fd_ptr = uv_stream + symaddrs->uv_stream_s_io_watcher_offset + symaddrs->uv_io_s_fd_offset;

	// read the fd from the pointer offset
	int32_t fd = 0;
	if (bpf_probe_read(&fd, sizeof(fd), fd_ptr) != 0) {
		TRACE_NODETLS(pid, "nodetls/get_fd_from_tlswrap (no fd found)", TRACE_INT("pid", pid), TRACE_POINTER("tlswrap", (void *)tlswrap));
		return 0;
	}

	// return the fd
	return fd;
}

// retrieve the fd from node internals
int32_t get_fd_from_node(uint64_t pid_tgid, uintptr_t ssl) {
	// extract the pid
	uint32_t pid = pid_tgid >> 32;

	TRACE_NODETLS(pid, "nodetls/get_fd_from_node", TRACE_INT("pid", pid), TRACE_POINTER("ssl", (void *)ssl));

	// get the tlswrap pointer from the ssl pointer
	uintptr_t tls_wrap = get_tlswrap_from_ssl(ssl);
	if (tls_wrap == 0) {
		TRACE_NODETLS(pid, "nodetls/get_fd_from_node (no tlswrap found)", TRACE_INT("pid", pid), TRACE_POINTER("ssl", (void *)ssl));
		return 0;
	}

	// fetch the symaddr offsets from the pid_tgid
	const struct node_tls_symaddr *symaddrs = bpf_map_lookup_elem(&node_tls_symaddrs_map, &pid);
	if (symaddrs == NULL) {
		TRACE_NODETLS(pid, "nodetls/get_fd_from_node (no symaddrs found)", TRACE_INT("pid", pid), TRACE_POINTER("ssl", (void *)ssl));
		return 0;
	}

	// get the fd from the tlswrap
	return get_fd_from_tlswrap(symaddrs, (void *)tls_wrap);
}

SEC("uprobe/TLSWrap_memfn")
int BPF_UPROBE(nodetls__probe_entry_TLSWrap_memfn) {
	// extract the tlswrap pointer as the first argument
	uintptr_t tls_wrap = (uintptr_t)PT_REGS_PARM1(ctx);

	// extract the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// extract the pid
	uint32_t pid = pid_tgid >> 32;

	// TRACE_NODETLS(pid, "nodetls/TLSWrap_memfn", TRACE_INT("pid", pid), TRACE_POINTER("tlswrap", (void *)tls_wrap));

	// update the tlswrap -> exists mapping
	bool exists = true;
	long ret    = bpf_map_update_elem(&node_tlswrap_exists_map, &tls_wrap, &exists, BPF_ANY);
	if (ret != 0) {
		TRACE_NODETLS(pid, "nodetls/TLSWrap_memfn (bpf_map_update_elem failed)", TRACE_INT("pid", pid), TRACE_POINTER("tlswrap", (void *)tls_wrap),
			TRACE_INT("ret", ret));
	}

	return 0;
}

SEC("uprobe/TLSWrap_destructor")
int BPF_UPROBE(nodetls__probe_entry_TLSWrap_destructor) {
	uintptr_t tlswrap_ptr = (uintptr_t)PT_REGS_PARM1(ctx);
	uint32_t pid          = bpf_get_current_pid_tgid() >> 32;

	TRACE_NODETLS(pid, "nodetls/TLSWrap_destructor", TRACE_INT("pid", pid), TRACE_POINTER("tlswrap", (void *)tlswrap_ptr));

	// Remove from reverse map
	bpf_map_delete_elem(&node_tlswrap_to_ssl_map, &tlswrap_ptr);

	// Remove from exists map
	bpf_map_delete_elem(&node_tlswrap_exists_map, &tlswrap_ptr);

	return 0;
}

// ref: https://docs.openssl.org/master/man3/SSL_CTX_set_cert_cb/
SEC("uprobe/SSL_set_cert_cb")
int BPF_UPROBE(nodetls__probe_entry_SSL_set_cert_cb) {
	// extract the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// extract the pid
	uint32_t pid = pid_tgid >> 32;

	u64 ssl_ptr     = PT_REGS_PARM1(ctx); // SSL*
	u64 tlswrap_ptr = PT_REGS_PARM3(ctx); // void* arg (actually TLSWrap* this)

	// check if cb ptr is in the existence map (indicates this is a nodejs process)
	bool exists = bpf_map_lookup_elem(&node_tlswrap_exists_map, &tlswrap_ptr);
	if (!exists) {
		// This indicates that this is not a nodejs process, so we don't need to track it
		// the SSL_set_cert_cb's third argument is *a* pointer, not necessarily the tlswrap pointer
		// so we confirm it here.
		TRACE_NODETLS(pid, "nodetls/SSL_set_cert_cb (tlswrap not found)", TRACE_INT("pid", pid), TRACE_POINTER("tlswrap", (void *)tlswrap_ptr));
		return 0;
	}

	TRACE_NODETLS(
		pid, "nodetls/SSL_set_cert_cb", TRACE_INT("pid", pid), TRACE_POINTER("ssl", (void *)ssl_ptr), TRACE_POINTER("tlswrap", (void *)tlswrap_ptr));

	// update both directional maps
	bpf_map_update_elem(&node_ssl_to_tlswrap_map, &ssl_ptr, &tlswrap_ptr, BPF_ANY);
	bpf_map_update_elem(&node_tlswrap_to_ssl_map, &tlswrap_ptr, &ssl_ptr, BPF_ANY);

	return 0;
}

// ref: https://docs.openssl.org/master/man3/SSL_free/
SEC("uprobe/SSL_free")
int BPF_UPROBE(nodetls__probe_entry_SSL_free) {
	// extract the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// extract the pid
	uint32_t pid = pid_tgid >> 32;

	// get the pointer to ssl (first argument)
	uintptr_t ssl_ptr = (uintptr_t)PT_REGS_RC(ctx);

	TRACE_NODETLS(pid, "nodetls/SSL_free", TRACE_INT("pid", pid), TRACE_POINTER("ssl", (void *)ssl_ptr));
	bpf_map_delete_elem(&node_ssl_to_tlswrap_map, &ssl_ptr);

	return 0;
}