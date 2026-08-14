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
 *
 * This file incorporates concepts and borrows some code covered by the
 * following copyright and permission notice:
 *
 * Copyright 2018- The Pixie Authors.
 * Project: px.dev
 * File: src/stirling/source_connectors/socket_tracer/bcc_bpf/go_tls_trace.c
 * File: src/stirling/source_connectors/socket_tracer/bcc_bpf/go_trace_common.h
 * File: src/stirling/source_connectors/socket_tracer/bcc_bpf_intf/symaddrs.h
 *
 */

#include "common.bpf.h"
#include "socket.bpf.h"
#include "trace.bpf.h"
#include "settings.bpf.h"

#define REQUIRE_SYMADDR(symaddr, retval) \
	if (symaddr == -1) { \
		return retval; \
	}

#define REQUIRE_LOCATION(loc, retval) \
	if (loc.location == GO_UNKNOWN) { \
		return retval; \
	}

// representation of a go interface
struct go_interface {
	int64_t type;
	void *ptr;
};

// go function argument locations
enum GO_LOCATION { GO_UNKNOWN = 0, GO_STACK = 1, GO_REGISTERS = 2 } __attribute__((packed, aligned(4)));

// go function argument location and offset
struct go_location {
	uint32_t location;
	int32_t offset;
};

// arguments of crypto/tls.(*conn).write
const struct go_location write_c_loc       = {GO_REGISTERS, 0};
const struct go_location write_b_loc       = {GO_REGISTERS, 1};
const struct go_location write_retval0_loc = {GO_REGISTERS, 0};
const struct go_location write_retval1_loc = {GO_REGISTERS, 1};

// arguments of crypto/tls.(*conn).read
const struct go_location read_c_loc       = {GO_REGISTERS, 0};
const struct go_location read_b_loc       = {GO_REGISTERS, 1};
const struct go_location read_retval0_loc = {GO_REGISTERS, 0};
const struct go_location read_retval1_loc = {GO_REGISTERS, 1};

// the symbol offsets for go tls
struct __attribute__((packed)) go_tls_symaddr {
	// ---- itable symbols ----

	// net.conn interface types.
	// go.itab.*google.golang.org/grpc/credentials/internal.syscallconn,net.conn
	int64_t internal_syscall_conn;
	int64_t tls_conn; // go.itab.*crypto/tls.conn,net.conn
	int64_t net_tcp_conn; // go.itab.*net.tcpconn,net.conn

	// ---- struct member offsets ----

	// members of internal/poll.fd
	int32_t fd_sysfd_offset;

	// members of crypto/tls.conn
	int32_t tls_conn_offset;

	// members of google.golang.org/grpc/credentials/internal.syscallconn
	int32_t syscall_conn_offset;

	// member of runtime.g
	int32_t goid_offset;
};

// the composite key for the maps
struct tgid_goid_t {
	__u32 tgid;
	__s64 goid;
};

// the arguments for the go tls conn entry functions
struct go_tls_conn_args {
	uintptr_t conn_ptr;
	uintptr_t plaintext_ptr;
};

// registers of the golang register ABI.
struct go_regabi_regs {
	__u64 regs[9];
};

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 1024);
	__type(key, uint32_t); // pid
	__type(value, struct go_tls_symaddr);
} go_tls_symaddrs_map SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 1024); // Adjust this value as needed
	__type(key, struct tgid_goid_t);
	__type(value, struct go_tls_conn_args);
} active_tls_conn_op_map SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, struct go_regabi_regs);
} regs_heap SEC(".maps");

