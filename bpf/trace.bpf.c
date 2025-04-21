#include "vmlinux.h"

#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

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

	bpf_printk("handle user function with cookie %llu\n", cookie);

	/* Check if the function has been already reported */
	if (bpf_map_lookup_elem(&seen_funcs, &cookie)) {
		bpf_printk("function with cookie %llu already reported, skipping\n", cookie);

		return 0;
	}

	/* Track which functions have been reported */
	bpf_map_update_elem(&seen_funcs, &cookie, &seen, BPF_ANY);

	struct event_t *event = bpf_ringbuf_reserve(&events, sizeof(struct event_t), 0);
	if (!event) {
		bpf_printk("error submitting event to ring buffer for user function with cookie %s\n", cookie);

		return 0;
	}

	event->cookie = cookie;
	bpf_ringbuf_submit(event, ringbuffer_flags);
	bpf_printk("submitted event to ring buffer for user function with cookie %s\n", cookie);

	return 0;
}

char __license[] SEC("license") = "GPL";
