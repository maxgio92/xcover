#include "vmlinux.h"

#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>


/* Max length of function name string */
#define FUNC_NAME_LEN 64

struct func_name_t {
    char name[FUNC_NAME_LEN];
};

/* IP to function name map */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 16384);
    __type(key, u64); /* instruction pointer */
    __type(value, struct func_name_t);
} ip_to_func_name_map SEC(".maps");

/* List of function being execution back to userspace */
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 24); /* ~16MB buffer */
} events SEC(".maps");

SEC("uprobe/handle_user_function")
int handle_user_function(struct pt_regs *ctx) {
	u64 ip = ctx->ip;
	struct func_name_t *fn = bpf_map_lookup_elem(&ip_to_func_name_map, &ip);
	if (fn) {
	    bpf_printk("handle user function %s\n", fn->name);
	} else {
	    bpf_printk("handle user function unknown\n");
	    return 0;
	}

	struct func_name_t *event = bpf_ringbuf_reserve(&events, sizeof(struct func_name_t), 0);
	if (!event) {
		bpf_printk("error submitting event to ring buffer for user function %s\n", fn->name);
		return 0;
	}

	__builtin_memcpy(event->name, fn->name, FUNC_NAME_LEN);
	bpf_ringbuf_submit(event, 0);
	bpf_printk("submitted event to ring buffer for user function %s\n", event->name);

	return 0;
}

char __license[] SEC("license") = "GPL";
