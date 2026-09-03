# Plan: upstream GitHub issues for the bpftime patches

Target repo: <https://github.com/eunomia-bpf/bpftime>
Baseline: all bugs reproduce at pinned commit `5bf24b21af85` (2026-05-25).
Verified against master `0834957169da` (2026-08-28):

- Bug 1 (null `injected_pids`): **fixed upstream**. Master null-checks all
  three alive-agent-set functions (with locking) and guards `__destruct_shm`
  with `global_shm_initialized`. Do not file an issue.
- Bug 2 (handler OOB, fake-fd leak): still present. `get_handler`,
  `operator[]`, and `set_handler` index `handlers[]` unchecked;
  `open_fake_fd` has no size check.
- Bug 3 (link close leaks perf event slot): still present. `clear_id_at`
  has no `bpf_link_handler` branch. Master did add a bounds check on
  `clear_id_at`'s own fd, so the problem is partially acknowledged there.
- Bugs 4 and 5 (bundled libbpf build errors): still present. Master pins
  bpftool `53c1852920c8`, whose libbpf submodule `3f077472ee7e` has the
  non-const declarations (`libbpf.c` lines 8205, 11513, 12098) and the
  5-parameter `bpf_stream_vprintk` declaration (`bpf_helpers.h` line 318).

All four remaining bugs have reproduced artifacts under `proofs/` (build
errors for C and D, an in-process attach/detach harness for A and B, plus a
patched-build counterfactual). See `proofs/README.md`. Attach the relevant
log when filing each issue.

File four issues, one per remaining patch. File the two runtime bugs first
(issues 2 and 3): they form one story (the leak that exhausts the pool, and
the corruption that exhaustion causes) and should cross-reference each
other. The two toolchain issues (4 and 5) are independent.

## Issue 1: tracee crashes on exit with null `injected_pids` (LD_PRELOAD agent)

**Do not file: fixed on master.** Master `0834957169da` null-checks
`add_pid_into_alive_agent_set`, `remove_pid_from_alive_agent_set`, and
`iterate_all_pids_in_alive_agent_set`, and bails out of `__destruct_shm`
when `global_shm_initialized` is false. That covers everything patch
`0001-fix-null-injected_pids-crash-on-tracee-exit.patch` does and more
(the upstream version adds locking). Keep the patch only for the pinned
commit; it drops out when `BPFTIME_COMMIT` is bumped past master.

## Issue 2: out-of-bounds handler access and fd leak when the handler pool is exhausted

- Patch: `0002-fix-handler-manager-oob-and-open-fake-fd-leak.patch`
- Suggested title: `handler_manager indexes handlers[] with unchecked OS fd values: OOB write and memory corruption on pool exhaustion`

### Draft body

```markdown
## Environment

- bpftime commit: `5bf24b21af85`, still present on master at `0834957169da` (2026-08-28)
- OS: Fedora 44, kernel `7.1.6-201.fc44.x86_64`

## What happens

`handler_manager` uses OS-assigned fd values as direct indices into the
fixed-size `handlers` array, with no bounds check on any path
(`runtime/src/handler/handler_manager.cpp`):

- `set_handler(fd, ...)` calls `is_allocated(fd)`, which returns false
  for any fd >= `handlers.size()`, then writes `handlers[fd]`. That is
  an out-of-bounds write into shared memory.
- `get_handler(fd)` and `operator[]` read `handlers[fd]` unchecked.
- `open_fake_fd()` opens `/dev/null` and returns whatever fd the OS
  assigns. Once the pool is exhausted (see issue <#issue-3-number>, which
  leaks one slot per uprobe attach/detach cycle), the OS hands out fds
  past the array bounds. The fd is also never closed in that case.

The result is silent shared-memory corruption followed by a SIGSEGV in
whichever process touches the corrupted region next, far from the root
cause.

## How to reproduce

Attach and detach uprobes in a loop until the handler pool fills (the
leak in issue <#issue-3-number> gets you there in one process), then
attach once more. The next `set_handler` writes out of bounds and the
server or tracee crashes.

## Proposed fix

- `set_handler`: return `-ENOSPC` for out-of-range fds before the
  `is_allocated` check.
- `get_handler` / `operator[]`: return a static `unused_handler`
  sentinel for out-of-range indices.
- `open_fake_fd`: when the fd would exceed `manager->size()`, close it,
  set `errno = ENOSPC`, and return -1.

Two design points worth agreeing on before a PR:

- The read-path sentinel lives in process-local memory while in-range
  handlers live in shared memory. No current caller computes
  shared-memory offsets from the returned reference, but the asymmetry
  deserves a comment.
- The sentinel makes OOB reads silent. A `SPDLOG_WARN` on that path may
  be preferable; happy to add it.

## Reproduction

An in-process harness drives the public shm API (the calls the syscall
server makes on attach/detach), so no Frida or target is needed. With
`BPFTIME_MAX_FD_COUNT=128`, the perf-event slot leak drains the pool and
`open_fake_fd` returns fd 129 at cycle 41; the pinned code uses it as an
unchecked `handlers[129]` index. Rebuilding the same binary with the fix
returns a clean `-ENOSPC` instead. Harness and logs:
https://github.com/maxgio92/xcover/tree/main/patches/bpftime/proofs

I have a working patch and can open a PR:
https://github.com/maxgio92/xcover/blob/main/patches/bpftime/0002-fix-handler-manager-oob-and-open-fake-fd-leak.patch
```

