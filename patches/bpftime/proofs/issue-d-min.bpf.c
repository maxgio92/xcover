#include "vmlinux.h"
#include <bpf/bpf_helpers.h>

SEC("uprobe")
int probe(void *ctx)
{
	return 0;
}

char LICENSE[] SEC("license") = "GPL";
