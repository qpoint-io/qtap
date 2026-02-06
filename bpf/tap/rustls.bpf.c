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

/*
 * ============================================================================
 * RING-SPECIFIC PROBES
 * ============================================================================
 * 
 * ring's aesni_gcm_encrypt/decrypt have different calling conventions than
 * aws-lc's EVP_AEAD functions. Plaintext is in register parameters:
 *
 * ring_core_X_X_X__aesni_gcm_encrypt:
 *   rdi = inp (PLAINTEXT input)
 *   rsi = out (ciphertext output)
 *   rdx = len
 *   rcx = key
 *   r8  = ivec
 *   r9  = Htable
 *   stack = Xi
 *
 * ring_core_X_X_X__aesni_gcm_decrypt:
 *   rdi = inp (ciphertext input)
 *   rsi = out (PLAINTEXT output)
 *   rdx = len
 *   rcx = key
 *   r8  = ivec
 *   r9  = Htable
 *   stack = Xi
 */

// Arguments saved at ring encrypt entry
struct ring_encrypt_args {
	int32_t fd;
	uint64_t inp;      // plaintext buffer (capture on entry)
	uint64_t len;
};

// Arguments saved at ring decrypt entry
struct ring_decrypt_args {
	int32_t fd;
	uint64_t out;      // plaintext buffer (capture on return)
	uint64_t len;
};

// Map to save ring encrypt arguments
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uint64_t);  // pid_tgid
	__type(value, struct ring_encrypt_args);
	__uint(max_entries, 4096);
} active_ring_encrypt_args SEC(".maps");

// Map to save ring decrypt arguments
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uint64_t);  // pid_tgid
	__type(value, struct ring_decrypt_args);
	__uint(max_entries, 4096);
} active_ring_decrypt_args SEC(".maps");

/*
 * Probe: ring aesni_gcm_encrypt entry (EGRESS - data being sent/encrypted)
 * Plaintext is in rdi, capture it before encryption
 */
SEC("uprobe/ring_encrypt")
int ring_probe_entry_encrypt(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid = pid_tgid >> 32;

	bpf_printk("ring/encrypt_entry: FIRED pid=%d", pid);

	// Get fd from active connection tracking
	int32_t fd = rustls_get_active_fd(pid_tgid);

	// Parameters in registers (System V AMD64 ABI)
	uint64_t inp = (uint64_t)PT_REGS_PARM1(ctx);  // plaintext
	uint64_t len = (uint64_t)PT_REGS_PARM3(ctx);  // length

	// Sanity check - ring requires minimum 288 bytes for fast path
	if (inp == 0 || len == 0 || len > 65536) {
		return 0;
	}

	bpf_printk("ring/encrypt_entry: inp=%llx len=%llu fd=%d", inp, len, fd);

	// Save args for return handler
	struct ring_encrypt_args args = {
		.fd = fd,
		.inp = inp,
		.len = len,
	};
	bpf_map_update_elem(&active_ring_encrypt_args, &pid_tgid, &args, BPF_ANY);

	return 0;
}

/*
 * Probe: ring aesni_gcm_encrypt return
 * Process the plaintext we captured on entry
 */
SEC("uretprobe/ring_encrypt")
int ring_probe_ret_encrypt(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid = pid_tgid >> 32;

	// Get saved args
	struct ring_encrypt_args *args = bpf_map_lookup_elem(&active_ring_encrypt_args, &pid_tgid);
	if (args == NULL) {
		return 0;
	}

	bpf_printk("ring/encrypt_ret: pid=%d fd=%d len=%llu", pid, args->fd, args->len);

	int32_t fd = args->fd;
	if (fd < 3) {
		fd = rustls_get_active_fd(pid_tgid);
	}

	if (fd < 3) {
		bpf_map_delete_elem(&active_ring_encrypt_args, &pid_tgid);
		return 0;
	}

	// Construct pid_fd_key
	struct pid_fd_key id = {
		.pid = pid,
		.fd = fd,
	};

	// Build data_args for plaintext
	struct data_args data = {
		.fd = fd,
		.buf = args->inp,
		.iovcnt = 0,
		.ssl = 0,
		.ex_bytes = 0,
	};

	// Build socket context
	struct socket_ctx sock_ctx = {
		.id = &id,
		.pid_tgid = pid_tgid,
		.trace_mod = QTAP_OPENSSL,  // Reuse OpenSSL module for TLS
	};
	bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "ring/encrypt");

	// Process plaintext (EGRESS = data being sent/encrypted)
	process_data(&sock_ctx, D_EGRESS, &data, args->len, /* ssl */ true);

	// Cleanup
	bpf_map_delete_elem(&active_ring_encrypt_args, &pid_tgid);

	return 0;
}

/*
 * Probe: ring aesni_gcm_decrypt entry
 * Save the output buffer pointer - plaintext will be there after return
 */
SEC("uprobe/ring_decrypt")
int ring_probe_entry_decrypt(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid = pid_tgid >> 32;

	bpf_printk("ring/decrypt_entry: FIRED pid=%d", pid);

	// Get fd from active connection tracking
	int32_t fd = rustls_get_active_fd(pid_tgid);

	// Parameters in registers
	uint64_t out = (uint64_t)PT_REGS_PARM2(ctx);  // output buffer (will contain plaintext)
	uint64_t len = (uint64_t)PT_REGS_PARM3(ctx);  // length

	// Sanity check
	if (out == 0 || len == 0 || len > 65536) {
		return 0;
	}

	bpf_printk("ring/decrypt_entry: out=%llx len=%llu fd=%d", out, len, fd);

	// Save args for return handler
	struct ring_decrypt_args args = {
		.fd = fd,
		.out = out,
		.len = len,
	};
	bpf_map_update_elem(&active_ring_decrypt_args, &pid_tgid, &args, BPF_ANY);

	return 0;
}

/*
 * Probe: ring aesni_gcm_decrypt return
 * Now the output buffer contains decrypted plaintext
 */
SEC("uretprobe/ring_decrypt")
int ring_probe_ret_decrypt(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid = pid_tgid >> 32;

	// Get saved args
	struct ring_decrypt_args *args = bpf_map_lookup_elem(&active_ring_decrypt_args, &pid_tgid);
	if (args == NULL) {
		return 0;
	}

	bpf_printk("ring/decrypt_ret: pid=%d fd=%d len=%llu", pid, args->fd, args->len);

	int32_t fd = args->fd;
	if (fd < 3) {
		fd = rustls_get_active_fd(pid_tgid);
	}

	if (fd < 3) {
		bpf_map_delete_elem(&active_ring_decrypt_args, &pid_tgid);
		return 0;
	}

	// Construct pid_fd_key
	struct pid_fd_key id = {
		.pid = pid,
		.fd = fd,
	};

	// Build data_args for plaintext (now in output buffer)
	struct data_args data = {
		.fd = fd,
		.buf = args->out,
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
	bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "ring/decrypt");

	// Process plaintext (INGRESS = data being received/decrypted)
	process_data(&sock_ctx, D_INGRESS, &data, args->len, /* ssl */ true);

	// Cleanup
	bpf_map_delete_elem(&active_ring_decrypt_args, &pid_tgid);

	return 0;
}
