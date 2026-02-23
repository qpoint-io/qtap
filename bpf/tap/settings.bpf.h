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
#include "tap.bpf.h"

// Protocol stream flags (bitmask)
#define STREAM_HTTP_FLAG  (1 << 0)
#define STREAM_REDIS_FLAG (1 << 1)
#define STREAM_MYSQL_FLAG (1 << 2)
#define STREAM_KAFKA_FLAG (1 << 3)

// Helper macros for checking protocol stream flags
#define SHOULD_STREAM_HTTP(flags)  ((flags) & STREAM_HTTP_FLAG)
#define SHOULD_STREAM_REDIS(flags) ((flags) & STREAM_REDIS_FLAG)
#define SHOULD_STREAM_MYSQL(flags) ((flags) & STREAM_MYSQL_FLAG)
#define SHOULD_STREAM_KAFKA(flags) ((flags) & STREAM_KAFKA_FLAG)

// Settings keys
enum SOCKET_SETTINGS {
	SOCK_SETTING_IGNORE_LOOPBACK,
	SOCK_SETTING_DIRECTION,
	SOCK_SETTING_STREAM_PROTOCOLS,
};

// Settings value types
union socket_setting_value {
	// ignore loopback
	bool ignore_loopback;

	// direction
	enum DIRECTION direction;

	// stream protocols (bitmask of STREAM_*_FLAG values)
	__u32 stream_protocols;
};

// Socket settings (from loader app)
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 12);
	__type(key, enum SOCKET_SETTINGS);
	__type(value, union socket_setting_value);
} socket_settings_map SEC(".maps");

static __always_inline bool get_ignore_loopback_setting();
static __always_inline enum DIRECTION get_direction_setting();
static __always_inline __u32 get_stream_protocols_setting();
static __always_inline bool should_stream(enum PROTOCOL protocol);
