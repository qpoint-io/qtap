/*
 * rustls TLS interception BPF probe
 * 
 * This code runs using libbpf in the Linux kernel.
 * Copyright 2025 - The Qpoint Authors
 *
 * SPDX-License-Identifier: GPL-2.0
 *
 * This probe hooks aws-lc's EVP_AEAD functions to capture plaintext from
 * rustls applications. Hook points are discovered at runtime via
 * .eh_frame parsing and AES-NI pattern matching.
 *
 * FD Correlation Strategy:
 * Unlike OpenSSL which has SSL* handles we can track, rustls's EVP_AEAD
 * functions don't have access to the socket. We use a different approach:
 * 
 * 1. Track the most recent socket fd per pid_tgid in syscall probes
 * 2. When EVP_AEAD fires, look up the "active fd" for this thread
 * 3. This works because TLS operations happen synchronously on the same thread
 */

#include "common.bpf.h"
#include "socket.bpf.h"
#include "trace.bpf.h"
#include "bpf_helpers.h"
#include "settings.bpf.h"
#include "openssl.bpf.h"

// Track the active/recent socket fd per thread for rustls correlation
// Key: pid_tgid, Value: fd
// Updated by syscall probes, read by rustls probes
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__type(key, uint64_t);   // pid_tgid
	__type(value, int32_t);  // fd
	__uint(max_entries, 4096);
} rustls_active_fd SEC(".maps");

// Arguments saved at seal entry (encryption)
struct rustls_seal_args {
	int32_t fd;         // file descriptor
	uint64_t buf;       // plaintext buffer pointer
	uint64_t buf_len;   // plaintext length
};

// Map to save seal arguments for return probe
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uint64_t);  // pid_tgid
	__type(value, struct rustls_seal_args);
	__uint(max_entries, 1024);
} active_rustls_seal_args SEC(".maps");

// Arguments saved at open entry (decryption)
struct rustls_open_args {
	int32_t fd;         // file descriptor
	uint64_t out_buf;   // output buffer pointer (plaintext after decrypt)
	uint64_t in_len;    // ciphertext length (approx plaintext length)
};

// Map to save open arguments for return probe
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uint64_t);  // pid_tgid
	__type(value, struct rustls_open_args);
	__uint(max_entries, 1024);
} active_rustls_open_args SEC(".maps");

// Get the active fd for this thread (set by syscall probes)
static __always_inline int32_t rustls_get_active_fd(uint64_t pid_tgid) {
	int32_t *fd = bpf_map_lookup_elem(&rustls_active_fd, &pid_tgid);
	if (fd == NULL) {
		return 0;
	}
	return *fd;
}

// Track active fd - called from socket.bpf.c syscall probes
// This is NOT static so it can be called from socket.bpf.c
void rustls_track_active_fd(uint64_t pid_tgid, int32_t fd) {
	if (fd >= 3) {  // Skip stdin/stdout/stderr
		bpf_map_update_elem(&rustls_active_fd, &pid_tgid, &fd, BPF_ANY);
	}
}

/*
 * Probe: EVP_AEAD_CTX_seal_scatter entry (ENCRYPTION)
 * 
 * Captures plaintext BEFORE it gets encrypted.
 * 
 * x86_64 calling convention:
 *   rdi = ctx
 *   rsi = out (ciphertext output)
 *   rdx = out_tag
 *   rcx = out_tag_len
 *   r8  = max_out_tag_len
 *   r9  = nonce
 *   stack+8 = nonce_len
 *   stack+16 = in (PLAINTEXT!) 
 *   stack+24 = in_len
 */
SEC("uprobe/rustls_seal_scatter")
int rustls_probe_entry_seal_scatter(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid = pid_tgid >> 32;

	// DEBUG: First line - verify probe fires
	bpf_printk("rustls/seal_entry: FIRED pid=%d", pid);

	// Get fd from active connection tracking
	int32_t fd = rustls_get_active_fd(pid_tgid);

	// Read stack arguments
	uint64_t sp = PT_REGS_SP(ctx);
	
	uint64_t in_ptr = 0;
	uint64_t in_len = 0;
	
	// Stack args after return address
	bpf_probe_read_user(&in_ptr, sizeof(in_ptr), (void *)(sp + 16));
	bpf_probe_read_user(&in_len, sizeof(in_len), (void *)(sp + 24));

	// Sanity check
	if (in_ptr == 0 || in_len == 0 || in_len > 65536) {
		return 0;
	}

	// Save args for exit handler
	struct rustls_seal_args args = {
		.fd = fd,
		.buf = in_ptr,
		.buf_len = in_len,
	};
	bpf_map_update_elem(&active_rustls_seal_args, &pid_tgid, &args, BPF_ANY);

	return 0;
}

/*
 * Probe: EVP_AEAD_CTX_seal_scatter return
 * Process the captured plaintext
 */
