# Architecture

This page explains how xcover works internally. It is for contributors; users
should start with the [README](../README.md).

## Repository layout

| Path | Responsibility |
|---|---|
| `main.go` | Entry point, calls `pkg/cmd.Execute`. |
| `bpf/trace.bpf.c` | The single BPF program attached to every function. `bpf/vmlinux.h` is generated at build time. |
| `pkg/cmd` | Cobra commands: `run`, `wait`, `status`, `stop`, `merge`; `agent` under the `userspace` build tag. `pkg/cmd/common` holds PID-file helpers, `pkg/cmd/options` the shared logger plumbing. |
| `pkg/trace` | Core: function resolvers, C++ and Rust demangling, scope filtering, the tracee model, the tracer event loop, report assembly. |
| `pkg/probe` | libbpfgo wrapper: loads the embedded BPF object, attaches uprobes, polls the ring buffer. `pkg/probe/output` receives the compiled object. |
| `pkg/coverage` | The `CoverageReport` type and its JSON encoding. |
| `pkg/healthcheck` | Unix-socket readiness server and client behind `xcover wait`. |
| `pkg/bpftime` | Userspace BPF mode: embeds bpftime shared libraries, re-execs xcover with the syscall server preloaded, extracts the agent. Stubbed out without the `userspace` tag. |
| `pkg/static` | Plain `.symtab` reader. Not used by the CLI path. |
| `internal/settings` | Command name and the fixed `/tmp/xcover.{pid,log,sock}` paths. |
| `internal/output` | Terminal status bar. |
| `internal/utils` | Small helpers. |
| `e2e/` | Black-box tests tagged `e2e` that drive the built binary. `e2e/doc.go` carries the feature matrix from every `run` flag, command and failure path to its scenario. |
| `benchmark/` | Per-call latency benchmark with its own Makefile and C targets. |
| `demo/` | asciinema demo scripts, one directory per scenario; see `demo/README.md`. |
| `patches/bpftime/` | Patches applied to the pinned bpftime checkout, each documented in its README. |
| `docs/docs.go` | Generates the CLI reference and `README.md` from `README.md.tpl`. |
| `libbpfgo/` | Git submodule providing libbpf and its Go bindings. |

## Pipeline

