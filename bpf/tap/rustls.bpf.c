/*
 * rustls TLS interception BPF probe
 * 
 * This code runs using libbpf in the Linux kernel.
 * Copyright 2025 - The Qpoint Authors
 *
 * This program is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation; either version 2 of the License, or
 * (at your option) any later version.
 *
 * SPDX-License-Identifier: GPL-2.0
 *
 * This probe hooks aws-lc's EVP_AEAD functions to capture plaintext from
 * rustls applications. The hook points are discovered at runtime via
 * .eh_frame parsing and AES-NI pattern matching.
 *
 * EVP_AEAD_CTX_seal_scatter signature (x86_64):
 *   rdi = ctx
 *   rsi = out (ciphertext)
 *   rdx = out_tag
 *   rcx = out_tag_len
 *   r8  = max_out_tag_len
 *   r9  = nonce
 *   stack+0  = nonce_len
 *   stack+8  = in (PLAINTEXT)
 *   stack+16 = in_len
 *   stack+24 = ad
 *   stack+32 = ad_len
 */

#include "common.bpf.h"
#include "socket.bpf.h"
#include "trace.bpf.h"
#include "bpf_helpers.h"
#include "settings.bpf.h"
#include "openssl.bpf.h"

// Arguments saved at seal entry, used at exit
struct rustls_seal_args {
	void *out;      // ciphertext output buffer
	void *in;       // plaintext input buffer
	uint64_t in_len;
};

// Map to save seal arguments for return probe
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uint64_t);  // pid_tgid
	__type(value, struct rustls_seal_args);
	__uint(max_entries, 1024);
} active_rustls_seal_args SEC(".maps");

// Arguments saved at open entry
struct rustls_open_args {
	void *out;      // plaintext output buffer
	uint64_t out_len;
};

// Map to save open arguments for return probe
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, uint64_t);  // pid_tgid
	__type(value, struct rustls_open_args);
	__uint(max_entries, 1024);
} active_rustls_open_args SEC(".maps");

/*
 * Probe: EVP_AEAD_CTX_seal_scatter entry
 * Captures plaintext before it gets encrypted
 */
SEC("uprobe/rustls_seal_scatter")
int rustls_probe_entry_seal_scatter(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid = pid_tgid >> 32;

	TRACE_PRINTK("rustls: seal_scatter entry, pid=%d", pid);

	// Read arguments from registers and stack
	// First 6 args in registers, rest on stack
	void *out = (void *)PT_REGS_PARM2(ctx);  // rsi = out
	
	// Stack arguments (after return address)
	// Stack layout: [ret_addr] [shadow space on windows, not linux] [arg7] [arg8] ...
	// On Linux x86_64: stack pointer + 8 (skip return addr) + offset
	void *stack_ptr = (void *)PT_REGS_SP(ctx);
	
	void *in;
	uint64_t in_len;
	
	// Read 'in' from stack (7th argument = stack+8)
	// Note: First stack arg is at rsp+8 (after return addr)
	bpf_probe_read_user(&in, sizeof(in), stack_ptr + 8);
	// Read 'in_len' (8th argument = stack+16)
	bpf_probe_read_user(&in_len, sizeof(in_len), stack_ptr + 16);

	// Sanity check
	if (in == NULL || in_len == 0 || in_len > 65536) {
		return 0;
	}

	// Save args for exit handler
	struct rustls_seal_args args = {
		.out = out,
		.in = in,
		.in_len = in_len,
	};
	bpf_map_update_elem(&active_rustls_seal_args, &pid_tgid, &args, BPF_ANY);

	TRACE_PRINTK("rustls: seal_scatter in=%p in_len=%lu", in, in_len);

	// Capture the plaintext data!
	// This is the data BEFORE encryption
	// Use the same buffer submission as OpenSSL probe
	
	// For now, just log that we captured it
	// Full integration would submit to the ringbuf

	return 0;
}

/*
 * Probe: EVP_AEAD_CTX_seal_scatter return
 * Called after encryption completes
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

	int ret = (int)PT_REGS_RC(ctx);
	
	TRACE_PRINTK("rustls: seal_scatter exit, pid=%d ret=%d len=%lu", pid, ret, args->in_len);

	// Clean up
	bpf_map_delete_elem(&active_rustls_seal_args, &pid_tgid);

	// If successful (ret == 1 in BoringSSL), we captured the plaintext in entry probe
	
	return 0;
}

/*
 * Probe: EVP_AEAD_CTX_open_gather entry
 * Captures context before decryption
 */
SEC("uprobe/rustls_open_gather")
int rustls_probe_entry_open_gather(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid = pid_tgid >> 32;

	TRACE_PRINTK("rustls: open_gather entry, pid=%d", pid);

	// For open, we want to capture the OUTPUT (decrypted plaintext)
	// So we save the out pointer and capture on return
	void *out = (void *)PT_REGS_PARM2(ctx);  // rsi = out
	
	// Read out_len from stack
	void *stack_ptr = (void *)PT_REGS_SP(ctx);
	uint64_t out_len;
	bpf_probe_read_user(&out_len, sizeof(out_len), stack_ptr + 8);

	struct rustls_open_args args = {
		.out = out,
		.out_len = out_len,
	};
	bpf_map_update_elem(&active_rustls_open_args, &pid_tgid, &args, BPF_ANY);

	return 0;
}

/*
 * Probe: EVP_AEAD_CTX_open_gather return
 * Captures decrypted plaintext
 */
SEC("uretprobe/rustls_open_gather")
int rustls_probe_ret_open_gather(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid = pid_tgid >> 32;

	struct rustls_open_args *args = bpf_map_lookup_elem(&active_rustls_open_args, &pid_tgid);
	if (args == NULL) {
		return 0;
	}

	int ret = (int)PT_REGS_RC(ctx);
	
	TRACE_PRINTK("rustls: open_gather exit, pid=%d ret=%d", pid, ret);

	// If successful, args->out now contains decrypted plaintext
	// We could capture it here

	bpf_map_delete_elem(&active_rustls_open_args, &pid_tgid);

	return 0;
}

char LICENSE[] SEC("license") = "GPL";
