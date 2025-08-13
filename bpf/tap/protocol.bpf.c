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

#define TLS_RECORD_HEADER_SIZE         5
#define TLS_HANDSHAKE_HEADER_SIZE      4
#define TLS_EXTENSION_HEADER_SIZE      4
#define TLS_SERVER_NAME_EXTENSION_SIZE 5

#define MINIMUM_TLS_HANDSHAKE_SIZE 52

#define HTTP2_PREFACE_LEN 24
const unsigned char HTTP2_PREFACE[HTTP2_PREFACE_LEN] = {
	'P', 'R', 'I', ' ', '*', ' ', 'H', 'T', 'T', 'P', '/', '2', '.', '0', '\r', '\n', '\r', '\n', 'S', 'M', '\r', '\n', '\r', '\n'};

static size_t buf_read_simple(char *dst, uint32_t size, const void *buf, size_t offset) {
	if (bpf_probe_read_user(dst, size, buf + offset) != 0)
		return 0;

	if (size > 1)
		return size - 1;

	return size;
}

static size_t buf_read_iovec(char *dst, uint32_t size, struct buf_info *buf_info, size_t offset) {
	// read from a vector of buffers
	const struct iovec *iov = (const struct iovec *)buf_info->buf;
	size_t bytes_read       = 0;
	size_t bytes_skipped    = 0;

	// don't let the compiler confuse the verifier
	asm volatile("" : "+r"(bytes_read) :);
	size_t bytes_to_read = size;

	// loop through all the buffers in the list
	for (int i = 0; (i < LOOP_LIMIT_SM) && (i < buf_info->iovcnt) && (bytes_read < bytes_to_read); ++i) {
		struct iovec iov_cpy;
		// Safely attempt to read the iovec struct
		if (bpf_probe_read_user(&iov_cpy, sizeof(struct iovec), &iov[i]) != 0) {
			break; // Stop on failure
		}

		size_t bytes_to_skip = 0;
		// if bytes_skipped is less than offset, we need to skip the first offset bytes
		if (bytes_read == 0 && bytes_skipped < offset) {
			// if the remaining bytes to skip is greater than the iov_len, skip the entire iov
			bytes_to_skip = offset - bytes_skipped;

			if (bytes_to_skip >= iov_cpy.iov_len) {
				bytes_skipped += iov_cpy.iov_len;
				continue;
			}
		}

		// now we can safely read from the iovec buffer
		if (bytes_to_read - bytes_read > 0) {
			if (bpf_probe_read_user(&dst[bytes_read], bytes_to_read - bytes_read, iov_cpy.iov_base + bytes_to_skip) != 0) {
				break; // Stop on failure
			}

			// update bytes_read
			bytes_read += bytes_to_read - bytes_read;
		}
	}

	return bytes_read;
}

static size_t buf_read(char *dst, uint32_t size, struct buf_info *buf_info, size_t offset) {
	bool is_iovec = buf_info->iovcnt > 0;

	if (size == 0) {
		return 0;
	}

	// Simple read if not iovoc
	if (!is_iovec) {
		return buf_read_simple(dst, size, buf_info->buf, offset);
	}

	return buf_read_iovec(dst, size, buf_info, offset);
}

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
		if (total_size > count || total_size > MAX_TLS_HANDSHAKE_SIZE) {
			// bpf_printk("capture_tls_client_hello: TLS handshake size too large, count: %u, total_size: %u", count, total_size);
			return false;
		}

		total_size &= (MAX_TLS_HANDSHAKE_SIZE - 1);

		// bpf_printk("capture_tls_client_hello: count: %u, total_size: %u, handshake_body_size: %u", count, total_size, handshake_body_size);

		// Read the entire handshake into our buffer
		if (buf_read_simple((char *)handshake->data, total_size, buf_info->buf, 0) == 0) {
			// bpf_printk("capture_tls_client_hello: Failed to read TLS handshake");
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

	// detect http
	if (conn_info->protocol == P_UNKNOWN)
		detected = detect_http(conn_info, buf_info, count);

	// debug
	// bpf_printk("detect_protocol = pid: %u, fd: %u, protocol: %u\n", conn_info->conn_pid_id.pid, conn_info->conn_pid_id.fd,
	// conn_info->protocol);

	// return status
	return detected;
}
