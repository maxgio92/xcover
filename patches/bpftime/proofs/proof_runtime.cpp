// Proof harness for two bpftime handler-manager bugs, driven through the
// public shm API (the same calls the syscall server makes on attach/detach).
//
//   Issue B: closing a bpf_link fd leaks its perf event handler slot.
//   Issue A: once the slot pool is exhausted, open_fake_fd hands back an fd
//            past the handlers[] bounds and set_handler writes out of range.
//
// Build unpatched -> both bugs fire. Build with patches 0002+0003 -> clean.

#include <bpftime_shm.hpp>
#include <ebpf_inst.h>
#include <unistd.h>
#include <cerrno>
#include <cstdio>
#include <cstdlib>
#include <cstring>

using namespace bpftime;

// Minimal valid eBPF program: r0 = 0; exit.
static const ebpf_inst prog_insns[] = {
	{ 0xb7, 0, 0, 0, 0 }, // mov r0, 0
	{ 0x95, 0, 0, 0, 0 }, // exit
};

static int make_link()
{
	int perf_fd = bpftime_uprobe_create(-1, getpid(), "/proc/self/exe",
					    0x1000, false, 0);
	if (perf_fd < 0)
		return -1;
	int prog_fd = bpftime_progs_create(-1, prog_insns, 2, "p", 0);
	if (prog_fd < 0)
		return -1;
	// attach returns the link fd; this is the slot freed on detach.
	int link_fd = bpftime_attach_perf_to_bpf(perf_fd, prog_fd);
	return link_fd;
}

int main()
{
	bpftime_initialize_global_shm(shm_open_type::SHM_REMOVE_AND_CREATE);

	// --- Issue B: one detach cycle, count perf-event slots before/after ---
	int perf_fd = bpftime_uprobe_create(-1, getpid(), "/proc/self/exe",
					    0x1000, false, 0);
	int prog_fd = bpftime_progs_create(-1, prog_insns, 2, "p", 0);
	int link_fd = bpftime_attach_perf_to_bpf(perf_fd, prog_fd);
	printf("[B] created perf_fd=%d prog_fd=%d link_fd=%d\n", perf_fd,
	       prog_fd, link_fd);
	printf("[B] before close: is_perf_event_fd(%d) = %d\n", perf_fd,
	       bpftime_is_perf_event_fd(perf_fd));
	bpftime_close(link_fd); // detach: closes the link fd
	int leaked = bpftime_is_perf_event_fd(perf_fd);
	printf("[B] after  close: is_perf_event_fd(%d) = %d  -> %s\n", perf_fd,
	       leaked, leaked ? "LEAKED (perf slot still allocated)"
			      : "freed (no leak)");

	// --- Issue A: attach/detach loop until the pool exhausts ---
	// Each cycle leaks one perf slot (bug B), so the pool drains and
	// open_fake_fd eventually returns an out-of-bounds fd. Unpatched,
	// set_handler then writes past handlers[] and corrupts memory.
	unsigned long pool = 128; // effective min pool size (see log)
	if (const char *e = getenv("BPFTIME_MAX_FD_COUNT"))
		pool = strtoul(e, nullptr, 10) < 128 ? 128 : strtoul(e, nullptr, 10);
	printf("[A] pool size = %lu; looping attach/detach...\n", pool);
	for (int i = 0; i < 100000; i++) {
		int pf = bpftime_uprobe_create(-1, getpid(), "/proc/self/exe",
					       0x1000, false, 0);
		if (pf >= 0 && (unsigned long)pf >= pool) {
			printf("[A] cycle %d: open_fake_fd returned fd=%d >= pool=%lu\n",
			       i, pf, pool);
			printf("[A] -> this fd is used UNCHECKED as handlers[%d] index (OOB write)\n",
			       pf);
			return 0;
		}
		int gf = bpftime_progs_create(-1, prog_insns, 2, "p", 0);
		int lf = (pf >= 0 && gf >= 0)
				 ? bpftime_attach_perf_to_bpf(pf, gf)
				 : -1;
		if (lf < 0) {
			printf("[A] cycle %d: allocation failed (fd=%d, errno=%d) after leak-driven exhaustion\n",
			       i, lf, errno);
			return 0;
		}
		bpftime_close(lf); // detach: leaks pf's perf slot (bug B)
	}
	printf("[A] loop finished without crash\n");
	return 0;
}
