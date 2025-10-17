#include "vmlinux.h"
#include "bpf_helpers.h"
#include "bpf_tracing.h"
#include "common.bpf.h"
#include "sock_pid_fd.bpf.h"
#include "socket.bpf.h"

// this map is used to store the socket pointer from the entry args to be available in the ret probe
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 16384);
	__type(key, uint64_t); // pid_tgid
	__type(value, uintptr_t); // the socket pointer
} active_sock_alloc_file_args SEC(".maps");

// this map is used to find the socket when the fd is created
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 16384);
	__type(key, uintptr_t); // the file pointer
	__type(value, uintptr_t); // the socket pointer
} active_file_to_sock_map SEC(".maps");

// this map is used to find the pid/fd composite key when a file is closed (needed for cleanup)
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 16384);
	__type(key, uintptr_t); // the file pointer
	__type(value, struct pid_fd_key); // the pid/fd composite key
} active_file_to_pid_fd_map SEC(".maps");

// save a reference to the tcp_v6_source_addr by socket cookie
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 16384);
	__type(key, __u64); // socket cookie
	__type(value, struct addr_port_key); // the source address and port
} active_tcp_source_addr_map SEC(".maps");

// Step 1: get the socket pointer
//
// function: sock_alloc_file(struct socket *sock) -> struct file *file
//
// receives the socket pointer as the first argument, and returns the file pointer
//
// Step 2: get the file descriptor
//
// function: fd_install(int fd, struct file *file) -> void
//
// receives the file descriptor as the first argument, and the file pointer as the second argument
//
// Step 3: store the socket pointer with a pid/fd composite key

SEC("kprobe/sock_alloc_file")
int BPF_KPROBE(track_sock_alloc_file_entry) {
	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// get the socket
	uintptr_t sock = (uintptr_t)PT_REGS_PARM1(ctx);

	// store the socket pointer
	bpf_map_update_elem(&active_sock_alloc_file_args, &pid_tgid, &sock, BPF_ANY);

	return 0;
}

SEC("kretprobe/sock_alloc_file")
int BPF_KRETPROBE(track_sock_alloc_file_ret) {
	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// get the socket pointer
	uintptr_t *sock = bpf_map_lookup_elem(&active_sock_alloc_file_args, &pid_tgid);
	if (!sock) {
		return 0;
	}

	// get the file descriptor
	uintptr_t file = (uintptr_t)PT_REGS_RC(ctx);

	// store the file pointer
	bpf_map_update_elem(&active_file_to_sock_map, &file, sock, BPF_ANY);

	// delete the socket pointer
	bpf_map_delete_elem(&active_sock_alloc_file_args, &pid_tgid);

	return 0;
}

SEC("kprobe/fd_install")
int BPF_KPROBE(track_fd_install_entry) {
	// get the file
	uintptr_t file = (uintptr_t)PT_REGS_PARM2(ctx);

	// get the socket
	uintptr_t *sock = bpf_map_lookup_elem(&active_file_to_sock_map, &file);
	if (!sock) {
		return 0;
	}

	// get the fd
	int fd = PT_REGS_PARM1(ctx);

	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();

	// get the pid
	pid_t pid = pid_tgid >> 32;

	// create a pid_fd_key
	struct pid_fd_key key = {
		.pid = pid,
		.fd  = fd,
	};

	// store the socket pointer
	bpf_map_update_elem(&pid_fd_to_sock_map, &key, sock, BPF_ANY);

	// store the pid/fd composite key for cleanup when the file is closed
	bpf_map_update_elem(&active_file_to_pid_fd_map, &file, &key, BPF_ANY);

	// remove the file pointer
	bpf_map_delete_elem(&active_file_to_sock_map, &file);

	return 0;
}

SEC("kprobe/__fput")
int BPF_KPROBE(cleanup_pid_fd_file_entries) {
	// get the file pointer
	uintptr_t file = (uintptr_t)PT_REGS_PARM1(ctx);

	// get the pid/fd composite key
	struct pid_fd_key *key = bpf_map_lookup_elem(&active_file_to_pid_fd_map, &file);
	if (!key) {
		return 0;
	}

	// see if the socket layer still has the open socket
	struct conn_info *conn_info = bpf_map_lookup_elem(&conn_info_map, key);
	if (conn_info != NULL) {
		// initialize a socket context
		struct socket_ctx sock_ctx = {};
		sock_ctx.id                = key;
		sock_ctx.pid_tgid          = bpf_get_current_pid_tgid();
		sock_ctx.trace_mod         = QTAP_SOCKET;
		bpf_probe_read_str(sock_ctx.trace_id, sizeof(sock_ctx.trace_id), "kprobe/__fput");

		// process the close (likely the syscall didn't happen)
		process_close(&sock_ctx);
	}

	// delete the pid/fd composite key
	bpf_map_delete_elem(&active_file_to_pid_fd_map, &file);

	// delete the socket pointer
	bpf_map_delete_elem(&pid_fd_to_sock_map, key);

	return 0;
}