static __always_inline __u64 *go_regabi_regs(const struct pt_regs *ctx) {
	__u32 key                            = 0;
	struct go_regabi_regs *regs_heap_var = bpf_map_lookup_elem(&regs_heap, &key);
	if (!regs_heap_var) {
		return NULL;
	}

#if defined(__TARGET_ARCH_x86)
	regs_heap_var->regs[0] = ctx->ax;
	regs_heap_var->regs[1] = ctx->bx;
	regs_heap_var->regs[2] = ctx->cx;
	regs_heap_var->regs[3] = ctx->di;
	regs_heap_var->regs[4] = ctx->si;
	regs_heap_var->regs[5] = ctx->r8;
	regs_heap_var->regs[6] = ctx->r9;
	regs_heap_var->regs[7] = ctx->r10;
	regs_heap_var->regs[8] = ctx->r11;
#elif defined(__TARGET_ARCH_arm64)
	regs_heap_var->regs[0] = ctx->regs[0];
	regs_heap_var->regs[1] = ctx->regs[1];
	regs_heap_var->regs[2] = ctx->regs[2];
	regs_heap_var->regs[3] = ctx->regs[3];
	regs_heap_var->regs[4] = ctx->regs[4];
	regs_heap_var->regs[5] = ctx->regs[5];
	regs_heap_var->regs[6] = ctx->regs[6];
	regs_heap_var->regs[7] = ctx->regs[7];
	regs_heap_var->regs[8] = ctx->regs[8];
#endif

	return regs_heap_var->regs;
}

static __always_inline void assign_arg(void *arg, __u32 arg_size, struct go_location loc, const void *sp, __u64 *regs) {
	switch (loc.location) {
	case GO_STACK:
		bpf_probe_read(arg, arg_size, (void *)(sp + loc.offset));
		break;
	case GO_REGISTERS:
		if (loc.offset >= 0 && loc.offset < 9) {
			bpf_probe_read(arg, arg_size, &regs[loc.offset]);
		}
		break;
	default:
		break;
	}
}

static __always_inline __u64 get_goid(struct pt_regs *ctx) {
	__u64 id   = bpf_get_current_pid_tgid();
	__u32 tgid = id >> 32;
	u64 goid   = 0;

	struct go_tls_symaddr *symaddrs = bpf_map_lookup_elem(&go_tls_symaddrs_map, &tgid);
	if (!symaddrs) {
		// bpf_printk("get_goid NO_SYMADDRS = pid: %u", tgid);
		return 0;
	}

	// ensure the goid offset is within reasonable bounds
	if (symaddrs->goid_offset < 0 || symaddrs->goid_offset > 1024) {
		// bpf_printk("get_goid BAD_GOID_OFFSET = pid: %u", tgid);
		return 0;
	}

	// g struct pointer
	void *g_ptr;

#if defined(__TARGET_ARCH_x86)
	// On x86, the g_ptr is stored in the r14 register
	g_ptr = (void *)(unsigned long)ctx->r14;
#elif defined(__TARGET_ARCH_arm64)
	// on ARM64, g_ptr is stored in x28 register
	g_ptr = (void *)(unsigned long)ctx->regs[28];
#endif

	// as long as g_ptr is not null, we can read the goid from it
	if (g_ptr) {
		bpf_probe_read(&goid, sizeof(goid), (void *)((unsigned long)g_ptr + symaddrs->goid_offset));
	}

	// return whatever we were able to extract
	// bpf_printk("goid: %llu", goid);
	return goid;
}

static __always_inline __s32 get_fd_from_conn_intf_core(struct go_interface conn_intf, const struct go_tls_symaddr *symaddrs) {
	REQUIRE_SYMADDR(symaddrs->fd_sysfd_offset, INVALID_FD);

	if (conn_intf.type == symaddrs->internal_syscall_conn) {
		REQUIRE_SYMADDR(symaddrs->syscall_conn_offset, INVALID_FD);
		bpf_probe_read(&conn_intf, sizeof(conn_intf), (void *)(conn_intf.ptr + symaddrs->syscall_conn_offset));
	}

	if (conn_intf.type == symaddrs->tls_conn) {
		REQUIRE_SYMADDR(symaddrs->tls_conn_offset, INVALID_FD);
		bpf_probe_read(&conn_intf, sizeof(conn_intf), (void *)(conn_intf.ptr + symaddrs->tls_conn_offset));
	}

	if (conn_intf.type != symaddrs->net_tcp_conn) {
		return INVALID_FD;
	}

	void *fd_ptr;
	bpf_probe_read(&fd_ptr, sizeof(fd_ptr), conn_intf.ptr);

	__s64 sysfd;
	bpf_probe_read(&sysfd, sizeof(__s64), (void *)(fd_ptr + symaddrs->fd_sysfd_offset));

	return (__s32)sysfd;
}

