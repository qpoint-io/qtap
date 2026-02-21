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

#include "vmlinux.h"
#include "tap.bpf.h"
#include "common.bpf.h"
#include "protocol.bpf.h"
#include "bpf_endian.h"
#include "buffers.bpf.h"

#define TLS_RECORD_HEADER_SIZE         5
#define TLS_HANDSHAKE_HEADER_SIZE      4
#define TLS_EXTENSION_HEADER_SIZE      4
#define TLS_SERVER_NAME_EXTENSION_SIZE 5

#define MINIMUM_TLS_HANDSHAKE_SIZE 52

#define BUF_CHUNK_LIMIT 4

#define HTTP2_PREFACE_LEN 24
const unsigned char HTTP2_PREFACE[HTTP2_PREFACE_LEN] = {
	'P', 'R', 'I', ' ', '*', ' ', 'H', 'T', 'T', 'P', '/', '2', '.', '0', '\r', '\n', '\r', '\n', 'S', 'M', '\r', '\n', '\r', '\n'};

static bool capture_tls_client_hello(struct socket_tls_client_hello_event *handshake, struct buf_info *buf_info, size_t count) {
	if (!handshake || count < MINIMUM_TLS_HANDSHAKE_SIZE || !buf_info->buf) {
		// bpf_printk("capture_tls_client_hello: Failed to read TLS header");
		return false;
	}

	unsigned char tls_header[6] = {0};
	if (buf_read((char *)&tls_header, sizeof(tls_header), buf_info, 0) == 0) {
		// bpf_printk("capture_tls_client_hello: Failed to read TLS header");
		return false;
	}

	if (tls_header[0] == 0x16 && tls_header[1] == 0x03 && tls_header[2] >= 0x01) {
		if (tls_header[5] != 0x01) {
			// bpf_printk("capture_tls_client_hello: not a client hello, actual: %x", tls_header[5]);
			return false;
		}

		uint16_t handshake_body_size = (tls_header[3] << 8) | tls_header[4];
		// bpf_printk("capture_tls_client_hello: TLS handshake detected, size: %u", handshake_body_size);

		// Calculate total size needed (record header + payload)
		uint32_t total_size = TLS_RECORD_HEADER_SIZE + handshake_body_size;

		// Ensure we have enough data and don't exceed our buffer
		if (total_size > count || total_size > MAX_MSG_SIZE) {
			// bpf_printk("capture_tls_client_hello: TLS handshake size too large, count: %u, total_size: %u", count, total_size);
			return false;
		}

		total_size &= (MAX_MSG_SIZE - 1);

		// bpf_printk("capture_tls_client_hello: count: %u, total_size: %u, handshake_body_size: %u", count, total_size, handshake_body_size);

		// Read the entire handshake into our buffer
		if (buf_read((char *)handshake->data, total_size, buf_info, 0) == 0) {
			// bpf_printk("capture_tls_client_hello: Failed to read TLS handshake");
			return false;
		}

		// Store the actual size
		handshake->attr.size = total_size;

		return true;
	}

	return false;
}

static bool capture_tls_server_hello(struct socket_tls_server_hello_event *handshake, struct buf_info *buf_info, size_t count) {
	if (!handshake || count < MINIMUM_TLS_HANDSHAKE_SIZE || !buf_info->buf) {
		return false;
	}

	unsigned char tls_header[6] = {0};
	if (buf_read((char *)&tls_header, sizeof(tls_header), buf_info, 0) == 0) {
		return false;
	}

	// Check for TLS handshake record (0x16) with valid version
	if (tls_header[0] == 0x16 && tls_header[1] == 0x03 && tls_header[2] >= 0x01) {
		// Check for ServerHello (0x02)
		if (tls_header[5] != 0x02) {
			return false;
		}

		uint16_t handshake_body_size = (tls_header[3] << 8) | tls_header[4];

		// Calculate total size needed (record header + payload)
		uint32_t total_size = TLS_RECORD_HEADER_SIZE + handshake_body_size;

		// Ensure we have enough data and don't exceed our buffer
		if (total_size > count || total_size > MAX_MSG_SIZE) {
			return false;
		}

		total_size &= (MAX_MSG_SIZE - 1);

		// Read the entire handshake into our buffer
		if (buf_read((char *)handshake->data, total_size, buf_info, 0) == 0) {
			return false;
		}

		// Store the actual size
		handshake->attr.size = total_size;

		return true;
	}

	return false;
}

