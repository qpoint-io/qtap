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

// max size of strings within trace messages
#define MAX_TRACE_MSG_SIZE 256

// enum to represent the different components that can be traced
enum QTAP_COMPONENT {
	QTAP_CA,
	QTAP_DEBUG,
	QTAP_GOTLS,
	QTAP_JAVASSL,
	QTAP_NODETLS,
	QTAP_OPENSSL,
	QTAP_PROCESS,
	QTAP_PROTOCOL,
	QTAP_REDIRECTOR,
	QTAP_RUSTLS,
	QTAP_SOCKET,
};

enum TRACE_EVENT {
	TRACE_MSG  = 1ULL,
	TRACE_ATTR = 2ULL,
	TRACE_END  = 3ULL,
};

enum TRACE_ATTR_TYPE {
	TRACE_STRING  = 1ULL, // For %s
	TRACE_INT     = 2ULL, // For %i and %d
	TRACE_UINT    = 3ULL, // For %u and %llu
	TRACE_POINTER = 4ULL, // For %p
	TRACE_BOOL    = 5ULL, // For %d
	TRACE_IP4     = 6ULL, // For %d
	TRACE_IP6     = 7ULL, // For %d
};

struct trace_msg_event {
	uint64_t type; // TRACE_EVENT
	uint64_t tsid; // timestamp id
	// total size of the message
	uint32_t msg_size;
	// the message
	char msg[MAX_TRACE_MSG_SIZE];
};

struct trace_attr_event {
	uint64_t type; // TRACE_EVENT_ATTR
	uint64_t tsid; // timestamp id
	uint64_t attr_type; // TRACE_ATTR_TYPE
	uint32_t title_size;
	char title[MAX_TRACE_MSG_SIZE];
	union {
		int64_t int_value; // For T_INT
		uint64_t uint_value; // For T_UINT (covers both %u and %llu)
		void *ptr_value; // For T_POINTER
		uint32_t str_size; // For T_STRING
		bool bool_value; // For T_BOOL
		__u32 ip4_value; // For T_IP4
		__u32 ip6_value[4]; // For T_IP6
	} value;
	// string_data will be appended here for T_STRING
	// and the 'value' field will be the size of the string
	char string_data[MAX_TRACE_MSG_SIZE];
};

struct trace_end_event {
	uint64_t type; // TRACE_EVENT
	uint64_t tsid; // timestamp id
};

// ring buffer for trace messages
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 256 * 1024); // 256 KB buffer
} trace_events SEC(".maps");

// trace toggle map (activated by qtap)
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 128);
	__type(key, __u32);
	__type(value, bool);
} trace_toggle_map SEC(".maps");

// check if tracing is enabled for a component
static __always_inline bool trace_enabled(__u32 component) {
	// see if the component is set and enabled
	bool *enabled = bpf_map_lookup_elem(&trace_toggle_map, &component);
	if (!enabled) {
		return false;
	}
	return *enabled;
}

// check if tracing is enabled for a component and a specific pid
static __always_inline bool trace_enabled_for_pid(__u32 component, __u32 pid) {
	// ignore the first 100 pids
	if (pid < 100) {
		return false;
	}

	// first see if the component is enabled
	bool component_enabled = trace_enabled(component);
	if (!component_enabled) {
		return false;
	}

	// then see if the pid is enabled
	bool *enabled = bpf_map_lookup_elem(&trace_toggle_map, &pid);
	if (!enabled) {
		return false;
	}
	return *enabled;
}

static __always_inline void trace_msg(uint64_t tsid, const char *msg) {
	struct trace_msg_event *msg_event;
	msg_event = bpf_ringbuf_reserve(&trace_events, sizeof(*msg_event), 0);
	if (!msg_event)
		return;

	msg_event->type = TRACE_MSG;
	msg_event->tsid = tsid;

	// determine the length of the message
	size_t msg_len = __builtin_strlen(msg);
	if (msg_len >= MAX_TRACE_MSG_SIZE) {
		msg_len = MAX_TRACE_MSG_SIZE - 1;
	}
	msg_event->msg_size = msg_len + 1;

	// copy the message
	__builtin_memcpy(msg_event->msg, msg, msg_len);
	msg_event->msg[msg_len] = '\0';

	// submit the event
	bpf_ringbuf_submit(msg_event, 0);
}