static __always_inline int process_write(struct pt_regs *ctx, __u64 id, __u32 tgid, struct go_tls_conn_args *args) {
	// lookup the symbol address offsets
	struct go_tls_symaddr *symaddrs = bpf_map_lookup_elem(&go_tls_symaddrs_map, &tgid);
	if (!symaddrs) {
		return 0;
	}

	// debug the retval0 and retval1 locations
	// bpf_printk("write_retval0_loc: location=%d, offset=%d", symaddrs->write_retval0_loc.location, symaddrs->write_retval0_loc.offset);
	// bpf_printk("write_retval1_loc: location=%d, offset=%d", symaddrs->write_retval1_loc.location, symaddrs->write_retval1_loc.offset);

	const void *sp = (const void *)PT_REGS_SP(ctx);
	__u64 *regs    = go_regabi_regs(ctx);
	if (!regs) {
		return 0;
	}

	// extract the bytes written
	__s64 bytes_written = 0;
	assign_arg(&bytes_written, sizeof(bytes_written), write_retval0_loc, sp, regs);
	// bpf_printk("bytes_written: %lld", bytes_written);

	struct go_interface err = {};
	assign_arg(&err, sizeof(err), write_retval1_loc, sp, regs);
	// bpf_printk("err: %llx", err.ptr);

	// if function returns an error, then there's no data to trace.
	if (err.ptr != 0) {
		return 0;
	}

	// to get the fd, cast the conn_ptr into a go_interface.
	struct go_interface conn_intf;
	conn_intf.type = symaddrs->tls_conn;
	conn_intf.ptr  = (void *)args->conn_ptr;
	int fd         = get_fd_from_conn_intf_core(conn_intf, symaddrs);
	if (fd == INVALID_FD) {
		// bpf_printk("process_write INVALID FD");
		return 0;
	}

	// bpf_printk("process_write WORKED!!!");

	if (bytes_written > 0) {
		// trace
		TRACE_GOTLS(tgid, "gotls/write", TRACE_INT("pid", tgid), TRACE_INT("fd", fd), TRACE_INT("bytes", bytes_written));

		// initialize read_args
		struct data_args read_args = {};
		read_args.buf              = args->plaintext_ptr;

		// construct a pid_fd_key
		struct pid_fd_key key = {};
		key.pid               = (uint32_t)tgid;
		key.fd                = (int32_t)fd;

		// initialize a socket context
		struct socket_ctx ctx = {};
		ctx.id                = &key;
		ctx.pid_tgid          = (uint64_t)id;
		ctx.trace_mod         = QTAP_GOTLS;
		bpf_probe_read_str(ctx.trace_id, sizeof(ctx.trace_id), "gotls/write");

		// process the data
		process_data(&ctx, D_EGRESS, &read_args, bytes_written, /* ssl */ true);
	}

	return 0;
}

