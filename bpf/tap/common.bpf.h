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

#pragma once

#include "vmlinux.h"
#include "bpf_helpers.h"
#include "bpf_tracing.h"
#include "net.bpf.h"

// This keeps instruction count below BPF's limit of 4096 per probe
#define LOOP_LIMIT    100
#define LOOP_LIMIT_SM 25

// Check if BPF_UPROBE is not defined
#ifndef BPF_UPROBE
// If BPF_UPROBE is not defined, define it as BPF_KPROBE
// This equates user-space probes to kernel-space probes if they are not separately defined
#define BPF_UPROBE BPF_KPROBE
#endif

// Check if BPF_URETPROBE is not defined
#ifndef BPF_URETPROBE
// If BPF_URETPROBE is not defined, define it as BPF_KRETPROBE
// This equates user-space return probes to kernel-space return probes if they are not separately defined
#define BPF_URETPROBE BPF_KRETPROBE
#endif

// Invalid file descriptor
const __s32 INVALID_FD = -1;

// Qpoint PID (this is set by the user-space program)
const volatile u32 qpid = 0;

static __inline int _strncmp(const char *s1, const char *s2, const uint32_t n) {
	for (uint32_t i = 0; i < n; ++i) {
		if (s1[i] != s2[i] || s1[i] == '\0')
			return s1[i] - s2[i];
	}
	return 0;
}

static __inline char *_strstr(const char *haystack, const char *needle) {
	if (!*needle)
		return (char *)haystack;

	for (; *haystack; ++haystack) {
		if (*haystack == *needle) {
			const char *h = haystack, *n = needle;
			while (*h && *n && *h == *n) {
				++h;
				++n;
			}
			if (!*n)
				return (char *)haystack;
		}
	}
	return NULL;
}

static inline __u64 _strlen(const char *s, __u64 max) {
	__u64 len = 0;
	while (len < max && s[len])
		len++;
	return len;
}

// Get the current PID in kernelspace
static __always_inline __u32 bpf_get_current_kernel_pid() {
	// Lower 32 bits = PID (thread ID in userspace)
	return (__u32)bpf_get_current_pid_tgid();
}

// Get the current TGID in kernelspace
static __always_inline __u32 bpf_get_current_kernel_tgid() {
	// Upper 32 bits = TGID (process ID in userspace)
	return bpf_get_current_pid_tgid() >> 32;
}

// Get the current PID in userspace
static __always_inline __u32 bpf_get_current_userspace_pid() {
	// Kernel TGIS is the same as userspace PID
	return bpf_get_current_kernel_tgid();
}

// Get the current TGID in userspace
static __always_inline __u32 bpf_get_current_userspace_tgid() {
	// Kernel PID is the same as userspace TGID
	return bpf_get_current_kernel_pid();
}