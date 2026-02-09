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
#include "tap.bpf.h"
#include "buffers.bpf.h"

// given a buffer and connection information, dynamically detect the protocol
static bool detect_protocol(struct conn_info *conn_info, struct buf_info *buf_info, size_t count);

// given a buffer and connection information, detect if the connection is TLS
// sets conn_info->is_ssl to true if TLS is detected
static bool detect_tls(struct conn_info *conn_info, struct buf_info *buf_info, size_t count);

// given a buffer and connection information, detect if the connection is DNS
static bool is_dns(const struct conn_info *conn_info);

// given a buffer and connection information, extract the tls client hello handshake
static bool capture_tls_client_hello(struct socket_tls_client_hello_event *handshake, struct buf_info *buf_info, size_t count);

// given a buffer and connection information, extract the tls server hello handshake
static bool capture_tls_server_hello(struct socket_tls_server_hello_event *handshake, struct buf_info *buf_info, size_t count);