static __always_inline int process_read(struct pt_regs *ctx, __u64 id, __u32 tgid, struct go_tls_conn_args *args) {
	// lookup the symbol addresses
	struct go_tls_symaddr *symaddrs = bpf_map_lookup_elem(&go_tls_symaddrs_map, &tgid);
	if (!symaddrs) {
		return 0;
	}

	// get the stack pointer and registers
	const void *sp = (const void *)PT_REGS_SP(ctx);
	__u64 *regs    = go_regabi_regs(ctx);
	if (!regs) {
		return 0;
	}

	// extract the bytes read
	__s64 bytes_read = 0;
	assign_arg(&bytes_read, sizeof(bytes_read), read_retval0_loc, sp, regs);
	// bpf_printk("bytes_read: %lld", bytes_read);

	// extract the error
	struct go_interface err = {};
	assign_arg(&err, sizeof(err), read_retval1_loc, sp, regs);
	// bpf_printk("err: %llx", err.ptr);

	// if function returns an error, then there's no data to trace.
	if (err.ptr != 0) {
		return 0;
	}

	// to get the fd, cast the conn_ptr into a go_interface.
	struct go_interface conn_intf;
	conn_intf.type = symaddrs->tls_conn;
	conn_intf.ptr  = (void *)args->conn_ptr;
	int fd         = get_fd_from_conn_intf_core(conn_intf, symaddrs);
	if (fd == INVALID_FD) {
		// bpf_printk("process_read INVALID FD");
		return 0;
	}

	// debug
	// bpf_printk("process_read WORKED!!!");

	if (bytes_read > 0) {
		// trace
		TRACE_GOTLS(tgid, "gotls/read", TRACE_INT("pid", tgid), TRACE_INT("fd", fd), TRACE_INT("bytes", bytes_read));

		// initialize read_args
		struct data_args read_args = {};
		read_args.buf              = args->plaintext_ptr;

		// construct a pid_fd_key
		struct pid_fd_key key = {};
		key.pid               = (uint32_t)tgid;
		key.fd                = (int32_t)fd;

		// initialize a socket context
		struct socket_ctx ctx = {};
		ctx.id                = &key;
		ctx.pid_tgid          = (uint64_t)id;
		ctx.trace_mod         = QTAP_GOTLS;
		bpf_probe_read_str(ctx.trace_id, sizeof(ctx.trace_id), "gotls/read");

		// process the data
		process_data(&ctx, D_INGRESS, &read_args, bytes_read, /* ssl */ true);
	}

	return 0;
}

SEC("uprobe/go_tls_conn_write")
int BPF_UPROBE(gotls__probe_entry_tls_conn_write) {
	// bpf_printk("gotls__probe_entry_tls_conn_write");

	// extract the context
	__u64 id   = bpf_get_current_pid_tgid();
	__u32 tgid = id >> 32;
	__u32 pid  = id;

	// get the goid
	__u64 goid = get_goid(ctx);
	if (goid == 0) {
		return 0;
	}

	// construct a composite key for the maps
	struct tgid_goid_t tgid_goid = {};
	tgid_goid.tgid               = tgid;
	tgid_goid.goid               = goid;

	// lookup the symbol addresses
	struct go_tls_symaddr *symaddrs = bpf_map_lookup_elem(&go_tls_symaddrs_map, &tgid);
	if (!symaddrs) {
		return 0;
	}

	// Debug the write_c and write_b locations
	// bpf_printk("write_c_loc: location=%d, offset=%d", symaddrs->write_c_loc.location, symaddrs->write_c_loc.offset);
	// bpf_printk("write_b_loc: location=%d, offset=%d", symaddrs->write_b_loc.location, symaddrs->write_b_loc.offset);

	// get the stack pointer and registers
	const void *sp = (const void *)PT_REGS_SP(ctx);
	__u64 *regs    = go_regabi_regs(ctx);
	if (!regs) {
		return 0;
	}

	// store the arguments in a map for the uretprobe
	struct go_tls_conn_args args = {};
	assign_arg(&args.conn_ptr, sizeof(args.conn_ptr), write_c_loc, sp, regs);
	assign_arg(&args.plaintext_ptr, sizeof(args.plaintext_ptr), write_b_loc, sp, regs);

	bpf_map_update_elem(&active_tls_conn_op_map, &tgid_goid, &args, BPF_ANY);

	return 0;
}

