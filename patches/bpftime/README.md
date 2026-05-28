# bpftime patches

Upstream: <https://github.com/eunomia-bpf/bpftime>
Pinned commit: `5bf24b21af85`

Each patch file corresponds to one bug fix. They are applied inline during
`make bpftime-libs` via Python string replacements in the root `Makefile`.
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
