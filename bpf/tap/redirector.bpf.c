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
#include "bpf_helpers.h"
#include "bpf_tracing.h"
#include "bpf_endian.h"
#include "sock_pid_fd.bpf.h"
#include "tap.bpf.h"
#include "common.bpf.h"
#include "net.bpf.h"
#include "process.bpf.h"
#include "trace.bpf.h"
#include "sock_utils.bpf.h"

struct rdr_socket {
	__u32 src_addr[4];
	__u16 src_port;
	__u32 dst_addr[4];
	__u16 dst_port;
};

// map_ports: addr_port_key -> socket cookie
// addr_port_key is the address and port of the socket in network byte order
// map_ports uses a source ip and port key to map the socket cookie
// note: the values in this map are only used during startup so we use an
// lru hash map to avoid using a lot of memory and to avoid having to implement
// a cleanup function
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__type(key, struct addr_port_key);
	__type(value, __u64); // socket cookie
	__uint(max_entries, 4096);
} map_ports SEC(".maps");

// map_socks: socket cookie -> rdr_socket
// map_socks uses a socket cookie key to map to the original destination rdr_socket
// note: the values in this map are only used during startup so we use an
// lru hash map to avoid using a lot of memory and to avoid having to implement
// a cleanup function
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__type(key, __u64); // socket cookie
	__type(value, struct rdr_socket);
	__uint(max_entries, 4096);
} map_socks SEC(".maps");

static inline bool should_ignore_pid(__u32 pid) {
	return pid == qpid;
}

static inline bool set_ipv4_listen_addr(__u32 *proxy_ipv4, __u32 *proxy_port) {
	__u32 zero = 0;
	struct mgmt_addrs *addrs;

	addrs = bpf_map_lookup_elem(&mgmt_addrs, &zero);
	if (!addrs) {
		// bpf_printk("failed to lookup mgmt_addrs");
		return false;
	}

	// bpf_printk("egress listener ipv4 %x:%d", addrs->ipv4, addrs->port);

	// Set the IP from returned struct
	*proxy_ipv4 = addrs->ipv4;

	// Set the port from returned struct
	*proxy_port = addrs->port;

	return true;
}

static inline bool set_ipv6_listen_addr(bool is_ipv4_mapped, __u32 proxy_ipv6[4], __u32 *proxy_port) {
	__u32 zero = 0;
	struct mgmt_addrs *addrs;

	// Get item zero from mgmt_addrs map
	addrs = bpf_map_lookup_elem(&mgmt_addrs, &zero);
	if (!addrs) {
		// bpf_printk("failed to lookup mgmt_addrs");
		return false;
	}

	// If the original connection was IPv4-mapped, we should always redirect to
	// the IPv4-mapped version of our proxy address
	if (is_ipv4_mapped) {
		// Set the IPv4 address in mapped format
		proxy_ipv6[0] = 0x00000000;
		proxy_ipv6[1] = 0x00000000;
		proxy_ipv6[2] = 0xffff0000;
		proxy_ipv6[3] = addrs->ipv4;
	} else {
		proxy_ipv6[0] = addrs->ipv6[0];
		proxy_ipv6[1] = addrs->ipv6[1];
		proxy_ipv6[2] = addrs->ipv6[2];
		proxy_ipv6[3] = addrs->ipv6[3];
	}

	*proxy_port = addrs->port;
	return true;
}