## Issue 3: closing a link fd leaks the perf event handler slot

- Patch: `0003-fix-link-close-leaks-perf-event-handler.patch`
- Suggested title: `clear_id_at() never frees the perf event handler when a bpf_link is closed: one slot leaks per attach/detach cycle`

### Draft body

```markdown
## Environment

- bpftime commit: `5bf24b21af85`, still present on master at `0834957169da` (2026-08-28)
- OS: Fedora 44, kernel `7.1.6-201.fc44.x86_64`

## What happens

When userspace closes a `BPF_PERF_EVENT` link fd, libbpf relies on the
kernel to drop the perf event reference. bpftime never does the
equivalent: `clear_id_at()` in
`runtime/src/handler/handler_manager.cpp` has no branch for
`bpf_link_handler`, so it falls through to
`handlers[fd] = unused_handler()` and frees only the link slot. The
perf event handler referenced by `attach_target_id` stays allocated
forever.

One slot leaks per uprobe per attach/detach cycle. Long-running
processes that re-attach probes (benchmark rounds, test suites) exhaust
the handler pool. With the missing bounds checks reported in issue
<#issue-2-number>, exhaustion then turns into shared-memory corruption
instead of a clean error.

## How to reproduce

Attach a uprobe, detach it (close the link fd), and repeat. Watch the
handler slot count grow monotonically: each cycle permanently consumes
one perf event slot even though both fds were closed.

## Proposed fix

Add a `bpf_link_handler` branch to `clear_id_at()`:

1. Set the link slot to `unused_handler()` first. This prevents
   infinite recursion, because the perf event branch scans handlers for
   links referencing it.
2. Cascade to `clear_id_at(attach_target_id, memory)` to free the perf
   event handler.

Assumption made explicit: one perf event per link, which is how
libbpf's uprobe path behaves. If two links ever shared a perf event,
the cascade would destroy it under the surviving link; reference
counting would be needed at that point.

## Reproduction

Same in-process harness as the handler-bounds issue. After
`bpftime_close(link_fd)`, `bpftime_is_perf_event_fd(perf_fd)` still
returns 1 on the pinned commit (slot leaked); with the fix it returns 0.
Harness and logs:
https://github.com/maxgio92/xcover/tree/main/patches/bpftime/proofs

I have a working patch and can open a PR:
https://github.com/maxgio92/xcover/blob/main/patches/bpftime/0003-fix-link-close-leaks-perf-event-handler.patch
```

## Issue 4: GCC 14 const-qualifier hard errors in bpftool-bundled libbpf

- Patch: `0004-fix-gcc14-const-qualifier-in-bundled-libbpf.patch`
- Suggested title: `Build fails under GCC 14: discarded const qualifiers in bundled libbpf (libbpf.c)`

### Draft body