```
xcover run --path BIN
   │
   ├─ 1. resolve      ELF → []function{name, demangled, offset}  pkg/trace/resolver*.go, demangle.go
   ├─ 2. filter       exclude, include, scope                     pkg/trace/resolver.go, resolver_go.go, scope.go
   ├─ 3. load         embedded trace.bpf.o → BPF module           pkg/probe/probe.go
   ├─ 4. attach       uprobe_multi links, 65536 offsets per link  pkg/trace/tracer.go, pkg/probe/probe.go
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

Every symbol read from `.symtab` or DWARF is wrapped in a `funcSym` by
`newFuncSym`, which demangles the name once with
`github.com/ianlancetaylor/demangle` (`demangleName` in `demangle.go`). No
format option is passed, so overloads and template instantiations stay
distinct; the only option caps the output at 64 KiB, and input over 16 KiB
passes through unchanged, because symbol names are untrusted input. Names from
`.gopclntab` are Go names and skip the demangler. The filters and
`funcEntriesFromSymbols` share that value, and the latter copies it into
`FunctionEntry.Demangled`. Names without mangling, including the synthetic
recovery names, demangle to themselves. The raw name remains the report key.
Demangling dominates resolution time on large symbol tables, so the pass over
`.symtab` checks the context once per symbol and a cancelled run stops early.

### 2. Filter

`newSymFilter` in `resolver.go` compiles the `--include` and `--exclude`
patterns once into a `symFilter` and returns an error wrapping
`ErrInvalidPattern` for a bad pattern. `SymbolTableResolver` and
`SeparateDebugResolver` build the filter before opening any binary, and
`GoProjectResolver` validates the patterns before it reads the Go build info.
`run` also validates them with `ValidateSymPatterns` before the tracer is built
(see Daemon mode). The filter's `shouldInclude` applies, in order: symbol
binding exclude, symbol binding include (library API only), `--exclude` regex,
`--include` regex. A name pattern matches when it matches the raw or
the demangled name. Exclude wins over include. Project scope filtering runs
after these.

Functions are stored in a map keyed by file offset. The offset is also the BPF
cookie, so two names at the same address (weak aliases, identical code folding)
collapse into one entry.

### 3 and 4. Load and attach

`pkg/probe` embeds `output/trace.bpf.o` with `go:embed`, loads it through
libbpfgo with `SkipMemlockBump` (the kernel accounts BPF memory to the memcg
since 5.11), and sets the expected attach type to `TRACE_UPROBE_MULTI`.

`UserTracer.attachProbe` in `tracer.go` splits offsets into batches of 65536
(`bpfUprobeMultiAttachMaxOffsets`) and calls
`AttachUprobeMulti(-1, exePath, offsets, cookies)` per batch. libbpf passes the
offset and cookie arrays to the kernel by pointer, so the batch is bounded only
by the kernel cap `MAX_UPROBE_MULTI_CNT` (1<<20) per link; 65536 keeps the
per-syscall arrays small while staying well below it. The PID argument `-1`
means every process that maps the file. The first failed batch aborts
`Run` with an error before readiness is signalled; links from earlier batches
are destroyed on close.

In userspace BPF mode bpftime does not implement `uprobe_multi`, so
`attachSingleUprobes` attaches one perf-event uprobe per function instead.

### 5. Readiness

The healthcheck listener on `/tmp/xcover.sock` starts before the BPF module is
loaded, so `xcover wait` can connect early. Each connection blocks until the
tracer closes its ready channel after attach, then receives one byte `0x01`.
The socket is removed on every exit path.

### 6. Events

`bpf/trace.bpf.c` defines three maps: `events`, a 16 MiB ring buffer whose
size `--ringbuf-size` sets before load through `resizeEventsRingBuf`;
`seen_funcs`, a hash map whose `max_entries` is set to the traced function
count before load by `resizeSeenFuncs` (the compiled default of 40960 applies
only when the count is unknown); and `drops`, a one-slot array counter. A
binary built with `-tags e2etest` lets `XCOVER_E2E_SEEN_FUNCS_MAX` cap
`seen_funcs` below the function count (`pkg/probe/seenfuncs_cap_e2etest.go`),
which is how the e2e suite provokes the drops warning; release builds compile
the stub that returns no cap. The
program reads the attach cookie and returns if the cookie is already in
`seen_funcs`. Otherwise it reserves an 8-byte event, inserts the cookie and
submits the event. The insert comes after the reserve. A failed reserve means
the ring buffer was full: the program increments `drops` and returns without
marking the function as seen, so a later call can retry. A rejected insert
discards the event and increments `drops`. `Probe.Drops` reads the counter on
exit. The program only fires on function entry; there is no return probe.

`make xcover/bpf` compiles the program with clang `-target bpf` and
`-D__TARGET_ARCH_<arch>`, where `<arch>` is the libbpf spelling (`x86`,
`arm64`) rather than the `uname -m` one. `bpf_printk` is compiled out unless
the object is built with `XCOVER_DEBUG`, which `make xcover BPF_DEBUG=1` adds.

Userspace polls the ring buffer with a 60 ms timeout into a channel of 4096
events. A single consumer goroutine (`processEvents`) receives from that channel
and `handleEvent` decodes the cookie and stores it in the `ack` map.

### 7. Report

On `SIGINT` or `SIGTERM` the tracer first destroys the links, which detaches the
probes so no new events are produced. It then keeps consuming the event channel
until no event has arrived for 150 ms (`drainQuietPeriod`) and writes
`xcover-report.json` in the current directory when `--report` is true. The quiet
period is a heuristic, not a completion signal; the comment on
`drainQuietPeriod` in `tracer.go` states what it does not guarantee. After the
drain, `warnDrops` reads the `drops` counter through `Probe.Drops` and logs a
warning when it is not zero, whether or not `--report` is set. The warning
names both causes and their remedies: a full ring buffer (raise
`--ringbuf-size`) or a rejected `seen_funcs` insert (narrow the probe set with
`--scope` or `--exclude`). Those calls were not recorded, so the report
undercounts coverage.

The report carries `schema_version`, `xcover_version`, `generated_at`, `kernel`,
`exe_path`, `pid` (when `--pid` restricted the trace) and `build_id` (the GNU
build-id captured when the functions were resolved), then `funcs_traced` (every
resolved function), `funcs_ack` (the names of acknowledged cookies that still
resolve to a function), `cov_by_func` (`len(funcs_ack) / len(funcs_traced) *
100`) and `functions[]` with `name`, `offset` and `hit` per function. Each
`functions[]` entry carries `demangled` when it differs from `name`. Lists are
sorted, and `functions` is ordered by offset, so two reports of the same
session differ only in `generated_at`. Verbose output prints the demangled
name.

## Daemon mode

`--detach` re-executes `os.Args[0] run ...` with `Setsid`, forwarding every flag
that was set except `--detach`, redirecting output to `/tmp/xcover.log` and
writing the child PID to `/tmp/xcover.pid`. The child process is built through
the package-level `execCommand` variable, which tests replace to inspect the
arguments without spawning a daemon. Before re-executing, the parent
validates `--include` and `--exclude` with `ValidateSymPatterns`, so a bad
pattern fails in the foreground instead of in the log file. `WritePID` writes
the PID to a temp file in `/tmp` and renames it over `/tmp/xcover.pid`, so a
concurrent `wait`, `status` or `stop` reads the old content or the new PID,
never an empty file. The child's `setup` writes its own PID only when the file
does not already name it; when the child runs ahead of the parent, both
writes carry the same PID. `status` checks the PID with signal 0. `stop` sends `SIGTERM` and polls every 100 ms for up to
`--timeout` (default 30 seconds). If the daemon is still alive it sends
`SIGKILL`, removes the PID file and exits with an error; if `SIGKILL` itself
fails the PID file is kept.

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

- `internal/utils.Hash` and `pkg/static` are unused by the CLI path.
