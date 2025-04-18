#include "vmlinux.h"

#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

struct event_t {
    __u64 cookie;
};

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 28); /* 256MB buffer */
} events SEC(".maps");

long ringbuffer_flags = 0;

SEC("uprobe/handle_user_function")
int handle_user_function(struct pt_regs *ctx) {
	__u64 cookie = bpf_get_attach_cookie(ctx);

	bpf_printk("handle user function with cookie %llu\n", cookie);

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
