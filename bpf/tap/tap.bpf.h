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

#include "net.bpf.h"

// Data buffer message size. BPF can submit at most this amount of data to a perf buffer.
// Kernel size limit is 32KiB. See https://github.com/iovisor/bcc/issues/2519 for more details
#define MAX_MSG_SIZE 30720 // 30KiB

#define TASK_COMM_LEN 16

// #define MAX_HOSTNAME_LENGTH 255

enum SOCKET_EVENT {
	S_OPEN             = 1ULL,
	S_CLOSE            = 2ULL,
	S_DATA             = 3ULL,
	S_PROTO            = 4ULL,
	S_HOSTNAME         = 5ULL,
	S_TLS_CLIENT_HELLO = 6ULL,
};

enum CONNECTION_TYPE {
	C_CLIENT = 1UL,
	C_SERVER = 2UL,
};

enum SOCKET_TYPE {
	S_UNKNOWN,
	S_TCP,
	S_UDP,
	S_RAW,
	S_ICMP,
};

enum PROTOCOL {
	P_UNKNOWN,
	P_HTTP1,
	P_HTTP2,
	P_DNS,
	P_MONGODB,
};

enum DIRECTION {
	D_INGRESS,
	D_EGRESS,
	D_EGRESS_INTERNAL,
	D_EGRESS_EXTERNAL,
	D_ALL,
};

// address and port composite key
struct addr_port_key {
	__u32 addr[4]; // First 4 bytes for IPv4, all 16 bytes for IPv6
	__u16 port;
};

// source and destination address and port composite key + pid
struct conn_key {
	uint32_t pid;
	uint8_t local_ip[16];
	uint8_t remote_ip[16];
	uint16_t local_port;
	uint16_t remote_port;
};

// A unique ID that is composed of the pid, the file
// descriptor and the creation time of the struct
struct conn_pid_id {
	// Process PID
	uint32_t pid;
	// Process TGID
	uint32_t tgid;
	// The file descriptor to the opened network connection
	int32_t fd;
	// The client or server function
	uint32_t function;
	// Timestamp at the initialization of the struct
	uint64_t tsid;
};

// Contains information collected when a connection is established
struct conn_info {
	// Connection identifier
	struct conn_pid_id conn_pid_id;
	// Socket cookie
	uint64_t cookie;
	// Connection key
	struct conn_key c_key;
	// Address provided to syscall (this is a fallback)
	struct net_addr addr;
	// The number of bytes written on this connection
	int64_t wr_bytes;
	// The number of bytes read on this connection
	int64_t rd_bytes;
	// A flag to indicate that the open event has already been submitted
	bool is_open;
	// A flag to indicate if a connection is encrypted (i.e. TLS using OpenSSL or equivalent)
	bool is_ssl;
	// Detected protocol
	enum PROTOCOL protocol;
	// Conditions were met to ignore this connection
	bool ignore;
};

// Minimum size of an event
struct capture_event {
	// Event type
	uint64_t type;
};

// A struct describing the event that we send to the user mode upon a new connection
struct socket_open_event {
	// Event type
	uint64_t type;
	// The time of the event
	uint64_t timestamp_ns;
	// Connection key
	struct conn_key c_key;
	// Process PID
	uint32_t pid;
	// Process TGID
	uint32_t tgid;
	// Socket type (udp/tcpk/etc)
	enum SOCKET_TYPE socket_type;
	// is this redirected?
	bool is_redirected;
	// The client or server function
	uint32_t source;
};

// Struct describing the close event being sent to the user mode
struct socket_close_event {
	// Event type
	uint64_t type;
	// Timestamp of the close syscall
	uint64_t timestamp_ns;
	// Connection key
	struct conn_key c_key;
	// Total number of bytes written on that connection
	int64_t wr_bytes;
	// Total number of bytes read on that connection
	int64_t rd_bytes;
	// Process PID
	uint32_t pid;
	// Process TGID
	uint32_t tgid;
};

struct socket_data_event {
	// Event type.
	uint64_t type;
	// We split attributes into a separate struct, because BPF gets upset if you do lots of
	// size arithmetic. This makes it so that it's attributes followed by message.
	struct socket_data_attr_t {
		// The timestamp when syscall completed (return probe was triggered)
		uint64_t timestamp_ns;
		// Connection key
		struct conn_key c_key;
		// The type of the actual data that the msg field encodes, which is used by the caller
		// to determine how to interpret the data
		enum DIRECTION direction;
		// The size of the original message. We use this to truncate msg field to minimize the amount
		// of data being transferred
		uint32_t msg_size;
		// A 0-based position number for this event on the connection, in terms of byte position.
		// The position is for the first byte of this message
		uint64_t pos;
		// Process PID
		uint32_t pid;
		// Process TGID
		uint32_t tgid;
	} attr;
	// the data
	char msg[MAX_MSG_SIZE];
};

// When protocol is detected
struct socket_proto_event {
	// Event type
	uint64_t type;
	// The time of the event
	uint64_t timestamp_ns;
	// Connection key
	struct conn_key c_key;
	// Detected protocol
	enum PROTOCOL protocol;
	// Is this an ssl connection?
	bool is_ssl;
};

// // When a hostname has been found for a socket connection
// struct socket_hostname_event {
// 	// Event type
// 	uint64_t type;
// 	struct socket_hostname_attr_t {
// 		// The timestamp when syscall completed (return probe was triggered)
// 		uint64_t timestamp_ns;
// 		// Connection key
// 		struct c_key c_key;
// 		// hostname length
// 		uint8_t hostname_len;
// 	} attr;
// 	// hostname
// 	char hostname[MAX_HOSTNAME_LENGTH];
// };

struct socket_tls_client_hello_event {
	// Event type
	uint64_t type;
	struct socket_tls_client_hello_attr_t {
		// Connection key
		struct conn_key c_key;
		// TLS handshake size
		uint32_t size;
	} attr;
	// TLS handshake data
	unsigned char data[MAX_MSG_SIZE];
};
