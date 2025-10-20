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
#include "net.bpf.h"
#include "socket.bpf.h"

// mgmt_addrs is the struct that contains the management addresses and port
// which are handled by external services such as a transparent proxy
struct mgmt_addrs {
	__u32 ipv4;
	__u32 ipv6[4];
	__u32 port;
};

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, __u32); // zero
	__type(value, struct mgmt_addrs);
	__uint(max_entries, 1);
} mgmt_addrs SEC(".maps");

// determine if a given address is the management address
static __always_inline bool is_management_address(uint8_t addr[16], uint16_t port) {
	// fetch the listen addrs
	__u32 zero = 0;
	struct mgmt_addrs *addrs;
	addrs = bpf_map_lookup_elem(&mgmt_addrs, &zero);

	// if it doesn't exist, return false
	if (!addrs) {
		return false;
	}

	// if the port is not the same, return false
	if (port != addrs->port) {
		// bpf_printk("is_management_address, port mismatch %u != %u", __bpf_ntohs(port), __bpf_ntohs(addrs->port));
		return false;
	} else {
		// bpf_printk("is_management_address, port match %u == %u", __bpf_ntohs(port), __bpf_ntohs(addrs->port));
	}

	// Check if this is an IPv4-mapped IPv6 address using ip4_in_ip6_prefix
	bool is_ipv4_mapped = true;
	for (int i = 0; i < 12; i++) {
		if (addr[i] != ip4_in_ip6_prefix[i]) {
			is_ipv4_mapped = false;
			break;
		}
	}

	if (is_ipv4_mapped) {
		// Compare only the last 32 bits (IPv4 part) with the IPv4 address
		__u32 addr_ipv4;
		__builtin_memcpy(&addr_ipv4, &addr[12], sizeof(__u32));

		if (addr_ipv4 != addrs->ipv4) {
			// bpf_printk("is_management_address, ipv4 mapped ipv6 mismatch %pI4 != %pI4", &addr_ipv4, &addrs->ipv4);
			return false;
		} else {
			return true;
		}
	} else {
		// Regular IPv6 comparison
		__u32 addr_ipv6[4];
		__builtin_memcpy(addr_ipv6, addr, sizeof(addr_ipv6));

		for (int i = 0; i < 4; i++) {
			__u32 addr_part  = addr_ipv6[i];
			__u32 addrs_part = addrs->ipv6[i];

			if (addr_part != addrs_part) {
				// bpf_printk("is_management_address, ipv6 mismatch %pI6 != %pI6", addr_ipv6, addrs->ipv6);
				return false;
			}
		}
		return true;
	}
}