static __always_inline void trace_end(uint64_t tsid) {
	struct trace_end_event *end_event;
	end_event = bpf_ringbuf_reserve(&trace_events, sizeof(*end_event), 0);
	if (!end_event)
		return;

	end_event->type = TRACE_END;
	end_event->tsid = tsid;

	bpf_ringbuf_submit(end_event, 0);
}

static __always_inline void set_title(struct trace_attr_event *attr_event, const char *title) {
	// determine the length of the title
	size_t title_len = __builtin_strlen(title);
	if (title_len >= MAX_TRACE_MSG_SIZE) {
		title_len = MAX_TRACE_MSG_SIZE - 1;
	}
	attr_event->title_size = title_len + 1;

	// copy the title
	__builtin_memcpy(attr_event->title, title, title_len);
	attr_event->title[title_len] = '\0';
}

static __always_inline void trace_attr_int(uint64_t tsid, const char *title, int64_t value) {
	struct trace_attr_event *attr_event;
	attr_event = bpf_ringbuf_reserve(&trace_events, sizeof(*attr_event), 0);
	if (!attr_event)
		return;

	attr_event->type            = TRACE_ATTR;
	attr_event->tsid            = tsid;
	attr_event->attr_type       = TRACE_INT;
	attr_event->value.int_value = value;

	set_title(attr_event, title);

	bpf_ringbuf_submit(attr_event, 0);
}

static __always_inline void trace_attr_uint(uint64_t tsid, const char *title, uint64_t value) {
	struct trace_attr_event *attr_event;
	attr_event = bpf_ringbuf_reserve(&trace_events, sizeof(*attr_event), 0);
	if (!attr_event)
		return;

	attr_event->type             = TRACE_ATTR;
	attr_event->tsid             = tsid;
	attr_event->attr_type        = TRACE_UINT;
	attr_event->value.uint_value = value;

	set_title(attr_event, title);

	bpf_ringbuf_submit(attr_event, 0);
}

static __always_inline void trace_attr_string(uint64_t tsid, const char *title, const char *value) {
	struct trace_attr_event *attr_event;
	attr_event = bpf_ringbuf_reserve(&trace_events, sizeof(*attr_event), 0);
	if (!attr_event)
		return;

	attr_event->type      = TRACE_ATTR;
	attr_event->tsid      = tsid;
	attr_event->attr_type = TRACE_STRING;

	set_title(attr_event, title);

	// read the string value
	int value_read_result = bpf_probe_read_str(attr_event->string_data, MAX_TRACE_MSG_SIZE, value);

	if (value_read_result <= 0) {
		// discard the event
		bpf_ringbuf_discard(attr_event, 0);

		// nothing more to do
		return;
	}

	// set the string value size
	attr_event->value.str_size = value_read_result;

	bpf_ringbuf_submit(attr_event, 0);
}

static __always_inline void trace_attr_pointer(uint64_t tsid, const char *title, void *value) {
	struct trace_attr_event *attr_event;
	attr_event = bpf_ringbuf_reserve(&trace_events, sizeof(*attr_event), 0);
	if (!attr_event)
		return;

	attr_event->type            = TRACE_ATTR;
	attr_event->tsid            = tsid;
	attr_event->attr_type       = TRACE_POINTER;
	attr_event->value.ptr_value = value;

	set_title(attr_event, title);

	bpf_ringbuf_submit(attr_event, 0);
}

static __always_inline void trace_attr_bool(uint64_t tsid, const char *title, bool value) {
	struct trace_attr_event *attr_event;
	attr_event = bpf_ringbuf_reserve(&trace_events, sizeof(*attr_event), 0);
	if (!attr_event)
		return;

	attr_event->type             = TRACE_ATTR;
	attr_event->tsid             = tsid;
	attr_event->attr_type        = TRACE_BOOL;
	attr_event->value.bool_value = value;

	set_title(attr_event, title);

	bpf_ringbuf_submit(attr_event, 0);
}

static __always_inline void trace_attr_ip4(uint64_t tsid, const char *title, uint8_t addr[16]) {
	struct trace_attr_event *attr_event;
	attr_event = bpf_ringbuf_reserve(&trace_events, sizeof(*attr_event), 0);
	if (!attr_event)
		return;

	attr_event->type      = TRACE_ATTR;
	attr_event->tsid      = tsid;
	attr_event->attr_type = TRACE_IP4;

	set_title(attr_event, title);

	// copy only the first 4 bytes of addr
	__builtin_memcpy(&attr_event->value.ip4_value, addr, sizeof(__u32));

	bpf_ringbuf_submit(attr_event, 0);
}