static bool detect_http(struct conn_info *conn_info, struct buf_info *buf_info, size_t count) {
	// Initialize to zero to ensure null-termination
	char http1_method_prefix[8] = {0};

	// An HTTP/1.x request and response have a minimum size which we could calculate but an assumption
	// is made that it must be at least as long as the what will be read from the buffer and checked.
	// This also makes an assumption that anything writing data chunks will do so with reasonable
	// buffer sizes (i.e. not 1-byte buffers)
	if (count < sizeof(http1_method_prefix) - 1 || !buf_info->buf) // Minimum length for methods or headers
		return false;

	// Safely read the first bytes from the user buffer to check if this might be HTTP/1.x
	if (buf_read((char *)&http1_method_prefix, sizeof(http1_method_prefix) - 1, buf_info, 0) == 0)
		return false;

	// bpf_printk(
	// 	"Checking HTTP method = fd: %d, pid: %llu, count: %d, s: %s\n", conn_info->conn_pid_id.fd, conn_info->conn_pid_id.pid, count,
	// http1_method_prefix);

	// Order matters, the most popular are first. It's a small optimization
	// First, detect HTTP/1.1
	if (_strncmp(http1_method_prefix, "GET", 3) == 0 || _strncmp(http1_method_prefix, "HTTP", 4) == 0 ||
		_strncmp(http1_method_prefix, "POST", 4) == 0 || _strncmp(http1_method_prefix, "PUT", 3) == 0 ||
		_strncmp(http1_method_prefix, "DELETE", 6) == 0 || _strncmp(http1_method_prefix, "PATCH", 5) == 0 ||
		_strncmp(http1_method_prefix, "CONNECT", 7) == 0 || _strncmp(http1_method_prefix, "HEAD", 4) == 0 ||
		_strncmp(http1_method_prefix, "OPTIONS", 7) == 0) {
		conn_info->protocol = P_HTTP1;
		return true;
	}

	// Next, look for HTTP/2
	// The size of the buffer must be at least as sone the HTTP/2 preface length. Also, if the connection
	// has already been identified as HTTP/1.x then there is no need to check for HTTP/2
	if (count < HTTP2_PREFACE_LEN)
		return false;

	// This is a minor optmization. One can check to see if the first three characters of the HTTP/1.x
	// prefix buffer match "PRI"
	if (_strncmp(http1_method_prefix, "PRI", 3) != 0)
		return false;

	// There is no need to null terminate this as a direct byte for byte comparison, up to a length,
	// will be performed
	char http2_preface_check[HTTP2_PREFACE_LEN] = {};
	if (buf_read((char *)&http2_preface_check, sizeof(http2_preface_check), buf_info, 0) == 0)
		return false;

	// Perform a byte-for-byte comparison and if any are different than this isn't HTTP/2
	for (size_t i = 0; i < HTTP2_PREFACE_LEN; ++i) {
		if (http2_preface_check[i] != HTTP2_PREFACE[i])
			return false;
	}

	// The preface matched and so we have HTTP/2
	conn_info->protocol = P_HTTP2;

	// report
	return true;
}

static bool is_dns(const struct conn_info *conn_info) {
	// simple check for now, just look for port 53
	return conn_info->addr.port == __bpf_htons(53);
}

static bool detect_dns(struct conn_info *conn_info) {
	if (is_dns(conn_info)) {
		// set protocol to dns
		conn_info->protocol = P_DNS;
		return true;
	}

	// default (not dns)
	return false;
}

