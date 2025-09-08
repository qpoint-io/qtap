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

#define BUF_CHUNK_LIMIT 4

// struct for reading buffers from simple buffers and iovec buffers
struct buf_info {
	const void *buf;
	size_t iovcnt;
};

static size_t buf_read_simple(char *dst, uint32_t size, const void *buf, size_t offset) {
  // if the read fails, return 0 (no bytes read)
	if (bpf_probe_read_user(dst, size, buf + offset) != 0)
		return 0;

	// otherwise, return the number of bytes provided since the read was successful
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
	for (int i = 0; (i < BUF_CHUNK_LIMIT) && (i < buf_info->iovcnt) && (bytes_read < bytes_to_read); ++i) {
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