```markdown
## Environment

- bpftime commit: `5bf24b21af85`, still present on master at `0834957169da` (2026-08-28)
- OS: Fedora 44
- Compiler: GCC 16.1.1 (Red Hat 16.1.1-2); anything from GCC 14 on fails
  the same way

## What happens

GCC 14 promotes `-Wdiscarded-qualifiers` to a hard error. Three string
pointer variables in `third_party/bpftool/libbpf/src/libbpf.c` are
declared `char *` but assigned from string literals or const-returning
functions, so the build fails:

- `res` in `kallsyms_cb`
- `sym_sfx` in `avail_kallsyms_cb`
- `next_path` in `resolve_full_path`

Master still pins bpftool `53c1852920c8`, whose libbpf submodule
`3f077472ee7e` carries all three sites (lines 8205, 11513, 12098).

## How to reproduce

Build bpftime with GCC 14 on any recent distro (Fedora 40+, Ubuntu
24.10+). Compilation of the bundled libbpf stops on the qualifier
errors above.

## Proposed fix

Upstream libbpf already fixed all three sites (commit `f5dcbae`,
2026-03-12). The clean fix is bumping the bpftool submodule past that
commit. Until then, a three-line interim diff changes the declarations
to `const char *`:

https://github.com/maxgio92/xcover/blob/main/patches/bpftime/0004-fix-gcc14-const-qualifier-in-bundled-libbpf.patch

Reproduced by compiling the bundled libbpf with the flags its own
Makefile uses (`-Werror -Wall -std=gnu89`): three
`-Werror=discarded-qualifiers` errors. Log:
https://github.com/maxgio92/xcover/blob/main/patches/bpftime/proofs/issue-c-const-qualifier.log

Happy to open a PR for either option.
```

## Issue 5: conflicting `bpf_stream_vprintk` declaration against kernel 6.15+ vmlinux.h

- Patch: `0005-fix-bpf-stream-vprintk-conflicting-decl.patch`
- Suggested title: `Build fails against kernel 6.15+ BTF: bundled bpf_helpers.h declares bpf_stream_vprintk with a conflicting signature`

### Draft body

```markdown
## Environment

- bpftime commit: `5bf24b21af85`, still present on master at `0834957169da` (2026-08-28)
- OS: Fedora 44, kernel `7.1.6-201.fc44.x86_64` (6.15+ BTF)

## What happens

The bundled `third_party/bpftool/libbpf/src/bpf_helpers.h` declares
`bpf_stream_vprintk` with 5 parameters. `vmlinux.h` generated from
kernel 6.15+ BTF declares it with 4. Any BPF object build that includes
both headers fails with a conflicting-declaration error during skeleton
generation.

Master still pins bpftool `53c1852920c8`, whose libbpf submodule
`3f077472ee7e` carries the 5-parameter declaration (`bpf_helpers.h`
line 318).

## How to reproduce

Generate `vmlinux.h` from a 6.15+ kernel's BTF and build any BPF source
that includes it together with the bundled `bpf_helpers.h`.

## Proposed fix

Nothing in the skeleton path calls `bpf_stream_printk` or
`bpf_stream_vprintk`, so the bundled declaration can be removed.
Upstream bpftool fixed this in `640fb7ceed18` (2025-11-10); as with the
GCC 14 issue, the clean fix is a submodule bump and the interim fix is
a small diff:

https://github.com/maxgio92/xcover/blob/main/patches/bpftime/0005-fix-bpf-stream-vprintk-conflicting-decl.patch

Reproduced by compiling a minimal BPF program that includes both this
kernel's `vmlinux.h` (4-param decl) and the bundled `bpf_helpers.h`
(5-param decl): `error: conflicting types for 'bpf_stream_vprintk'`.
Source and log:
https://github.com/maxgio92/xcover/tree/main/patches/bpftime/proofs

Happy to open a PR for either option.
```

## After the issues

- Replace the `<#issue-2-number>` / `<#issue-3-number>` placeholders once
  the issue numbers exist; issues 2 and 3 reference each other.
- Wait for maintainer acknowledgement on issues 2 and 3 before opening the
  PRs, since both touch design (sentinel semantics, cascade ownership).
- Open one PR per issue using the suggested commit messages already in
  `README.md`, referencing the issue with `Fixes #N`.
- Issues 4 and 5 can go straight to PRs if maintainers prefer; both are
  self-expiring once the bpftool submodule is bumped, so say so in the PR
  description.
- When a fix merges upstream, bump `BPFTIME_COMMIT` in the Makefile and
  delete the corresponding patch file and its README section.
