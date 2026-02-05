/*
 * rustls TLS interception BPF probe
 * 
 * This code runs using libbpf in the Linux kernel.
 * Copyright 2025 - The Qpoint Authors
 *
 * SPDX-License-Identifier: GPL-2.0
 *
 * This probe hooks aws-lc's EVP_AEAD functions to capture plaintext from
 * rustls applications.
 */

#include "common.bpf.h"
#include "socket.bpf.h"
#include "trace.bpf.h"
#include "bpf_helpers.h"
#include "settings.bpf.h"
#include "openssl.bpf.h"

// Arguments saved at seal entry, used at exit
struct rustls_seal_args {
	uint64_t out;       // ciphertext output buffer (as uintptr)
	uint64_t in;        // plaintext input buffer (as uintptr)
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
	uint64_t out;       // plaintext output buffer (as uintptr)
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

	// Read arguments from registers and stack
	uint64_t out = (uint64_t)PT_REGS_PARM2(ctx);  // rsi = out
	uint64_t stack_ptr = (uint64_t)PT_REGS_SP(ctx);
	
	uint64_t in = 0;
	uint64_t in_len = 0;
	
	// Read 'in' from stack (after nonce_len at stack+8)
	bpf_probe_read_user(&in, sizeof(in), (void *)(stack_ptr + 8));
	bpf_probe_read_user(&in_len, sizeof(in_len), (void *)(stack_ptr + 16));

	// Sanity check
	if (in == 0 || in_len == 0 || in_len > 65536) {
		return 0;
	}

	// Save args for exit handler
	struct rustls_seal_args args = {
		.out = out,
		.in = in,
		.in_len = in_len,
	};
	bpf_map_update_elem(&active_rustls_seal_args, &pid_tgid, &args, BPF_ANY);

	return 0;
}

/*
 * Probe: EVP_AEAD_CTX_seal_scatter return
 */
SEC("uretprobe/rustls_seal_scatter")
int rustls_probe_ret_seal_scatter(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	struct rustls_seal_args *args = bpf_map_lookup_elem(&active_rustls_seal_args, &pid_tgid);
	if (args == NULL) {
		return 0;
	}

	// Clean up
	bpf_map_delete_elem(&active_rustls_seal_args, &pid_tgid);
	
	return 0;
}

/*
 * Probe: EVP_AEAD_CTX_open_gather entry
 */
SEC("uprobe/rustls_open_gather")
int rustls_probe_entry_open_gather(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	uint64_t out = (uint64_t)PT_REGS_PARM2(ctx);  // rsi = out
	uint64_t stack_ptr = (uint64_t)PT_REGS_SP(ctx);
	uint64_t out_len = 0;
	bpf_probe_read_user(&out_len, sizeof(out_len), (void *)(stack_ptr + 8));

	struct rustls_open_args args = {
		.out = out,
		.out_len = out_len,
	};
	bpf_map_update_elem(&active_rustls_open_args, &pid_tgid, &args, BPF_ANY);

	return 0;
}

/*
 * Probe: EVP_AEAD_CTX_open_gather return
 */
SEC("uretprobe/rustls_open_gather")
int rustls_probe_ret_open_gather(struct pt_regs *ctx) {
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	struct rustls_open_args *args = bpf_map_lookup_elem(&active_rustls_open_args, &pid_tgid);
	if (args == NULL) {
		return 0;
	}

	bpf_map_delete_elem(&active_rustls_open_args, &pid_tgid);

	return 0;
}