// Reference: https://www.mongodb.com/docs/manual/reference/mongodb-wire-protocol/
static bool detect_mongodb(struct conn_info *conn_info, struct buf_info *buf_info, size_t count) {
	// MongoDB wire protocol messages have at least 16 bytes header
	if (count < 16 || !buf_info->buf) {
		return false;
	}

	// Read the first 16 bytes for MongoDB wire protocol header
	unsigned char hdr[16] = {0};
	if (buf_read((char *)&hdr, sizeof(hdr), buf_info, 0) == 0) {
		return false;
	}

	// MongoDB wire protocol structure:
	// https://www.mongodb.com/docs/manual/reference/mongodb-wire-protocol/#standard-message-header
	// - 4 bytes: message length (little-endian)
	// - 4 bytes: request ID
	// - 4 bytes: response to
	// - 4 bytes: opCode (little-endian)

	// Get message length (little-endian)
	uint32_t msg_len = hdr[0] | (hdr[1] << 8) | (hdr[2] << 16) | (hdr[3] << 24);

	// Get opCode (little-endian)
	uint32_t opcode = hdr[12] | (hdr[13] << 8) | (hdr[14] << 16) | (hdr[15] << 24);

	// Valid MongoDB opcodes
	// https://www.mongodb.com/docs/manual/reference/mongodb-wire-protocol/#opcodes
	bool valid_opcode = (opcode == 1 || // OP_REPLY
						 opcode == 2001 || // OP_UPDATE
						 opcode == 2002 || // OP_INSERT
						 opcode == 2003 || // RESERVED
						 opcode == 2004 || // OP_QUERY
						 opcode == 2005 || // OP_GET_MORE
						 opcode == 2006 || // OP_DELETE
						 opcode == 2007 || // OP_KILL_CURSORS
						 opcode == 2010 || // OP_COMMAND
						 opcode == 2011 || // OP_COMMANDREPLY
						 opcode == 2012 || // OP_COMPRESSED
						 opcode == 2013); // OP_MSG

	// Sanity check: message length should be reasonable (at least header size, not too large)
	// Note: 16MiB (16777216 bytes) is the max for a BSON submission. However, when doing bulk inserts the max
	// is 48MiB (50331648 bytes). In the interest of flexibility, support the upper limit.
	bool valid_length = (msg_len >= 16 && msg_len <= 50331648);

	if (valid_opcode && valid_length) {
		conn_info->protocol = P_MONGODB;
		return true;
	}

	return false;
}

static bool detect_tls(struct conn_info *conn_info, struct buf_info *buf_info, size_t count) {
	// bpf_printk("detect_tls: Starting TLS detection, count: %zu", count);

	// TLS record header is at least 5 bytes
	if (count < 5 || !buf_info->buf) {
		// bpf_printk("detect_tls: Buffer too small or null, count: %zu", count);
		return false;
	}

	unsigned char tls_header[5] = {0};
	if (buf_read((char *)&tls_header, sizeof(tls_header), buf_info, 0) == 0) {
		// bpf_printk("detect_tls: Failed to read TLS header");
		return false;
	}

	// bpf_printk("detect_tls: Header bytes: %02x %02x %02x %02x %02x",
	//            tls_header[0], tls_header[1], tls_header[2], tls_header[3], tls_header[4]);

	// Check for TLS record type (0x16 for Handshake) and version (0x03 0x01 for TLS 1.0 or higher)
	if (tls_header[0] == 0x16 && tls_header[1] == 0x03 && tls_header[2] >= 0x01) {
		conn_info->is_ssl = true;
		// bpf_printk("detect_tls: TLS detected, version: %02x %02x", tls_header[1], tls_header[2]);
		return true;
	}

	// bpf_printk("detect_tls: Not TLS");
	return false;
}

// Redis RESP protocol detection
// Redis commands are sent as RESP arrays: *<count>\r\n$<len>\r\n<cmd>...
static bool detect_mysql(struct conn_info *conn_info, struct buf_info *buf_info, size_t count) {
	// MySQL Server Greeting (handshake) detection
	// The server sends this as the first packet on a new connection (ingress)
	// Format: 4-byte header (3 bytes length + 1 byte sequence number) + payload
	// For the initial greeting: sequence number = 0x00, payload[0] = 0x0a (protocol version 10)
	if (count < 5 || !buf_info->buf) {
		return false;
	}

	char header[5] = {0};
	if (buf_read(header, sizeof(header), buf_info, 0) == 0) {
		return false;
	}

	// Sequence number must be 0 (first packet from server)
	if (header[3] != 0x00) {
		return false;
	}

	// Protocol version must be 10 (0x0a)
	if (header[4] != 0x0a) {
		return false;
	}

	// Validate length field: 3 little-endian bytes
	// MySQL greeting packets are typically 60-120 bytes, payload length > 0
	uint32_t payload_len = (uint32_t)(unsigned char)header[0] |
	                       ((uint32_t)(unsigned char)header[1] << 8) |
	                       ((uint32_t)(unsigned char)header[2] << 16);

	// Payload length should be reasonable for a greeting (at least 1, typically > 50)
	if (payload_len < 1 || payload_len > 0xFFFFFF) {
		return false;
	}

	conn_info->protocol = P_MYSQL;
	// Watch for STARTTLS upgrade on MySQL connections
	conn_info->tls_upgrade_pending = true;
	return true;
}

