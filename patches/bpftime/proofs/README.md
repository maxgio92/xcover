# Proofs for the bpftime upstream issues

Each bug reproduced at pinned commit `5bf24b21af85`. Environment: Fedora 44,
kernel `7.1.6-201.fc44.x86_64`, GCC 16.1.1, clang 22.1.8, bpftool submodule
`53c1852920c8` (libbpf `3f077472ee7e`).

Bug 1 (null `injected_pids`) is fixed on master and gets no issue, so it has
no proof here.

## Issue C: GCC const-qualifier hard errors

`issue-c-const-qualifier.log`. Reproduced by compiling the bundled libbpf with
the flags its own Makefile uses (`-Werror -Wall -std=gnu89`, line 38 of
`third_party/bpftool/libbpf/src/Makefile`):

```
cd third_party/bpftool/libbpf/src
gcc -g -O2 -Werror -Wall -std=gnu89 -I. -I../include -I../include/uapi -c libbpf.c
```

Three `-Werror=discarded-qualifiers` errors at `libbpf.c:8207`, `:11515`,
`:12103`. The default runtime build only warns because it does not pass
`-Werror`; the bpftool/CLI build does, which is why xcover's Makefile skips it.

## Issue D: conflicting `bpf_stream_vprintk` declaration

`issue-d-vprintk-conflict.log` and `issue-d-min.bpf.c`. Reproduced by compiling
a minimal BPF program that includes both this kernel's `vmlinux.h` and the
bundled `bpf_helpers.h`:

```
bpftool btf dump file /sys/kernel/btf/vmlinux format c > vmlinux.h   # 4-param decl
clang -g -O2 -target bpf -I<bundled bpf_helpers dir> -c issue-d-min.bpf.c
```

`error: conflicting types for 'bpf_stream_vprintk'`: bundled header declares 5
params, kernel BTF declares 4.

## Issues A and B: handler-manager OOB and perf-event slot leak

`proof_runtime.cpp` drives the public shm API (the same calls the syscall
server makes on attach/detach), so no Frida or target process is needed.
Link it against the built `libruntime.a` and run with a small pool:

```
BPFTIME_MAX_FD_COUNT=128 ./proof_runtime
```

`issue-ab-unpatched.log` (pinned commit, bugs present):

- **B**: after `bpftime_close(link_fd)`, `is_perf_event_fd(perf_fd)` is still 1.
  The perf event slot leaked; one slot per attach/detach cycle.
- **A**: the leak drains the pool. At cycle 41, `open_fake_fd` returns fd 129
  against a 128-slot `handlers[]`, and the pinned code uses that value as an
  unchecked index (`handlers[129]`, out-of-bounds write into shared memory).

`issue-ab-patched.log` (patches 0002 + 0003 applied, same binary rebuilt):

- **B**: after close, `is_perf_event_fd` is 0. Slot freed.
- **A**: exhaustion returns a clean `-ENOSPC` (errno 28) instead of the OOB
  write. No corruption, no crash.

The `[info] Created uprobe/uretprobe perf event handler` lines interleaved in
the raw run are bpftime's own logging; the logs here are filtered to the `[A]`
and `[B]` markers.