static __always_inline void trace_attr_ip6(uint64_t tsid, const char *title, uint8_t addr[16]) {
	struct trace_attr_event *attr_event;
	attr_event = bpf_ringbuf_reserve(&trace_events, sizeof(*attr_event), 0);
	if (!attr_event)
		return;

	attr_event->type      = TRACE_ATTR;
	attr_event->tsid      = tsid;
	attr_event->attr_type = TRACE_IP6;

	set_title(attr_event, title);

	// copy the ip6 value
	__builtin_memcpy(attr_event->value.ip6_value, addr, sizeof(attr_event->value.ip6_value));

	bpf_ringbuf_submit(attr_event, 0);
}

static __always_inline void trace_port(uint64_t tsid, const char *title, uint16_t port) {
	// reverse the port and convert to int
	uint32_t reversed_port = bpf_ntohs(port);
	trace_attr_uint(tsid, title, reversed_port);
}

#define TRACE_HELPER(tsid, ...) \
	do { \
		__VA_ARGS__; \
		trace_end(tsid); \
	} while (0)

#define TRACE(msg, ...) \
	do { \
		uint64_t __tsid = bpf_ktime_get_ns(); \
		trace_msg(__tsid, msg); \
		TRACE_HELPER(__tsid, __VA_ARGS__); \
	} while (0)

#define TRACE_STRING(title, value)  trace_attr_string(__tsid, title, value)
#define TRACE_INT(title, value)     trace_attr_int(__tsid, title, value)
#define TRACE_UINT(title, value)    trace_attr_uint(__tsid, title, value)
#define TRACE_BOOL(title, value)    trace_attr_bool(__tsid, title, value)
#define TRACE_POINTER(title, value) trace_attr_pointer(__tsid, title, value)
#define TRACE_IP4(title, value)     trace_attr_ip4(__tsid, title, value)
#define TRACE_IP6(title, value)     trace_attr_ip6(__tsid, title, value)
#define TRACE_PORT(title, value)    trace_port(__tsid, title, value)
#ifdef QTAP_TRACE_DISABLE
// Empty definitions when tracing is not enabled
#define TRACE_IF_ENABLED(component, pid, ...)
#define TRACE_CA(pid, ...)
#define TRACE_DEBUG(pid, ...)
#define TRACE_GOTLS(pid, ...)
#define TRACE_JAVASSL(pid, ...)
#define TRACE_NODETLS(pid, ...)
#define TRACE_OPENSSL(pid, ...)
#define TRACE_PROCESS(pid, ...)
#define TRACE_PROTOCOL(pid, ...)
#define TRACE_REDIRECTOR(pid, ...)
#define TRACE_SOCKET(pid, ...)
#else

#define TRACE_IF_ENABLED(component, pid, ...) \
	if (trace_enabled_for_pid(component, pid)) { \
		TRACE(__VA_ARGS__); \
	}

#define TRACE_CA(pid, ...)         TRACE_IF_ENABLED(QTAP_CA, pid, __VA_ARGS__)
#define TRACE_DEBUG(pid, ...)      TRACE_IF_ENABLED(QTAP_DEBUG, pid, __VA_ARGS__)
#define TRACE_GOTLS(pid, ...)      TRACE_IF_ENABLED(QTAP_GOTLS, pid, __VA_ARGS__)
#define TRACE_JAVASSL(pid, ...)    TRACE_IF_ENABLED(QTAP_JAVASSL, pid, __VA_ARGS__)
#define TRACE_NODETLS(pid, ...)    TRACE_IF_ENABLED(QTAP_NODETLS, pid, __VA_ARGS__)
#define TRACE_OPENSSL(pid, ...)    TRACE_IF_ENABLED(QTAP_OPENSSL, pid, __VA_ARGS__)
#define TRACE_PROCESS(pid, ...)    TRACE_IF_ENABLED(QTAP_PROCESS, pid, __VA_ARGS__)
#define TRACE_PROTOCOL(pid, ...)   TRACE_IF_ENABLED(QTAP_PROTOCOL, pid, __VA_ARGS__)
#define TRACE_REDIRECTOR(pid, ...) TRACE_IF_ENABLED(QTAP_REDIRECTOR, pid, __VA_ARGS__)
#define TRACE_RUSTLS(pid, ...)     TRACE_IF_ENABLED(QTAP_RUSTLS, pid, __VA_ARGS__)
#define TRACE_SOCKET(pid, ...)     TRACE_IF_ENABLED(QTAP_SOCKET, pid, __VA_ARGS__)
#endif
