# Architecture

This page explains how xcover works internally. It is for contributors; users
should start with the [README](../README.md).

## Repository layout

| Path | Responsibility |
|---|---|
| `main.go` | Entry point, calls `pkg/cmd.Execute`. |
| `bpf/trace.bpf.c` | The single BPF program attached to every function. `bpf/vmlinux.h` is generated at build time. |
| `pkg/cmd` | Cobra commands: `run`, `wait`, `status`, `stop`; `agent` under the `userspace` build tag. `pkg/cmd/common` holds PID-file helpers, `pkg/cmd/options` the shared logger plumbing. |
| `pkg/trace` | Core: function resolvers, scope filtering, the tracee model, the tracer event loop, report assembly. |
| `pkg/probe` | libbpfgo wrapper: loads the embedded BPF object, attaches uprobes, polls the ring buffer. `pkg/probe/output` receives the compiled object. |
| `pkg/coverage` | The `CoverageReport` type and its JSON encoding. |
| `pkg/healthcheck` | Unix-socket readiness server and client behind `xcover wait`. |
| `pkg/bpftime` | Userspace BPF mode: embeds bpftime shared libraries, re-execs xcover with the syscall server preloaded, extracts the agent. Stubbed out without the `userspace` tag. |
| `pkg/static` | Plain `.symtab` reader. Not used by the CLI path. |
| `internal/settings` | Command name and the fixed `/tmp/xcover.{pid,log,sock}` paths. |
| `internal/output` | Terminal status bar. |
| `internal/utils` | Small helpers. |
| `e2e/` | Black-box tests tagged `e2e` that drive the built binary. |
| `benchmark/` | Per-call latency benchmark with its own Makefile and C targets. |
| `demo/` | asciinema demo scripts, one directory per scenario; see `demo/README.md`. |
| `patches/bpftime/` | Patches applied to the pinned bpftime checkout, each documented in its README. |
| `docs/docs.go` | Generates the CLI reference and `README.md` from `README.md.tpl`. |
| `libbpfgo/` | Git submodule providing libbpf and its Go bindings. |

## Pipeline

```
xcover run --path BIN
   │
   ├─ 1. resolve      ELF → []function{name, file offset}        pkg/trace/resolver*.go
   ├─ 2. filter       exclude, include, scope                     pkg/trace/resolver.go, resolver_go.go, scope.go
   ├─ 3. load         embedded trace.bpf.o → BPF module           pkg/probe/probe.go
   ├─ 4. attach       uprobe_multi links, 128 offsets per link    pkg/trace/tracer.go, pkg/probe/probe.go
   ├─ 5. ready        close readiness channel, serve /tmp/xcover.sock   pkg/healthcheck
   ├─ 6. events       ring buffer → channel → ack map             pkg/probe/probe.go, pkg/trace/tracer.go
   └─ 7. report       on SIGINT/SIGTERM write xcover-report.json  pkg/trace/tracer.go, pkg/coverage
```

### 1. Resolve functions

`pkg/trace/tracee.go` picks a `FunctionResolver`:

- `SeparateDebugResolver` (`resolver_debug.go`) when `--debug-path` is set. It
  verifies the GNU build-id from the `PT_NOTE` program headers of both files
  (skipped with `--no-build-id-check`), reads function symbols from the debug
  file's `.symtab`, and falls back to DWARF `DW_TAG_subprogram` entries when the
  debug file has no symbol table. Symbols with value 0 or `SHN_UNDEF` are
  dropped so a PIE never gets a probe on its ELF header. Offsets are always
  computed against the executable.
- `GoProjectResolver` (`resolver_go.go`) when `--scope project` is set. It reads
  `debug/buildinfo`, delegates to `SymbolTableResolver`, then keeps names
  prefixed by the main module path or by `main.`. Without build info it returns
  `ErrProjectScopeUnsupported` and the caller falls back to binary scope with a
  warning.
- `SymbolTableResolver` (`resolver.go`) otherwise. It reads `STT_FUNC` symbols
  from `.symtab`. If the section is missing it parses `.gopclntab` with
  `debug/gosym`, using the `.text` section header as the text base. If that
  fails it returns `ErrNoSymbolTable`.
- `RecoveryResolver` (`resolver.go`) when `ErrNoSymbolTable` comes back. It
  runs resurgo's `.eh_frame` based detection, keeps `ConfidenceHigh` candidates
  and names them `func_0x<offset>`.

Virtual addresses become file offsets by walking `PT_LOAD` segments. Because
uprobes are addressed by file offset, PIE and ASLR need no special handling.