SEC("fexit/tcp_v4_connect")
int BPF_PROG(trace_tcp_v4_connect_fexit, struct sock *sk, int ret) {
	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid      = pid_tgid >> 32;

	// retrieve socket information
	__be32 saddr = 0;
	__u16 sport  = 0;

	// Read source port and address safely
	bpf_probe_read(&sport, sizeof(sport), &sk->__sk_common.skc_num);
	bpf_probe_read(&saddr, sizeof(saddr), &sk->__sk_common.skc_rcv_saddr);

	// Get the socket cookie
	__u64 cookie = bpf_get_socket_cookie(sk);

	// create a addr_port_key
	struct addr_port_key key = {0};
	key.port                 = __bpf_htons(sport);

	// copy the source IPv4 address to the key
	__builtin_memcpy(key.addr, &saddr, sizeof(saddr));

	// store the pid
	bpf_map_update_elem(&addr_port_to_pid_map, &key, &pid, BPF_ANY);

	// store the source address and port by socket cookie
	bpf_map_update_elem(&active_tcp_source_addr_map, &cookie, &key, BPF_ANY);

	// Log the connection details
	// bpf_printk("TCP v4 connect fexit: saddr=%pI4 sport=%d, pid: %d, sock: %p, cookie: %llu", &saddr, sport, pid, sk, cookie);

	return 0;
}

SEC("fexit/tcp_v6_connect")
int BPF_PROG(trace_tcp_v6_connect_fexit, struct sock *sk, int ret) {
	// get the pid_tgid
	uint64_t pid_tgid = bpf_get_current_pid_tgid();
	uint32_t pid      = pid_tgid >> 32;

	// read saddr6 and sport directly from sock structure
	struct in6_addr saddr6;
	__u16 sport;

	// Read source port and address safely
	bpf_probe_read(&sport, sizeof(sport), &sk->__sk_common.skc_num);
	bpf_probe_read(&saddr6, sizeof(saddr6), &sk->__sk_common.skc_v6_rcv_saddr);

	// Get the socket cookie
	__u64 cookie = bpf_get_socket_cookie(sk);

	// create a addr_port_key
	struct addr_port_key key = {0};
	key.port                 = __bpf_htons(sport);

	// copy the source IPv6 address to the key
	__builtin_memcpy(key.addr, &saddr6, sizeof(saddr6));

	bool is_ipv4_mapped = (key.addr[0] == 0x00000000 && key.addr[1] == 0x00000000 && key.addr[2] == 0xffff0000);
	if (is_ipv4_mapped) {
		// Set the IPv4 address in mapped format
		key.addr[0] = key.addr[3];
		key.addr[1] = 0x00000000;
		key.addr[2] = 0x00000000;
		key.addr[3] = 0x00000000;
	}

	// store the pid
	bpf_map_update_elem(&addr_port_to_pid_map, &key, &pid, BPF_ANY);

	// store the source address and port by socket cookie
	bpf_map_update_elem(&active_tcp_source_addr_map, &cookie, &key, BPF_ANY);

	return 0;
}

SEC("fexit/tcp_recvmsg")
int BPF_PROG(trace_tcp_recvmsg_fexit, struct sock *sk, int ret) {
	if (!sk)
		return 0;

	// force the kernel to generate a cookie for the socket
	bpf_get_socket_cookie(sk);

	return 0;
}

SEC("kprobe/tcp_close")
int BPF_KPROBE(trace_tcp_close) {
	struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);

	// // check if it's an IPv4 or IPv6 socket
	__u16 family;
	bpf_probe_read(&family, sizeof(family), &sk->__sk_common.skc_family);

	// initialize the key
	struct addr_port_key key = {0};

	// extract socket cookie
	__u64 cookie;
	bpf_probe_read(&cookie, sizeof(cookie), &sk->__sk_common.skc_cookie);

	struct addr_port_key *savedKey;
	savedKey = bpf_map_lookup_elem(&active_tcp_source_addr_map, &cookie);

	if (savedKey == NULL) {
		// bpf_printk("failed to lookup saved key, cookie: %llu", cookie);
		// return 0;
	} else {
		// bpf_printk("found saved key, cookie: %llu, raw_port: %u", cookie, savedKey->port);
		key.port = savedKey->port;
		__builtin_memcpy(key.addr, savedKey->addr, sizeof(key.addr));
	}

	// remove the entry from addr_port_to_pid_map
	if (bpf_map_delete_elem(&addr_port_to_pid_map, &key) < 0) {
		// bpf_printk("failed to delete from addr_port_to_pid_map, key: %pI4, port: %u", &key.addr, __bpf_ntohs(key.port));
	}

	// delete entry from active_tcp_source_addr_map
	if (bpf_map_delete_elem(&active_tcp_source_addr_map, &cookie) < 0) {
		// bpf_printk("failed to delete from active_tcp_source_addr_map, cookie: %llu", cookie);
	}

	return 0;
}