// This hook is triggered when a process (inside the cgroup where this is attached) calls the connect() syscall
// It redirect the connection to the transparent proxy but stores the original destination address and port in a map_socks
SEC("cgroup/connect4")
int cg_connect4(struct bpf_sock_addr *ctx) {
	// Only forward IPv4 TCP connections
	if (ctx->user_family != AF_INET) {
		return 1;
	}
	if (ctx->protocol != IPPROTO_TCP) {
		return 1;
	}

	// get the pid
	__u32 pid = bpf_get_current_pid_tgid() >> 32;

	// if the process is in the ignore pid set, ignore
	if (should_ignore_pid(pid)) {
		// bpf_printk("cg_connect6, should_ignore_pid");
		return 1;
	}

	// lookup the process meta
	struct process_meta *meta = get_process_meta(pid);
	if (!meta) {
		return 1;
	}

	// if the process strategy is not forward or proxy, ignore
	if (meta->qpoint_strategy != QP_FORWARD && meta->qpoint_strategy != QP_PROXY) {
		return 1;
	}

	struct net_addr net_addr = {};
	net_addr.sa_family       = ctx->user_family;
	net_addr.port            = ctx->user_port;
	__builtin_memcpy(net_addr.addr, &ctx->user_ip4, sizeof(ctx->user_ip4));

	// bpf_printk("cg_connect4, ip: %pI4, port: %u", &net_addr.addr[0], __bpf_ntohs(net_addr.port));
	TRACE_REDIRECTOR(pid, "cg_connect4", TRACE_INT("pid", pid), TRACE_IP4("ip", net_addr.addr), TRACE_PORT("port", net_addr.port));

	// Filter out 0.0.0.0 and localhost addresses
	if (is_local_ip(&net_addr)) {
		return 1;
	}

	// Filter out forwarder addresses. This shouldn't ever happen but is necessary to prevent accidental
	// redirect loops
	if (is_management_address(&net_addr)) {
		// bpf_printk("cg_connect4, is_management_address");
		return 1;
	}

	// get the capture direction from settings
	enum DIRECTION capture_direction = get_direction_setting();

	if (capture_direction == D_EGRESS_EXTERNAL) {
		if (is_private_ip(&net_addr)) {
			return 1;
		}
	} else if (capture_direction == D_EGRESS_INTERNAL) {
		if (!is_private_ip(&net_addr)) {
			return 1;
		}
	}

	// Unique identifier for the destination socket
	__u64 cookie = bpf_get_socket_cookie(ctx);
	// bpf_printk("cookie: %llu", cookie);

	// Store destination socket under cookie key
	struct rdr_socket sock;
	__builtin_memset(&sock, 0, sizeof(sock));

	// This field contains the IPv4 address passed to the connect() syscall
	// a.k.a. connect to this socket destination address
	sock.dst_addr[0] = ctx->user_ip4;
	// This field contains the port number passed to the connect() syscall
	// a.k.a. connect to this socket destination port
	sock.dst_port = ctx->user_port;
	if (bpf_map_update_elem(&map_socks, &cookie, &sock, BPF_ANY) < 0) {
		// bpf_printk("failed to update map_socks");
		return 1;
	}

	// Redirect the connection to the proxy
	__u32 proxy_ip;
	__u32 proxy_port;
	if (!set_ipv4_listen_addr(&proxy_ip, &proxy_port)) {
		// bpf_printk("failed to set listen addr");
		return 1;
	} else {
		// bpf_printk("cg_connect4, proxy_ip: %pI4, proxy_port: %u", &proxy_ip, __bpf_ntohs(proxy_port));
	}

	ctx->user_ip4  = proxy_ip;
	ctx->user_port = proxy_port;

	// bpf_printk("redirecting client connection to proxy");

	return 1;
}

