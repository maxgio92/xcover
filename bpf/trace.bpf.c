#include "vmlinux.h"

#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

/* Debug logging is compiled out by default to keep bpf_printk off the hot
 * path. Build with `make xcover/bpf BPF_DEBUG=1` to enable it. */
#ifdef XCOVER_DEBUG
#define xcover_debug(fmt, ...) bpf_printk(fmt, ##__VA_ARGS__)
#else
#define xcover_debug(fmt, ...) do {} while (0)
#endif

/* Function trace event */
struct event_t {
    __u64 cookie; /* Cookie is a function identifier */
};

/* Function trace event ring buffer */
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 28); /* 256MB buffer */
} events SEC(".maps");

/* Function trace report tracking map */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 40960); /* Maximum number of function symbols to track */
    __type(key, u64);           /* Function cookie */
    __type(value, u8);          /* Report marker */
} seen_funcs SEC(".maps");

long ringbuffer_flags = 0;

SEC("uprobe/handle_user_function")
int handle_user_function(struct pt_regs *ctx) {
	__u64 cookie = bpf_get_attach_cookie(ctx);
	u8 seen = 1;

	xcover_debug("handle user function with cookie %llu\n", cookie);

	/* Check if the function has been already reported */
	if (bpf_map_lookup_elem(&seen_funcs, &cookie)) {
		xcover_debug("function with cookie %llu already reported, skipping\n", cookie);

		return 0;
	}

	struct event_t *event = bpf_ringbuf_reserve(&events, sizeof(struct event_t), 0);
	if (!event) {
		xcover_debug("error submitting event to ring buffer for user function with cookie %llu\n", cookie);

		return 0;
	}

	/* Track which functions have been reported. Done only after a successful
	 * reserve, so a dropped event does not permanently hide the function. */
	if (bpf_map_update_elem(&seen_funcs, &cookie, &seen, BPF_ANY) < 0) {
		xcover_debug("error tracking user function with cookie %llu as seen\n", cookie);
	}

	event->cookie = cookie;
	bpf_ringbuf_submit(event, ringbuffer_flags);
	xcover_debug("submitted event to ring buffer for user function with cookie %llu\n", cookie);

	return 0;
}

char __license[] SEC("license") = "GPL";
