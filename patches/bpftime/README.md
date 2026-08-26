# bpftime patches

Upstream: <https://github.com/eunomia-bpf/bpftime>
Pinned commit: `5bf24b21af85`

Each patch file corresponds to one bug fix. `make bpftime-libs` applies them
with `patch -p1` right after cloning the pinned bpftime tree.
Once the fixes are validated, each should be opened as a separate PR upstream.

## 0001 — null `injected_pids` crash on tracee exit

**File:** `runtime/src/bpftime_shm_internal.cpp`

**Suggested commit message:**
```
fix(shm): guard against null injected_pids on LD_PRELOAD agent exit

When an LD_PRELOAD agent exits, __destruct_shm calls
remove_pid_from_alive_agent_set(). In the LD_PRELOAD path,
add_pid_into_alive_agent_set() is never called (it is guarded by
`if (injected_with_frida)`), so injected_pids is never populated and
remains null. The unconditional injected_pids->erase(pid) crashes the
tracee process on exit.

Add a null check in remove_pid_from_alive_agent_set() and guard the
get_open_type() call in __destruct_shm with global_shm_initialized to
avoid reading a partially constructed object.
```

**Root cause:** `add_pid_into_alive_agent_set()` is only called for
Frida-injected agents (guarded by `injected_with_frida`), so `injected_pids`
remains null in the LD_PRELOAD path. The destructor calls
`remove_pid_from_alive_agent_set()` unconditionally.

**Fix:** null check on `injected_pids` before `erase()`; check
`global_shm_initialized` before calling `get_open_type()`.

## 0002 — OOB access and fd leak when handler slot pool is exhausted

**Files:** `runtime/src/handler/handler_manager.cpp`,
`runtime/src/bpftime_shm_internal.cpp`

**Suggested commit message:**
```
fix(handler_manager): bounds-check fd in set/get_handler and open_fake_fd

handler_manager::set_handler() calls is_allocated(fd) which returns false
for any fd >= handlers.size(), then unconditionally accesses handlers[fd] —
an out-of-bounds write that corrupts memory and causes a SIGSEGV in the
caller process. get_handler() and operator[] have the same issue on reads.

open_fake_fd() opens a real /dev/null fd whose OS-assigned value is used
directly as the handlers[] index. When the pool is exhausted and the OS
returns a value >= handlers.size(), the fd is never closed and the caller
gets an unchecked OOB index, eventually leading to memory corruption.

- set_handler: return -ENOSPC for out-of-range fds before is_allocated
- get_handler / operator[]: return a static unused_handler sentinel for OOB
- open_fake_fd: close the fd and return -1/ENOSPC when fd >= manager->size()
```

**Root cause:** OS fd values are used directly as `handlers[]` indices with no
bounds check. When the handler pool is exhausted the OS assigns fds beyond the
array bounds, causing silent OOB writes and memory corruption.

**Fix:**
- `set_handler`: return `-ENOSPC` for out-of-range fds before the `is_allocated` check.
- `get_handler` / `operator[]`: return a static `unused_handler` sentinel for OOB indices.
- `open_fake_fd`: close the fd and return `-1` with `errno=ENOSPC` when the value would exceed `manager->size()`.

## 0003 — link close leaks perf event handler slot

**File:** `runtime/src/handler/handler_manager.cpp`

**Suggested commit message:**
```
fix(handler_manager): cascade link close to free perf event handler slot

When a BPF_PERF_EVENT link is destroyed (close(link_fd)), libbpf relies
on the kernel to drop the perf event reference. In bpftime userspace that
never happens: clear_id_at() for a bpf_link_handler only freed the link
slot, leaving the associated perf event handler permanently allocated.
One slot leaked per uprobe per attach/detach cycle, exhausting the
handler pool across repeated benchmark rounds.

In clear_id_at(), when clearing a bpf_link_handler, set the link slot to
unused_handler first (to prevent infinite recursion via the perf event
handler's own link scan), then cascade to clear_id_at(attach_target_id)
to free the perf event handler.
```

**Root cause:** `clear_id_at()` had no branch for `bpf_link_handler`. It fell
through to `handlers[fd] = unused_handler()` without touching the perf event
handler referenced by `attach_target_id`. The kernel drops its perf event
reference when a link fd is closed; bpftime must do this explicitly.

**Fix:** Add an `else if` branch for `bpf_link_handler` in `clear_id_at()`.
Clear the link slot first, then call `clear_id_at(attach_target_id, memory)`
to cascade cleanup to the perf event handler.

## 0004 — GCC 14 const-qualifier errors in bpftool-bundled libbpf

**File:** `third_party/bpftool/libbpf/src/libbpf.c`

**Suggested commit message:**
```
fix(libbpf): fix const-qualifier discards hard-erroring under GCC 14

Three string pointer variables are assigned from string literals or
const-returning functions but declared as non-const char *. GCC 14
promotes these implicit const-qualifier discards from warnings to
hard errors, breaking the build.

Upstream fix: libbpf commit f5dcbae (2026-03-12).
Remove once bpftime bumps its bpftool submodule past that date.
```

**Root cause:** GCC 14 changed the default for `-Wdiscarded-qualifiers` to
be an error. Three variables in `libbpf.c` assign const strings to non-const
pointers.

**Fix:** Change `char *res`, `char *sym_sfx`, and `char *next_path` to
`const char *` at their declaration sites.

## 0005 — conflicting bpf_stream_vprintk declaration vs kernel 6.15+

**File:** `third_party/bpftool/libbpf/src/bpf_helpers.h`

**Suggested commit message:**
```
fix(bpf_helpers): remove conflicting bpf_stream_vprintk declaration

The bundled bpf_helpers.h declares bpf_stream_vprintk with 5 parameters.
vmlinux.h generated from kernel 6.15+ BTF declares it with 4. The
conflicting declaration causes a compilation error. The bpftool skeleton
sources do not call bpf_stream_printk or bpf_stream_vprintk, so the
bundled declaration can be safely removed.

Upstream fix: bpftool commit 640fb7ceed18 (2025-11-10).
Remove once bpftime bumps its bpftool submodule past that date.
```

**Root cause:** The parameter count of `bpf_stream_vprintk` changed between
the bpftool-bundled libbpf and the kernel 6.15+ BTF, causing a conflicting
declaration error when both headers are included.

**Fix:** Remove the `bpf_stream_vprintk` declaration from
`bpf_helpers.h` using a regex substitution.