// Add a new hook for IPv6 connections
SEC("cgroup/connect6")
int cg_connect6(struct bpf_sock_addr *ctx) {
	if (ctx->protocol != IPPROTO_TCP) {
		return 1;
	}

	// Only forward IPv6 TCP connections
	if (ctx->user_family != AF_INET6) {
		return 1;
	}

	// get the pid
	__u32 pid = bpf_get_current_pid_tgid() >> 32;

	// if the process is in the ignore pid set, ignore
	if (should_ignore_pid(pid)) {
		// bpf_printk("cg_connect6, should_ignore_pid");
		return 1;
	}

	// lookup the process meta
	struct process_meta *meta = get_process_meta(pid);
	if (!meta) {
		return 1;
	}

	// if the process strategy is not forward or proxy, ignore
	if (meta->qpoint_strategy != QP_FORWARD && meta->qpoint_strategy != QP_PROXY) {
		return 1;
	}

	struct net_addr net_addr = {};
	net_addr.sa_family       = ctx->user_family;
	net_addr.port            = ctx->user_port;
	__builtin_memcpy(net_addr.addr, &ctx->user_ip6, sizeof(ctx->user_ip6));

	// bpf_printk("cg_connect6, ip: %pI6, port: %u", net_addr.addr, __bpf_ntohs(net_addr.port));
	TRACE_REDIRECTOR(pid, "cg_connect6", TRACE_INT("pid", pid), TRACE_IP6("ip", net_addr.addr), TRACE_PORT("port", net_addr.port));

	// Filter out [::] and localhost addresses
	// It also filters out local IPv4-mapped IPv6 addresses
	if (is_local_ip(&net_addr)) {
		return 1;
	}

	// Filter out forwarder addresses. This shouldn't ever happen but is necessary to prevent accidental
	// redirect loops
	if (is_management_address(&net_addr)) {
		// bpf_printk("cg_connect6, is_management_address");
		return 1;
	}

	// get the capture direction from settings
	enum DIRECTION capture_direction = get_direction_setting();

	if (capture_direction == D_EGRESS_EXTERNAL) {
		if (is_private_ip(&net_addr)) {
			return 1;
		}
	} else if (capture_direction == D_EGRESS_INTERNAL) {
		if (!is_private_ip(&net_addr)) {
			return 1;
		}
	}

	// Unique identifier for the destination socket
	__u64 cookie = bpf_get_socket_cookie(ctx);
	// bpf_printk("cookie: %llu", cookie);

	// Temporary storage for the destination address
	__u32 dst_addr[4];
	__builtin_memset(&dst_addr, 0, sizeof(dst_addr));
	dst_addr[0] = ctx->user_ip6[0];
	dst_addr[1] = ctx->user_ip6[1];
	dst_addr[2] = ctx->user_ip6[2];
	dst_addr[3] = ctx->user_ip6[3];

	// Store destination socket under cookie key
	struct rdr_socket sock;
	__builtin_memset(&sock, 0, sizeof(sock));

	// This field contains the IPv4 address passed to the connect() syscall
	// a.k.a. connect to this socket destination address and port
	__builtin_memcpy(sock.dst_addr, dst_addr, sizeof(dst_addr));
	// This field contains the port number passed to the connect() syscall
	// a.k.a. connect to this socket destination port
	sock.dst_port = ctx->user_port;
	if (bpf_map_update_elem(&map_socks, &cookie, &sock, BPF_ANY) < 0) {
		// bpf_printk("failed to update map_socks");
		return 1;
	}

	bool is_ipv4_mapped = (dst_addr[0] == 0x00000000 && dst_addr[1] == 0x00000000 && dst_addr[2] == 0xffff0000);

	// Redirect the connection to the proxy
	__u32 proxy_ip[4];
	__builtin_memset(&proxy_ip, 0, sizeof(proxy_ip));
	__u32 proxy_port;
	if (!set_ipv6_listen_addr(is_ipv4_mapped, proxy_ip, &proxy_port)) {
		// bpf_printk("failed to set IPv6 listen addr");
		return 1;
	} else {
		// bpf_printk("cg_connect6, is_ipv4_mapped: %d, proxy_ip: %pI6, proxy_port: %u", is_ipv4_mapped, proxy_ip, __bpf_ntohs(proxy_port));
	}
	ctx->user_ip6[0] = proxy_ip[0];
	ctx->user_ip6[1] = proxy_ip[1];
	ctx->user_ip6[2] = proxy_ip[2];
	ctx->user_ip6[3] = proxy_ip[3];
	ctx->user_port   = proxy_port;

	// bpf_printk("redirecting client connection to proxy");

	return 1;
}

// This program is called whenever there's a socket operation on a particular cgroup (retransmit timeout,
// connection establishment...)
// When the connection is established we record the client's source port and the socket's cookie.
// The source port is used to look up the socket's cookie in the map_ports map.
// The socket's cookie is used to look up the original destination information in the map_socks map.
SEC("sockops")
int cg_sock_ops(struct bpf_sock_ops *ctx) {
	// Only forward on IPv4 or IPv6 connections
	if (ctx->family != AF_INET && ctx->family != AF_INET6) {
		return 0;
	}

	__u64 cookie = bpf_get_socket_cookie(ctx);
	struct rdr_socket *sock;

	switch (ctx->op) {
	case BPF_SOCK_OPS_PASSIVE_ESTABLISHED_CB:
	case BPF_SOCK_OPS_ACTIVE_ESTABLISHED_CB:
		// bpf_printk("sockops connection established");

		// Lookup the socket in the map for the corresponding cookie
		// In case the socket is present, store the source port and socket mapping
		sock = bpf_map_lookup_elem(&map_socks, &cookie);
		if (sock) {
			struct addr_port_key key = {0};
			key.port                 = __bpf_htons(ctx->local_port);
			if (ctx->family == AF_INET) {
				__builtin_memcpy(&key.addr[0], &ctx->local_ip4, sizeof(ctx->local_ip4));
				// bpf_printk("cg_sock_ops, key, ip: %pI4, port: %u", &key.addr[0], __bpf_ntohs(key.port));
				if (bpf_map_update_elem(&map_ports, &key, &cookie, BPF_ANY) < 0) {
					// bpf_printk("failed to update map_ports");
					return 1;
				}
			} else if (ctx->family == AF_INET6) {
				bool is_ipv4_mapped = (ctx->local_ip6[0] == 0x00000000 && ctx->local_ip6[1] == 0x00000000 && ctx->local_ip6[2] == 0xffff0000);
				if (is_ipv4_mapped) {
					__builtin_memcpy(&key.addr[0], &ctx->local_ip6[3], sizeof(ctx->local_ip6[3]));
					// bpf_printk("cg_sock_ops, key, ip: %pI4, port: %u", &key.addr[0], __bpf_ntohs(key.port));
				} else {
					__builtin_memcpy(&key.addr[0], &ctx->local_ip6[0], sizeof(ctx->local_ip6[0]));
					__builtin_memcpy(&key.addr[1], &ctx->local_ip6[1], sizeof(ctx->local_ip6[1]));
					__builtin_memcpy(&key.addr[2], &ctx->local_ip6[2], sizeof(ctx->local_ip6[2]));
					__builtin_memcpy(&key.addr[3], &ctx->local_ip6[3], sizeof(ctx->local_ip6[3]));
					// bpf_printk("cg_sock_ops, key, ip: %pI6, port: %u", &key.addr, __bpf_ntohs(key.port));
				}
				if (bpf_map_update_elem(&map_ports, &key, &cookie, BPF_ANY) < 0) {
					// bpf_printk("failed to update map_ports");
					return 1;
				}
			}
		}
		break;
	}

	// bpf_printk("sockops hook successful");

	return 0;
}