### 2. Filter

`shouldInclude` in `resolver.go` applies, in order: symbol binding exclude,
symbol binding include (library API only), `--exclude` regex, `--include`
regex. Exclude wins over include. Project scope filtering runs after these.

Functions are stored in a map keyed by file offset. The offset is also the BPF
cookie, so two names at the same address (weak aliases, identical code folding)
collapse into one entry.

### 3 and 4. Load and attach

`pkg/probe` embeds `output/trace.bpf.o` with `go:embed`, loads it through
libbpfgo with `SkipMemlockBump` (the kernel accounts BPF memory to the memcg
since 5.11), and sets the expected attach type to `TRACE_UPROBE_MULTI`.

`UserTracer.attachProbe` in `tracer.go` splits offsets into batches of 128, the
largest that fits the kernel's `bpf_attr` buffer, and calls
`AttachUprobeMulti(-1, exePath, offsets, cookies)` per batch. The PID argument
`-1` means every process that maps the file. A failed batch is logged as a
warning and skipped; the tracer still reports readiness.

In userspace BPF mode bpftime does not implement `uprobe_multi`, so
`attachSingleUprobes` attaches one perf-event uprobe per function instead.

### 5. Readiness

The healthcheck listener on `/tmp/xcover.sock` starts before the BPF module is
loaded, so `xcover wait` can connect early. Each connection blocks until the
tracer closes its ready channel after attach, then receives one byte `0x01`.
The socket is removed on every exit path.

### 6. Events

`bpf/trace.bpf.c` defines two maps: `events`, a 256 MB ring buffer, and
`seen_funcs`, a hash map of 40960 cookies. The program reads the attach cookie,
returns if the cookie is already in `seen_funcs`, otherwise inserts it and
submits an 8-byte event. The program only fires on function entry; there is no
return probe.

Userspace polls the ring buffer with a 60 ms timeout into a channel of 4096 events, a
second goroutine forwards them, and `handleEvent` decodes the cookie and stores
it in the `ack` map.

### 7. Report

On `SIGINT` or `SIGTERM` the tracer drains its goroutines, destroys the links
(which detaches the probes) and writes `xcover-report.json` in the current
directory when `--report` is true. `funcs_traced` is every resolved function,
`funcs_ack` the names found for acknowledged cookies, `cov_by_func` the ratio
of acknowledged cookies to resolved functions times 100.

## Daemon mode

`--detach` re-executes `os.Args[0] run ...` with `Setsid`, forwarding every flag
that was set except `--detach`, redirecting output to `/tmp/xcover.log` and
writing the child PID to `/tmp/xcover.pid`. `status` checks the PID with
signal 0. `stop` sends `SIGTERM`, polls for 5 seconds, then `SIGKILL`s and
removes the PID file.

## Userspace BPF mode

With the `userspace` build tag, `pkg/bpftime` embeds `bpftime-syscall-server.so`
and `bpftime-agent.so` from `pkg/bpftime/libs`. When `--userspace-bpf` is set,
`EnsureSyscallServer` writes the syscall server into a `memfd`, prepends
`/proc/self/fd/N` to `LD_PRELOAD`, sets `XCOVER_BPFTIME_LOADED=1` and re-execs
xcover. The re-executed process handles BPF syscalls in userspace. The tracee
must load the agent through `LD_PRELOAD`; `xcover agent extract` writes it to a
temporary file and prints the path. See
[userspace-bpf.md](userspace-bpf.md).

## Known issues in the code

These are visible from reading the code and worth knowing before you change the
related areas:

- `--pid` is parsed into `Options.pid` but never used; attach always passes
  `-1`.
- `Probe.Attach` returns `nil` after a failed `uprobe_multi` attach, so partial
  instrumentation is silent apart from a warning.
- In `writeReport`, the `ack.Range` callback returns `false` on a cookie it
  cannot resolve, which stops the iteration and truncates `funcs_ack`.
  `cov_by_func` uses the raw ack count, so it can disagree with
  `len(funcs_ack)`. See issue #175.
- `bpf/trace.bpf.c` calls `bpf_printk` on every hit, including the fast path.
- `bpf_map_update_elem` on `seen_funcs` is not checked; past 40960 entries every
  call of an untracked function emits an event.
- `shouldInclude` compiles the include and exclude regexes once per symbol, and
  an invalid pattern panics instead of returning an error.
- `internal/utils.Hash` and `pkg/static` are unused by the CLI path.
