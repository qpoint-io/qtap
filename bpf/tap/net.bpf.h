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

#include "bpf_endian.h"

// Address family
#ifndef AF_UNSPEC
#define AF_UNSPEC 0
#endif

#ifndef AF_INET
#define AF_INET 2
#endif

#ifndef AF_INET6
#define AF_INET6 10
#endif

// Sock
#define SO_ORIGINAL_DST 80
#define SO_MARK         36
#define SO_COOKIE       57

// A simplified representation of the network address
// This structure and supporting function assume network byte order
struct net_addr {
	// Address family (AF_INET or AF_INET6)
	uint16_t sa_family;
	// Minimum size to hold a IPv6 address. If IPv4 then the address will be found in the first four bytes
	uint8_t addr[16];
	// The address port
	uint16_t port;
};

// The prefix of an IPv4 address stored in an IPv6 address
const uint8_t ip4_in_ip6_prefix[] = {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff};

// Helper function to check if an address is IPv4 mapped in IPv6
static inline int is_ipv4_mapped(const uint8_t addr[16]) {
	for (int i = 0; i < 12; i++) {
		if (addr[i] != ip4_in_ip6_prefix[i]) {
			return 0;
		}
	}
	return 1;
}

// determine if ip address is local (127.0.0.1 etc)
// This function assumes the input address is in network byte order
static inline int is_local_ip(const uint8_t addr[16]) {
	// Determine address family based on prefix
	if (is_ipv4_mapped(addr)) {
		// IPv4 address - actual IPv4 data is in bytes 12-15
		__be32 ip = *(__be32 *)(&addr[12]);

		// check for loopback (127.0.0.0/8)
		if ((ip & bpf_htonl(0xFF000000)) == bpf_htonl(0x7F000000)) {
			return 1;
		}

		// check for link-local (169.254.0.0/16)
		if ((ip & bpf_htonl(0xFFFF0000)) == bpf_htonl(0xA9FE0000)) {
			return 1;
		}

		// check for 0.0.0.0 (used to indicate "any" local address)
		if (ip == 0) {
			return 1;
		}

	} else {
		// IPv6 address
		// check for ipv6 loopback (::1)
		if (addr[0] == 0 && addr[1] == 0 && addr[2] == 0 && addr[3] == 0 && addr[4] == 0 && addr[5] == 0 && addr[6] == 0 && addr[7] == 0 &&
			addr[8] == 0 && addr[9] == 0 && addr[10] == 0 && addr[11] == 0 && addr[12] == 0 && addr[13] == 0 && addr[14] == 0 && addr[15] == 1) {
			return 1;
		}

		// check for IPv6 unspecified address (::)
		if (addr[0] == 0 && addr[1] == 0 && addr[2] == 0 && addr[3] == 0 && addr[4] == 0 && addr[5] == 0 && addr[6] == 0 && addr[7] == 0 &&
			addr[8] == 0 && addr[9] == 0 && addr[10] == 0 && addr[11] == 0 && addr[12] == 0 && addr[13] == 0 && addr[14] == 0 && addr[15] == 0) {
			return 1;
		}

		// IPv4-mapped 127.0.0.0/8 (already handled above when is_ipv4_mapped is true)
		// Keeping this check for completeness if the data doesn't have the standard prefix
		if (addr[0] == 0 && addr[1] == 0 && addr[2] == 0 && addr[3] == 0 && addr[4] == 0 && addr[5] == 0 && addr[6] == 0 && addr[7] == 0 &&
			addr[8] == 0 && addr[9] == 0 && addr[10] == 0xff && addr[11] == 0xff && addr[12] == 0x7f) {
			return 1;
		}
	}

	// not a local IP
	return 0;
}

// determine if ip address is public (external) or private
// This function assumes the input address is in network byte order
static inline int is_private_ip(const uint8_t addr[16]) {
	// Determine address family based on prefix
	if (is_ipv4_mapped(addr)) {
		// IPv4 checks - actual IPv4 data is in bytes 12-15
		__be32 ip = *(__be32 *)(&addr[12]);

		// check for 10.0.0.0/8
		if ((ip & bpf_htonl(0xFF000000)) == bpf_htonl(0x0A000000)) {
			return 1;
		}

		// check for 172.16.0.0/12
		if ((ip & bpf_htonl(0xFFF00000)) == bpf_htonl(0xAC100000)) {
			return 1;
		}

		// check for 192.168.0.0/16
		if ((ip & bpf_htonl(0xFFFF0000)) == bpf_htonl(0xC0A80000)) {
			return 1;
		}

	} else {
		// IPv6 checks
		// check for Unique Local Address (ULA)
		if ((addr[0] & 0xFE) == 0xFC) {
			return 1;
		}

		// check for Link-Local Address
		if (addr[0] == 0xFE && (addr[1] & 0xC0) == 0x80) {
			return 1;
		}

		// check for IPv4-mapped IPv6 address with non-standard prefix
		// (already handled above when is_ipv4_mapped is true)
		// Keeping this check for completeness if the data doesn't have the standard prefix
		if (addr[0] == 0 && addr[1] == 0 && addr[2] == 0 && addr[3] == 0 && addr[4] == 0 && addr[5] == 0 && addr[6] == 0 && addr[7] == 0 &&
			addr[8] == 0 && addr[9] == 0 && addr[10] == 0xFF && addr[11] == 0xFF) {
			__be32 ip = *(__be32 *)(&addr[12]);

			// check for 10.0.0.0/8
			if ((ip & bpf_htonl(0xFF000000)) == bpf_htonl(0x0A000000)) {
				return 1;
			}

			// check for 172.16.0.0/12
			if ((ip & bpf_htonl(0xFFF00000)) == bpf_htonl(0xAC100000)) {
				return 1;
			}

			// check for 192.168.0.0/16
			if ((ip & bpf_htonl(0xFFFF0000)) == bpf_htonl(0xC0A80000)) {
				return 1;
			}
		}
	}

	// not a private IP
	return 0;
}