SEC("uretprobe/go_tls_conn_write")
int BPF_UPROBE(gotls__probe_ret_tls_conn_write) {
	// bpf_printk("gotls__probe_ret_tls_conn_write");

	// extract the context
	__u64 id   = bpf_get_current_pid_tgid();
	__u32 tgid = id >> 32;
	__u32 pid  = id;

	// get the goid
	__u64 goid = get_goid(ctx);
	if (goid == 0) {
		return 0;
	}

	// construct a composite key for the maps
	struct tgid_goid_t tgid_goid = {};
	tgid_goid.tgid               = tgid;
	tgid_goid.goid               = goid;

	// lookup the args from the entry probe
	struct go_tls_conn_args *args = bpf_map_lookup_elem(&active_tls_conn_op_map, &tgid_goid);
	if (!args) {
		return 0;
	}

	// debug the go_tls_conn_args
	// bpf_printk("go_tls_conn_args debug: conn_ptr: %llx, plaintext_ptr: %llx", args->conn_ptr, args->plaintext_ptr);

	// the core logic (in a function to avoid goto for map cleanup)
	process_write(ctx, id, tgid, args);

	// cleanup the args from the entry probe
	bpf_map_delete_elem(&active_tls_conn_op_map, &tgid_goid);

	return 0;
}

SEC("uprobe/go_tls_conn_read")
int BPF_UPROBE(gotls__probe_entry_tls_conn_read) {
	// bpf_printk("gotls__probe_entry_tls_conn_read");
	// extract the context
	__u64 id   = bpf_get_current_pid_tgid();
	__u32 tgid = id >> 32;

	// get the goid
	__u64 goid = get_goid(ctx);
	if (goid == 0) {
		return 0;
	}

	// construct a composite key for the maps
	struct tgid_goid_t tgid_goid = {.tgid = tgid, .goid = goid};

	// lookup the symbol addresses
	struct go_tls_symaddr *symaddrs = bpf_map_lookup_elem(&go_tls_symaddrs_map, &tgid);
	if (!symaddrs) {
		return 0;
	}

	// bounds check for required argument offsets
	if (read_c_loc.location == GO_UNKNOWN || read_b_loc.location == GO_UNKNOWN) {
		return 0;
	}

	// get the stack pointer and registers
	const void *sp = (const void *)PT_REGS_SP(ctx);
	__u64 *regs    = go_regabi_regs(ctx);
	if (!regs) {
		return 0;
	}

	// read the arguments
	struct go_tls_conn_args args = {};

	// bounds check for register access
	if (read_c_loc.location == GO_REGISTERS && (read_c_loc.offset < 0 || read_c_loc.offset >= 9)) {
		return 0;
	}
	if (read_b_loc.location == GO_REGISTERS && (read_b_loc.offset < 0 || read_b_loc.offset >= 9)) {
		return 0;
	}

	assign_arg(&args.conn_ptr, sizeof(args.conn_ptr), read_c_loc, sp, regs);
	assign_arg(&args.plaintext_ptr, sizeof(args.plaintext_ptr), read_b_loc, sp, regs);

	// store the args in the map
	bpf_map_update_elem(&active_tls_conn_op_map, &tgid_goid, &args, BPF_ANY);

	return 0;
}

SEC("uretprobe/go_tls_conn_read")
int BPF_UPROBE(gotls__probe_ret_tls_conn_read) {
	// bpf_printk("gotls__probe_ret_tls_conn_read");

	// extract the context
	__u64 id   = bpf_get_current_pid_tgid();
	__u32 tgid = id >> 32;
	__u32 pid  = id;

	// get the goid
	__u64 goid = get_goid(ctx);
	if (goid == 0) {
		return 0;
	}

	// construct a composite key for the maps
	struct tgid_goid_t tgid_goid = {};
	tgid_goid.tgid               = tgid;
	tgid_goid.goid               = goid;

	// lookup the args from the entry probe
	struct go_tls_conn_args *args = bpf_map_lookup_elem(&active_tls_conn_op_map, &tgid_goid);
	if (!args) {
		// bpf_printk("gotls__probe_ret_tls_conn_read NO_ARGS = pid: %u", pid);
		return 0;
	}

	// the core logic (in a function to avoid goto for map cleanup)
	process_read(ctx, id, tgid, args);

	// cleanup the args from the entry probe
	bpf_map_delete_elem(&active_tls_conn_op_map, &tgid_goid);

	return 0;
}
