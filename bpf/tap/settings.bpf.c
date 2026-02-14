#include "settings.bpf.h"

// extract the capture direction from settings
static __always_inline bool get_ignore_loopback_setting() {
	// define the settings key
	enum SOCKET_SETTINGS key = SOCK_SETTING_IGNORE_LOOPBACK;

	// init setting value
	__u32 *setting_value;

	// try to fetch the entry
	setting_value = bpf_map_lookup_elem(&socket_settings_map, &key);

	// if it's empty, return the default
	if (setting_value == NULL) {
		// bpf_printk("socket: get_ignore_loopback_setting = NULL");
		return false;
	}

	// debug
	// bpf_printk("socket: get_ignore_loopback_setting = %d", *setting_value);

	// return the value
	return (bool)*setting_value != 0;
}

// extract the capture direction from settings
static __always_inline enum DIRECTION get_direction_setting() {
	// define the settings key
	enum SOCKET_SETTINGS key = SOCK_SETTING_DIRECTION;

	// init setting value
	__u32 *setting_value;

	// try to fetch the entry
	setting_value = bpf_map_lookup_elem(&socket_settings_map, &key);

	// if it's empty, return the default
	if (setting_value == NULL) {
		// bpf_printk("socket: get_direction_setting = NULL");
		return D_ALL;
	}

	// debug
	// bpf_printk("socket: get_direction_setting = %d", *setting_value);

	// return the value
	return (enum DIRECTION) * setting_value;
}

// extract the stream protocols setting (bitmask)
static __always_inline __u32 get_stream_protocols_setting() {
	// define the settings key
	enum SOCKET_SETTINGS key = SOCK_SETTING_STREAM_PROTOCOLS;

	// init setting value
	__u32 *stream_protocols;

	// try to fetch the entry
	stream_protocols = bpf_map_lookup_elem(&socket_settings_map, &key);

	// if it's empty, return the default (no protocols streamed)
	if (stream_protocols == NULL) {
		return 0;
	}

	// return the bitmask value
	return *stream_protocols;
}

// check if a protocol should be streamed based on settings
static __always_inline bool should_stream(enum PROTOCOL protocol) {
	// fetch the stream protocols bitmask
	__u32 stream_protocols = get_stream_protocols_setting();

	// check protocol against configured flags
	switch (protocol) {
	case P_HTTP1:
	case P_HTTP2:
		return SHOULD_STREAM_HTTP(stream_protocols);
	case P_REDIS:
		return SHOULD_STREAM_REDIS(stream_protocols);
	case P_MYSQL:
		return SHOULD_STREAM_MYSQL(stream_protocols);
	default:
		return false;
	}
}
