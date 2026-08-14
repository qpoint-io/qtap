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

// Functions that implement the TLS helpers interface
// These are exposed to be registered with the TLS helpers system, but
// no direct calls from OpenSSL to these functions should exist

// Update the ssl -> tlswrap mapping
int update_node_ssl_tls_wrap_map(uintptr_t ssl);

// Retrieve the fd from node internals
int32_t get_fd_from_node(uint64_t pid_tgid, uintptr_t ssl);

// Remove the ssl -> tlswrap mapping
int remove_node_ssl_tls_wrap_map(uintptr_t ssl);