SEC("uretprobe/rustls_seal_scatter")
int rustls_probe_ret_seal_scatter(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid = pid_tgid >> 32;

	// Get saved args
	struct rustls_seal_args *args = bpf_map_lookup_elem(&active_rustls_seal_args, &pid_tgid);
	if (args == NULL) {
		bpf_printk("rustls/seal_ret: no args for pid=%d", pid);
		return 0;
	}

	// Check return value (1 = success in BoringSSL/aws-lc)
	int ret = (int)PT_REGS_RC(ctx);
	if (ret != 1) {
		bpf_printk("rustls/seal_ret: ret=%d (not 1) pid=%d", ret, pid);
		bpf_map_delete_elem(&active_rustls_seal_args, &pid_tgid);
		return 0;
	}

	// Use saved fd or try to get it again
	int32_t fd = args->fd;
	if (fd < 3) {
		fd = rustls_get_active_fd(pid_tgid);
	}
	
	bpf_printk("rustls/seal_ret: pid=%d fd=%d len=%d", pid, fd, args->buf_len);
	
	if (fd < 3) {
		// Still no valid fd
		bpf_printk("rustls/seal_ret: no valid fd for pid=%d", pid);
		bpf_map_delete_elem(&active_rustls_seal_args, &pid_tgid);
		return 0;
	}

	// Construct pid_fd_key for connection lookup
	struct pid_fd_key id = {
		.pid = pid,
		.fd = fd,
	};

	// Build data_args for process_data
	struct data_args data = {
		.fd = fd,
		.buf = args->buf,
		.iovcnt = 0,
		.ssl = 0,
		.ex_bytes = 0,
	};

	// Build socket context
	struct socket_ctx sock_ctx = {
		.id = &id,
		.pid_tgid = pid_tgid,
		.trace_mod = QTAP_OPENSSL,
	};
	bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "rustls/seal");

	// Process the plaintext (EGRESS = data being sent/encrypted)
	process_data(&sock_ctx, D_EGRESS, &data, args->buf_len, /* ssl */ true);

	// Cleanup
	bpf_map_delete_elem(&active_rustls_seal_args, &pid_tgid);

	return 0;
}

/*
 * Probe: EVP_AEAD_CTX_open_gather entry (DECRYPTION)
 * 
 * Save context for capturing plaintext after decryption.
 * 
 * x86_64 calling convention:
 *   rdi = ctx
 *   rsi = out (plaintext output buffer)
 *   rdx = nonce
 *   rcx = nonce_len
 *   r8  = in (ciphertext)
 *   r9  = in_len
 *   stack = in_tag, in_tag_len, ad, ad_len
 */
SEC("uprobe/rustls_open_gather")
int rustls_probe_entry_open_gather(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid = pid_tgid >> 32;

	// DEBUG: First line - verify probe fires
	bpf_printk("rustls/open_entry: FIRED pid=%d", pid);

	// Get fd from active connection tracking
	int32_t fd = rustls_get_active_fd(pid_tgid);

	// Get output buffer (arg2 = rsi) and input length (arg6 = r9)
	uint64_t out_buf = (uint64_t)PT_REGS_PARM2(ctx);
	uint64_t in_len = (uint64_t)PT_REGS_PARM6(ctx);

	// DEBUG: Log args before sanity check
	bpf_printk("rustls/open_entry: out_buf=%llx in_len=%llu", out_buf, in_len);

	// Sanity check
	if (out_buf == 0 || in_len == 0 || in_len > 65536) {
		bpf_printk("rustls/open_entry: FAILED sanity check");
		return 0;
	}

	// Save args
	struct rustls_open_args args = {
		.fd = fd,
		.out_buf = out_buf,
		.in_len = in_len,
	};
	bpf_map_update_elem(&active_rustls_open_args, &pid_tgid, &args, BPF_ANY);

	return 0;
}

/*
 * Probe: EVP_AEAD_CTX_open_gather return
 * Capture the decrypted plaintext
 */
SEC("uretprobe/rustls_open_gather")
int rustls_probe_ret_open_gather(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid = pid_tgid >> 32;

	// Get saved args
	struct rustls_open_args *args = bpf_map_lookup_elem(&active_rustls_open_args, &pid_tgid);
	if (args == NULL) {
		return 0;
	}

	// Check return value
	int ret = (int)PT_REGS_RC(ctx);
	if (ret != 1) {
		bpf_map_delete_elem(&active_rustls_open_args, &pid_tgid);
		return 0;
	}

	// Use saved fd or try again
	int32_t fd = args->fd;
	if (fd < 3) {
		fd = rustls_get_active_fd(pid_tgid);
	}
	
	if (fd < 3) {
		bpf_map_delete_elem(&active_rustls_open_args, &pid_tgid);
		return 0;
	}

	// Construct pid_fd_key
	struct pid_fd_key id = {
		.pid = pid,
		.fd = fd,
	};

	// Build data_args - out_buf now contains decrypted plaintext
	struct data_args data = {
		.fd = fd,
		.buf = args->out_buf,
		.iovcnt = 0,
		.ssl = 0,
		.ex_bytes = 0,
	};

	// Build socket context
	struct socket_ctx sock_ctx = {
		.id = &id,
		.pid_tgid = pid_tgid,
		.trace_mod = QTAP_OPENSSL,
	};
	bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "rustls/open");

	// Process the plaintext (INGRESS = data being received/decrypted)
	process_data(&sock_ctx, D_INGRESS, &data, args->in_len, /* ssl */ true);

	// Cleanup
	bpf_map_delete_elem(&active_rustls_open_args, &pid_tgid);

	return 0;
}