static bool detect_redis(struct conn_info *conn_info, struct buf_info *buf_info, size_t count) {
	if (count < 4 || !buf_info->buf) {
		return false;
	}

	char header[4] = {0};
	if (buf_read(header, sizeof(header), buf_info, 0) == 0) {
		return false;
	}

	// Check for RESP array command pattern: *<digit>
	// This is highly specific to Redis client connections
	if (header[0] == '*' && header[1] >= '1' && header[1] <= '9') {
		conn_info->protocol = P_REDIS;
		return true;
	}

	return false;
}

// Kafka detection: 4-byte length + 2-byte ApiKey (0-67) + 2-byte ApiVersion (0-16) + 4-byte CorrelationID
// ApiVersions (18) or Metadata (3) are most common first requests.
// Note: the parser accepts ApiKey up to 74 for forward-compatibility, but BPF
// caps at 67 (the highest currently named constant) to keep detection tight.
static bool detect_kafka(struct conn_info *conn_info, struct buf_info *buf_info, size_t count) {
	if (count < 14 || !buf_info->buf) {
		return false;
	}

	unsigned char hdr[14] = {0};
	if (buf_read((char *)&hdr, sizeof(hdr), buf_info, 0) == 0) {
		return false;
	}

	// Length field (4 bytes, big-endian) — must be reasonable (> 8, < 100MB)
	__u32 length = (__u32)hdr[0] << 24 | (__u32)hdr[1] << 16 | (__u32)hdr[2] << 8 | hdr[3];
	if (length < 8 || length > 104857600) {
		return false;
	}

	// ApiKey (2 bytes) — valid range 0-67
	__u16 api_key = (__u16)hdr[4] << 8 | hdr[5];
	if (api_key > 67) {
		return false;
	}

	// ApiVersion (2 bytes) — valid range 0-16
	__u16 api_version = (__u16)hdr[6] << 8 | hdr[7];
	if (api_version > 16) {
		return false;
	}

	// Boost confidence: first request is usually ApiVersions (18) or Metadata (3)
	if (api_key == 18 || api_key == 3) {
		conn_info->protocol = P_KAFKA;
		return true;
	}

	// For other ApiKeys, require two additional checks to reduce false positives
	// from binary protocols (e.g. gRPC DATA frames, custom framing) whose first
	// 14 bytes happen to satisfy the ApiKey/ApiVersion range checks above.

	// 1. CorrelationID: Kafka clients start at 0 or 1 on a new connection and
	//    increment monotonically. Cap at 65535 instead of 1,000,000 — high
	//    values indicate this is not a fresh Kafka connection.
	__u32 corr_id = (__u32)hdr[8] << 24 | (__u32)hdr[9] << 16 | (__u32)hdr[10] << 8 | hdr[11];
	if (corr_id > 65535) {
		return false;
	}

	// 2. Frame-length coherence: require the first write to contain exactly one
	//    complete Kafka frame (length_field + 4 == packet size). Binary protocols
	//    that pass the field-range checks by coincidence rarely also satisfy this
	//    framing invariant. This may produce a false negative if a client
	//    coalesces multiple requests into a single write on the first packet, but
	//    that is uncommon during connection setup.
	if (length + 4 != (__u32)count) {
		return false;
	}

	conn_info->protocol = P_KAFKA;
	return true;
}

static bool detect_protocol(struct conn_info *conn_info, struct buf_info *buf_info, size_t count) {
	// set the default protocol to unknown
	conn_info->protocol = P_UNKNOWN;

	// initialize detected to false
	bool detected = false;

	// detect dns
	if (conn_info->protocol == P_UNKNOWN)
		detected = detect_dns(conn_info);

	// detect mongodb - check before HTTP as MongoDB binary data might be misdetected as HTTP
	if (conn_info->protocol == P_UNKNOWN)
		detected = detect_mongodb(conn_info, buf_info, count);

	// detect kafka - check before HTTP as Kafka binary frames might be misdetected
	if (conn_info->protocol == P_UNKNOWN)
		detected = detect_kafka(conn_info, buf_info, count);

	// detect redis - check before HTTP as Redis RESP commands start with *
	if (conn_info->protocol == P_UNKNOWN)
		detected = detect_redis(conn_info, buf_info, count);

	// detect mysql - server greeting on ingress
	if (conn_info->protocol == P_UNKNOWN)
		detected = detect_mysql(conn_info, buf_info, count);

	// detect http
	if (conn_info->protocol == P_UNKNOWN)
		detected = detect_http(conn_info, buf_info, count);

	// debug
	// bpf_printk("detect_protocol = pid: %u, fd: %u, protocol: %u\n", conn_info->conn_pid_id.pid, conn_info->conn_pid_id.fd,
	// conn_info->protocol);

	// return status
	return detected;
}
