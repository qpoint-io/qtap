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
 * EVP_AEAD_CTX_seal_scatter (encryption):
 *   - Plaintext is in the 'in' parameter (7th arg, on stack)
 *   - We capture at ENTRY before encryption
 *
 * EVP_AEAD_CTX_open_gather (decryption):
 *   - Plaintext is written to 'out' parameter (2nd arg)
 *   - We capture at RETURN after decryption
 */

#include "common.bpf.h"
#include "socket.bpf.h"
#include "trace.bpf.h"
#include "bpf_helpers.h"
#include "settings.bpf.h"
#include "openssl.bpf.h"

// Arguments saved at seal entry (encryption)
struct rustls_seal_args {
	int32_t fd;         // file descriptor (from syscall correlation)
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

// Request fd from syscall layer (same pattern as OpenSSL)
static __always_inline void rustls_request_fd(uint64_t pid_tgid) {
	struct fd_request fd_request = {};
	fd_request.is_ssl = true;
	bpf_map_update_elem(&uprobe_fd_requests, &pid_tgid, &fd_request, BPF_ANY);
}

// Get fd from syscall layer response
static __always_inline int32_t rustls_get_fd(uint64_t pid_tgid) {
	struct fd_request *fd_request = bpf_map_lookup_elem(&uprobe_fd_requests, &pid_tgid);
	if (fd_request == NULL) {
		return 0;
	}
	int32_t fd = fd_request->fd;
	bpf_map_delete_elem(&uprobe_fd_requests, &pid_tgid);
	return fd;
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
 *   stack+0 (rsp+8) = nonce_len
 *   stack+8 (rsp+16) = in (PLAINTEXT!) 
 *   stack+16 (rsp+24) = in_len
 */
SEC("uprobe/rustls_seal_scatter")
int rustls_probe_entry_seal_scatter(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid = pid_tgid >> 32;

	// Request fd from syscall layer
	rustls_request_fd(pid_tgid);

	// Read stack arguments
	uint64_t sp = PT_REGS_SP(ctx);
	
	uint64_t in_ptr = 0;
	uint64_t in_len = 0;
	
	// Stack layout after call: [return_addr] [arg7] [arg8] [arg9]...
	// On entry, SP points to return address
	// arg7 (nonce_len) is at sp+8
	// arg8 (in) is at sp+16  
	// arg9 (in_len) is at sp+24
	bpf_probe_read_user(&in_ptr, sizeof(in_ptr), (void *)(sp + 16));
	bpf_probe_read_user(&in_len, sizeof(in_len), (void *)(sp + 24));

	// Sanity check
	if (in_ptr == 0 || in_len == 0 || in_len > 65536) {
		return 0;
	}

	// Save args for exit handler
	struct rustls_seal_args args = {
		.fd = 0,  // Will be filled on exit
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
		return 0;
	}

	// Check return value (1 = success in BoringSSL/aws-lc)
	int ret = (int)PT_REGS_RC(ctx);
	if (ret != 1) {
		bpf_map_delete_elem(&active_rustls_seal_args, &pid_tgid);
		return 0;
	}

	// Get fd from syscall correlation
	int32_t fd = rustls_get_fd(pid_tgid);
	if (fd < 3) {
		// Invalid fd (0, 1, 2 are stdin/stdout/stderr)
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
		.trace_mod = QTAP_OPENSSL, // Reuse OpenSSL trace module for now
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

	// Request fd from syscall layer
	rustls_request_fd(pid_tgid);

	// Get output buffer (arg2 = rsi) and input length (arg6 = r9)
	uint64_t out_buf = (uint64_t)PT_REGS_PARM2(ctx);
	uint64_t in_len = (uint64_t)PT_REGS_PARM6(ctx);

	// Sanity check
	if (out_buf == 0 || in_len == 0 || in_len > 65536) {
		return 0;
	}

	// Save args
	struct rustls_open_args args = {
		.fd = 0,
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

	// Get fd from syscall correlation
	int32_t fd = rustls_get_fd(pid_tgid);
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
	// Note: in_len is ciphertext length, plaintext is slightly smaller due to tag
	// but close enough for our purposes
	process_data(&sock_ctx, D_INGRESS, &data, args->in_len, /* ssl */ true);

	// Cleanup
	bpf_map_delete_elem(&active_rustls_open_args, &pid_tgid);

	return 0;
}
