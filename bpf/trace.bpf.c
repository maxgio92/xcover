#include "vmlinux.h"

#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

/* Trace-pipe logging costs a few hundred ns per call, so it is compiled in
 * only with -DDEBUG (make xcover/bpf CFLAGS=-DDEBUG). */
#ifdef DEBUG
#define dbg_printk(fmt, ...) bpf_printk(fmt, ##__VA_ARGS__)
#else
#define dbg_printk(fmt, ...) do {} while (0)
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

/* Calls not recorded because seen_funcs was full, one per call. Userspace
 * reads it on exit to warn that the report undercounts. */
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, u32);
    __type(value, u64);
} drops SEC(".maps");

long ringbuffer_flags = 0;

SEC("uprobe/handle_user_function")
int handle_user_function(struct pt_regs *ctx) {
	__u64 cookie = bpf_get_attach_cookie(ctx);
	u8 seen = 1;

	dbg_printk("handle user function with cookie %llu\n", cookie);

	/* Check if the function has been already reported */
	if (bpf_map_lookup_elem(&seen_funcs, &cookie)) {
		dbg_printk("function with cookie %llu already reported, skipping\n", cookie);

		return 0;
	}

	/* Track which functions have been reported. When the map is full the
	 * function cannot be deduplicated, so count the drop instead of flooding
	 * the ring buffer with one event per call. */
	if (bpf_map_update_elem(&seen_funcs, &cookie, &seen, BPF_ANY) < 0) {
		u32 zero = 0;
		u64 *dropped = bpf_map_lookup_elem(&drops, &zero);
		if (dropped)
			__sync_fetch_and_add(dropped, 1);
		dbg_printk("seen_funcs full, dropping first hit for cookie %llu\n", cookie);

		return 0;
	}

	struct event_t *event = bpf_ringbuf_reserve(&events, sizeof(struct event_t), 0);
	if (!event) {
		dbg_printk("error submitting event to ring buffer for user function with cookie %llu\n", cookie);

		return 0;
	}

	event->cookie = cookie;
	bpf_ringbuf_submit(event, ringbuffer_flags);
	dbg_printk("submitted event to ring buffer for user function with cookie %llu\n", cookie);

	return 0;
}

char __license[] SEC("license") = "GPL";