// This is triggered when the proxy queries the original destination information through getsockopt SO_ORIGINAL_DST.
// This program uses the source port of the client to retrieve the socket's cookie from map_ports,
// and then from map_socks to get the original destination information,
// then establishes a connection with the original target and forwards the client's request.
SEC("cgroup/getsockopt")
int cg_sock_opt(struct bpf_sockopt *ctx) {
	if (ctx->optname != SO_ORIGINAL_DST && ctx->optname != SO_MARK && ctx->optname != SO_COOKIE) {
		// bpf_printk("not SO_ORIGINAL_DST");
		return 1;
	}

	// Only forward IPv4 or IPv6 TCP connections
	if (ctx->sk->family != AF_INET && ctx->sk->family != AF_INET6) {
		// bpf_printk("not AF_INET or AF_INET6");
		return 1;
	}

	// only tcp
	if (ctx->sk->protocol != IPPROTO_TCP) {
		// bpf_printk("not IPPROTO_TCP");
		return 1;
	}

	// bpf_printk("cg_sock_opt, family: %u, optname: %u", ctx->sk->family, ctx->optname);

	// bpf_printk("SO_ORIGINAL_DST: %u", ctx->sk->family);

	// Get the clients source port
	// It's actually sk->dst_port because getsockopt() syscall with SO_ORIGINAL_DST socket option
	// is retrieving the original dst port of the client so it's "querying" the destination port of the client
	__u16 src_port = ctx->sk->dst_port;

	struct addr_port_key key = {0};
	key.port                 = src_port;

	// copy the destination ipv4 address to the key
	if (ctx->sk->family == AF_INET) {
		__builtin_memcpy(&key.addr[0], &ctx->sk->dst_ip4, sizeof(ctx->sk->dst_ip4));
		// bpf_printk("cg_sock_opt, key, ip: %pI4, port: %u", &key.addr[0], __bpf_ntohs(key.port));
	}

	// copy the destination ipv6 address to the key
	if (ctx->sk->family == AF_INET6) {
		bool is_ipv4_mapped = (key.addr[0] == 0x00000000 && key.addr[1] == 0x00000000 && key.addr[2] == 0xffff0000);
		if (is_ipv4_mapped) {
			// bpf_printk("cg_sock_opt, key, ip: %pI6, port: %u", &key.addr, __bpf_ntohs(key.port));
			// Set the IPv4 address in mapped format
			key.addr[0] = key.addr[3];
			key.addr[1] = 0x00000000;
			key.addr[2] = 0x00000000;
			key.addr[3] = 0x00000000;
		} else {
			__builtin_memcpy(key.addr, ctx->sk->dst_ip6, sizeof(ctx->sk->dst_ip6));
		}
	}

	// lookup the cookie
	__u64 *cookie;
	cookie = bpf_map_lookup_elem(&map_ports, &key);
	if (!cookie) {
		// bpf_printk("cg_sock_opt, failed to lookup cookie");
		return 1;
	}

	// Using the cookie (socket identifier), retrieve the original socket (client connect to destination) from map_socks
	struct rdr_socket *sock = bpf_map_lookup_elem(&map_socks, cookie);
	if (!sock) {
		// bpf_printk("cg_sock_opt, failed to lookup sock");
		return 1;
	}

	if (ctx->optname == SO_COOKIE) {
		// Check if we have enough space in the output buffer
		uint64_t *cookie_out = ctx->optval;
		if ((void *)(cookie_out + 1) > ctx->optval_end) {
			return 1;
		}

		// bpf_printk("cg_sock_opt, SO_COOKIE, cookie: %lu", *cookie);
		ctx->optlen = sizeof(uint64_t);
		*cookie_out = *cookie;
		ctx->retval = 0;

		return 1;
	} else if (ctx->optname == SO_MARK) {
		// bpf_printk("cg_sock_opt, SO_MARK");
		__u32 *so_tls_ok = ctx->optval;
		if ((void *)(so_tls_ok + 1) > ctx->optval_end) {
			// bpf_printk("invalid optval");
			return 1;
		}

		// lookup the pid from the address and port
		uint32_t *pid = bpf_map_lookup_elem(&addr_port_to_pid_map, &key);
		if (!pid) {
			return 1;
		}

		// see if tls is ok from the pid
		struct process_meta *meta = get_process_meta(*pid);
		if (!meta) {
			return 1;
		}

		// print the pid
		// bpf_printk("cg_sock_opt pid: %u", *pid);

		ctx->optlen = sizeof(__u32);
		*so_tls_ok  = (meta->tls_ok) ? 1 : 0; // set to 1 if TLS is OK, 0 otherwise
		ctx->retval = 0;

		return 1;
	} else if (ctx->optname == SO_ORIGINAL_DST) {
		// bpf_printk("cg_sock_opt, SO_ORIGINAL_DST");

		if (ctx->sk->family == AF_INET) {
			// bpf_printk("cg_sock_opt, AF_INET");

			struct sockaddr_in *sa = ctx->optval;
			if ((void *)(sa + 1) > ctx->optval_end) {
				// bpf_printk("invalid optval");
				return 1;
			}

			ctx->optlen    = sizeof(*sa);
			sa->sin_family = ctx->sk->family;

			bool is_ipv4_mapped = (sock->dst_addr[0] == 0x00000000 && sock->dst_addr[1] == 0x00000000 && sock->dst_addr[2] == 0xffff0000);
			if (is_ipv4_mapped) {
				// bpf_printk("cg_sock_opt, is_ipv4_mapped, ip: %pI4, port: %u", &sock->dst_addr[3], __bpf_ntohs(sock->dst_port));
				sa->sin_addr.s_addr = sock->dst_addr[3]; // Already in network byte order
			} else {
				// bpf_printk("cg_sock_opt, !is_ipv4_mapped, ip: %pI4, port: %u", &sock->dst_addr[0], __bpf_ntohs(sock->dst_port));
				sa->sin_addr.s_addr = sock->dst_addr[0]; // Already in network byte order
			}
			sa->sin_port = sock->dst_port; // Already in network byte order

			// bpf_printk("cg_sock_opt, ip: %pI4, port: %u", &sa->sin_addr.s_addr, __bpf_ntohs(sa->sin_port));
		} else if (ctx->sk->family == AF_INET6) {
			// bpf_printk("cg_sock_opt, AF_INET6");
			// bpf_printk("ctx->sk->family: %u", ctx->sk->family);
			struct sockaddr_in6 *sa6 = ctx->optval;
			if ((void *)(sa6 + 1) > ctx->optval_end) {
				// bpf_printk("invalid optval");
				return 1;
			}

			ctx->optlen      = sizeof(*sa6);
			sa6->sin6_family = ctx->sk->family;
			// Already in network byte order
			sa6->sin6_addr.in6_u.u6_addr32[0] = sock->dst_addr[0];
			sa6->sin6_addr.in6_u.u6_addr32[1] = sock->dst_addr[1];
			sa6->sin6_addr.in6_u.u6_addr32[2] = sock->dst_addr[2];
			sa6->sin6_addr.in6_u.u6_addr32[3] = sock->dst_addr[3];
			sa6->sin6_port                    = sock->dst_port; // Already in network byte order

			// bpf_printk("cg_sock_opt, ip: %pI6, port: %u", sa6->sin6_addr.in6_u.u6_addr32, __bpf_ntohs(sa6->sin6_port));
		} else {
			// bpf_printk("unsupported address family");
			return 1;
		}
	}

	ctx->retval = 0;

	// bpf_printk("redirecting connection to original destination");

	return 1;
}
