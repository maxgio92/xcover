# xcover

[![CI](https://github.com/maxgio92/xcover/actions/workflows/ci.yml/badge.svg)](https://github.com/maxgio92/xcover/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/tag/maxgio92/xcover)](https://github.com/maxgio92/xcover/releases)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

**Function-level test coverage for any ELF binary, with no instrumentation.**

`xcover` (pronounced "cross cover") measures which functions a program executes
while your functional tests run. It attaches eBPF uprobes to the functions of the
binary you ship, so you do not need a coverage-instrumented build or a
language-specific tool such as [Go cover](https://go.dev/doc/build-cover) or
[LLVM cov](https://llvm.org/docs/CommandGuide/llvm-cov.html).

[![asciicast](https://asciinema.org/a/GyzGzTTEP63GJzAG.svg)](https://asciinema.org/a/GyzGzTTEP63GJzAG)

## Table of contents

- [Requirements](#requirements)
- [Install](#install)
- [Quickstart](#quickstart)
- [How it works](#how-it-works)
- [Filtering functions](#filtering-functions)
- [Symbolization](#symbolization)
- [Daemon mode](#daemon-mode)
- [Output and logging](#output-and-logging)
- [Report](#report)
- [Use in CI](#use-in-ci)
- [Overhead](#overhead)
- [Limitations](#limitations)
- [Troubleshooting](#troubleshooting)
- [Userspace BPF mode (experimental)](#userspace-bpf-mode-experimental)
- [CLI reference](#cli-reference)
- [Development](#development)

More pages, grouped by task, are indexed in [docs/README.md](docs/README.md).

## Requirements

| Requirement | Detail |
|---|---|
| OS and architecture | Linux on x86_64 or arm64. |
| Kernel | 6.6 or newer upstream, or a distribution kernel that backports `uprobe_multi` (RHEL 9.4 does on 5.14). Attach also relies on BPF cookies (5.15) and memcg-based BPF memory accounting (5.11). `xcover run` warns at start when the release looks older than 6.6; pass `--skip-preflight` to silence the check (it also skips the capability check). |
| Privileges | Root, or `CAP_BPF` plus `CAP_PERFMON`. Run xcover with `sudo` unless you use the [userspace BPF mode](#userspace-bpf-mode-experimental). `xcover run` checks the effective capability set at start and fails naming what is missing; the check cannot see user-namespace confinement (rootless containers): it may pass there and the BPF load fails instead, so xcover warns when `/proc/self/uid_map` shows it runs in a user namespace. With `--detach` that warning is printed on the terminal before the daemon starts, not in `/tmp/xcover.log`. |
| Target binary | An ELF executable with function symbols (`.symtab`), a Go `.gopclntab` section, or a separate debug file passed with `--debug-path`. Static or dynamic linking both work. |

A kernel with BTF (`/sys/kernel/btf/vmlinux`) is needed to build xcover, not to run it.
CI runs the end-to-end suite on the `ubuntu-24.04` runner kernel and on Linux
6.6 and 6.12 in a VM; see the kernel matrix in [CONTRIBUTING.md](CONTRIBUTING.md#kernel-matrix).

What xcover does with its privileges:

- Loads one BPF program, whose source is [`bpf/trace.bpf.c`](bpf/trace.bpf.c), and attaches it to the functions of the binary you name.
- Reads the target binary and, with `--debug-path`, the debug file. It does not modify either.
- Writes `xcover-report.json` in the current directory and `/tmp/xcover.pid`, `/tmp/xcover.log` and `/tmp/xcover.sock`.
- Makes no network calls.

## Install

Check [GitHub Releases](https://github.com/maxgio92/xcover/releases) for
archives named `xcover_<version>_linux_x86_64.tar.gz` and
`xcover_<version>_linux_arm64.tar.gz`. If the release you need has none,
build from source:

```shell
git clone --recurse-submodules https://github.com/maxgio92/xcover.git
cd xcover
make xcover            # needs clang, bpftool, gcc, libbpf, libelf and zlib headers
sudo install -m 0755 xcover /usr/local/bin/xcover
```

If you do not want to install the toolchain, build inside the pinned container
image (needs Docker and a BTF-enabled host kernel):

```shell
make xcover-container
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full list of build prerequisites.

## Quickstart

Start the profiler as a daemon, wait until the probes are attached, run your
tests, stop the daemon, then read the report:

```shell
$ sudo xcover run --detach --path /path/to/bin
$ sudo xcover wait
xcover is ready
$ /path/to/bin test1
$ /path/to/bin test2
$ /path/to/bin test3
$ sudo xcover stop
xcover stopped (PID 1234)
$ jq .cov_by_func xcover-report.json
89.9786897
```

The report `xcover-report.json` is written in the directory where you ran
`xcover run`. You can also run xcover in the foreground and stop it with
`Ctrl-C`; the report is written on exit either way.

## How it works

1. **Resolve functions.** xcover reads the target ELF, lists its functions and
   turns each function address into a file offset. See [Symbolization](#symbolization).
2. **Attach probes.** It loads one small BPF program and attaches it to every
   resolved offset through `uprobe_multi` links, up to 65536 offsets per link.
   Probes are keyed by the executable's inode, so every current or future
   process running that file is traced.
3. **Record first hits.** When a probed function runs, the BPF program checks a
   kernel hash map. The first hit of each function emits one event to a ring
   buffer; later hits return immediately. xcover only needs to know whether a
   function ran, not how often.
4. **Report.** On stop, xcover writes the list of resolved functions, the list of
   functions that ran at least once, and their ratio.

The whole pipeline is described for contributors in
[docs/architecture.md](docs/architecture.md).

## Filtering functions

By default xcover traces every function it can resolve in the binary. Three
mechanisms narrow that set.

### Include and exclude by name

Both flags take a Go (RE2) regular expression. A pattern matches a function
when it matches the raw symbol name or, for C++ and Rust binaries, its
demangled name: `^app::net::` selects a C++ namespace and `^_ZN3app` keeps
working. Exclude is evaluated first: a function that matches `--exclude` is
dropped even if it also matches `--include`. RE2 has no negative lookahead, so
use `--scope` or `--exclude` rather than trying to express "everything but" in
`--include`.

```shell
xcover run --path EXE_PATH --include "^github.com/maxgio92/xcover"
xcover run --path EXE_PATH --exclude "^runtime\.|^internal"
xcover run --path EXE_PATH --include "^app::net::" --exclude 'parse\('
xcover run --path EXE_PATH --include '^<?mycrate::'
```

Names are rendered by
[github.com/ianlancetaylor/demangle](https://github.com/ianlancetaylor/demangle).
The output is close to `c++filt` but not byte-identical: the library prints
`operator<<(std::ostream&, Foo const&)` where `c++filt` expands the typedef to
`std::basic_ostream<char, std::char_traits<char> >&`. C++ overloads keep their
parameter list (`app::net::parse(int)`), and template instantiations start
with the return type (`double app::net::twice<double>(double)`), so leave the
pattern unanchored to catch them. An unanchored pattern runs over the whole
signature, parameter types included, so `app::net::` also matches a function
elsewhere that takes an `app::net` type; anchor on the raw name
(`^_ZN[KVRO]*3app3net`) when that matters.

Rust names differ from `c++filt` output too: the demangler drops the legacy
hash suffix (`::h5d6b4c8a0f1e2d3b`) and the v0 crate disambiguator
(`[3c1c0]`), so several monomorphizations can share one demangled name and a
pattern must leave both out. v0 names also wrap the type a method belongs to
in angle brackets: an inherent method renders as `<mycrate::net::Conn>::open`
and a closure inside it as `<mycrate::net::Conn>::open::{closure#0}`, while a
legacy inherent method renders as `mycrate::net::Conn::open`. Free functions,
their closures (`mycrate::net::parse::{closure#0}`) and generic
instantiations (`mycrate::net::twice::<i32>`) keep the `mycrate::` prefix.
`^mycrate::` therefore misses every v0 method and the closures inside them;
`^<?mycrate::` catches those too. A trait impl for a type from another crate
renders as `<i32 as mycrate::net::MyTrait>::run`, which neither anchored
pattern matches; leave the pattern unanchored (`mycrate::`) to catch it, with
the same false-positive caveat as for C++.

### Scope

```shell
xcover run --path EXE_PATH --scope binary    # default: all resolved functions
xcover run --path EXE_PATH --scope project   # Go binaries: only your module
```

Project scope reads the Go build information embedded in the binary to find the
main module path. It keeps `main.*` functions and symbols prefixed by that module
path, and drops the standard library and third-party dependencies. Include and
exclude patterns still apply on top of it.

Build the Go target as a package or module (`go build -o app .` or
`go build -o app ./cmd/app`). A single-file build such as `go build main.go`
has no module metadata; xcover logs a warning and falls back to binary scope.
Non-Go binaries always use binary scope.

### Process filter

```shell
xcover run --path EXE_PATH --pid PID
```

`--pid` restricts tracing to one process. The value is a process (thread-group
leader) PID as seen in xcover's own PID namespace, so a container-local PID
handed to a host xcover selects a different process. The process must be
running when the probes attach, otherwise the run fails before readiness is
signalled. Without `--pid`, every process that executes the binary counts
toward coverage.

The kernel `uprobe_multi` filter matches the thread group, so every thread of
the process is traced. Kernels without upstream commit 46ba0e49b642 (6.6.0 to
6.6.34, 6.9.0 to 6.9.4, and 6.7 or 6.8 trees without the backport) match one
thread instead and drop hits from every other thread, which loses most of a Go
program's hits. xcover checks for that bug when the probes attach and, if it
finds it, logs a warning naming the kernel release: the report may undercount,
so use a kernel with the fix (6.6.35, 6.9.5, 6.10 or newer) or drop `--pid`.
See [docs/troubleshooting.md](docs/troubleshooting.md).

The filter is kernel mode only: `--pid` is refused together with
`--userspace-bpf`, because bpftime does not enforce the uprobe PID.

### Ring buffer size

```shell
xcover run --path EXE_PATH --ringbuf-size SIZE
```

`--ringbuf-size` sets the size of the ring buffer that carries first hits
from the BPF program to xcover. The default is `16MiB`. The value is a plain
byte count or a number with a case-sensitive `KiB`, `MiB` or `GiB` suffix.
It must be a power of two, a multiple of the page size and at most `2GiB`.
Any other value is refused before the daemon forks, with the rule in the
message.

The kernel allocates the whole buffer when the BPF object loads. A failed
load names the requested size and, when the kernel is out of memory,
suggests lowering it. Each first hit occupies one 16-byte record, so 16 MiB
holds about one million pending records. When the buffer is full the BPF
program cannot reserve a record. The call is dropped and counted in `drops`,
and `xcover run` warns on exit with that count, naming the ring buffer and
`--ringbuf-size`.

## Symbolization

xcover needs a name and an address for each function. It tries these sources in
order.

### 1. ELF symbol table

The `.symtab` section lists functions with names and virtual addresses. Filters
match against these names and, for C++ and Rust symbols, against their
demangled form.

### 2. Go `.gopclntab` (stripped Go binaries)

Stripping removes `.symtab` but keeps `.gopclntab`, the Go runtime's
program-counter table. xcover parses it with the standard library `debug/gosym`
package, which reads every table format since Go 1.2. This fallback is
automatic, works with all filters and with project scope, and needs no extra
flags.

### 3. Function recovery (stripped non-Go binaries)

When a binary has neither `.symtab` nor `.gopclntab`, xcover uses
[resurgo](https://github.com/maxgio92/resurgo) to detect function entry points
from `.eh_frame` unwind data. Only high-confidence candidates are kept, and they
are named `func_0x<offset>` because no real name is available. Recall drops
sharply on optimised builds, so prefer a debug file when you have one.

A pattern written for real symbols cannot match these synthetic names, so
recovery currently refuses `--include` and `--exclude` (and the library bind
filters) and the run stops with `symbol filters need a symbol table; this
binary has none`. To filter a stripped non-Go binary, pass a matching debug
file with `--debug-path` or trace an unstripped build.

### 4. Separate debug file

Point `--debug-path` at a debug file that matches the stripped binary, such as
the output of `objcopy --only-keep-debug` or a distribution `-dbg` or
debuginfod artefact:

```shell
xcover run --path ./app --debug-path ./app.debug
```

Names come from the debug file's `.symtab`, or from DWARF `DW_TAG_subprogram`
entries when the debug file has no symbol table. Probe offsets are always
computed against `--path`. The two files must carry the same GNU build-id; pass
`--no-build-id-check` for toolchains that omit it. Split DWARF (`-gsplit-dwarf`)
and `dwz` supplementary files are not supported.

## Daemon mode

`--detach` re-executes xcover in a new session and returns once the daemon PID is
written. State lives in fixed paths, so only one xcover daemon can run per host:

| File | Purpose |
|---|---|
| `/tmp/xcover.pid` | PID of the running profiler. |
| `/tmp/xcover.log` | stdout and stderr of the daemon. Scope fallback warnings and the error from a failed attach land here. |
| `/tmp/xcover.sock` | Readiness socket used by `xcover wait`. |

```shell
$ sudo xcover run --detach --path /path/to/bin
$ sudo xcover wait              # blocks until probes are attached, 2 minute timeout
xcover is ready
$ sudo xcover status
xcover is running (PID 1234)
$ sudo xcover stop
xcover stopped (PID 1234)
```

`wait` polls the socket every 500 ms; tune the limit with `--timeout`. `stop`
sends `SIGTERM` and waits up to 30 seconds (`--timeout`) for the daemon to write
the report, then sends `SIGKILL` and exits with an error. A daemon killed with
`SIGKILL` writes no report.

If `/tmp/xcover.pid` names a live process, `run --detach` prints
`Daemon already running`, exits 0 and starts nothing. Run `xcover status` to
see the PID and `xcover stop` to end that session before starting a new one.
`run --detach` also exits 0 as soon as the daemon process has started. An
error a moment later, such as a failed BPF load, lands in `/tmp/xcover.log`.
`xcover wait` and `xcover stop` print its last 20 lines on stderr when the
daemon is gone.

## Output and logging

- `--verbose` prints the demangled name of each function to stdout the first
  time it runs.
- `--status` (on by default) redraws a status line on stderr once per second
  with the coverage so far, events consumed in the last second and ring
  buffer channel usage. Pass `--status=false` when stderr is a file.
- `--log-level` sets the logger level (`trace`, `debug`, `info`, `warn`,
  `error`, `fatal`, `panic`; default `info`). Logs go to stderr. It applies to
  every subcommand.
- With `--detach`, all of the above goes to `/tmp/xcover.log`. Read it when a
  session does not behave as expected.

## Report

By default (`--report`) xcover writes `xcover-report.json` to the working
directory of the `xcover run` invocation when it stops. An existing file is
overwritten.

```go
type CoverageReport struct {
	SchemaVersion int                `json:"schema_version"` // 1
	FuncsTraced   []string           `json:"funcs_traced"`   // every resolved function, sorted
	FuncsAck      []string           `json:"funcs_ack"`      // functions that ran at least once, sorted
	CovByFunc     float64            `json:"cov_by_func"`    // len(funcs_ack) / len(funcs_traced) * 100
	ExePath       string             `json:"exe_path"`       // the --path argument
	PID           int                `json:"pid,omitempty"`  // set when --pid restricted the trace
	BuildID       string             `json:"build_id"`       // GNU build-id of the binary, read when functions were resolved
	Kernel        string             `json:"kernel"`         // uname -r of the tracing host
	XcoverVersion string             `json:"xcover_version"`
	GeneratedAt   string             `json:"generated_at"`   // RFC 3339, UTC
	Functions     []FunctionCoverage `json:"functions"`      // one entry per probed function, sorted by offset
}

type FunctionCoverage struct {
	Name      string `json:"name"`                // raw symbol name
	Demangled string `json:"demangled,omitempty"` // C++ or Rust name, only when it differs from name
	Offset    uint64 `json:"offset"`              // executable file offset where the probe is attached
	Hit       bool   `json:"hit"`
}
```

Notes on the numbers:

- Coverage is per function. There is no line, branch or call-count information.
- Without `--pid`, hits are aggregated across every process that ran the
  binary during the session and the report does not say which process
  exercised a function. With `--pid`, the `pid` field records the process the
  trace was restricted to.
- If any probe fails to attach, `xcover run` exits with an error before
  signalling readiness and writes no report, so a low number never hides an
  attach failure. `xcover wait` returns an error in that case; check
  `/tmp/xcover.log` for the cause.
- `cov_by_func` is `len(funcs_ack) / len(funcs_traced) * 100`. A recorded
  cookie that cannot be mapped back to a function is dropped and never counts.
- `funcs_traced`, `funcs_ack` and `name` hold the raw symbol names; each
  `functions[]` entry carries `demangled` when it differs from `name`.
- Lists are sorted and `functions` is ordered by offset, so two reports of the
  same session differ only in `generated_at` and diff cleanly.
- `build_id` identifies the exact binary measured. It is captured at start-up,
  when the functions are resolved, rather than at exit, so a binary rebuilt or
  removed during the session is still reported under the id it was probed with.

Print the ratio with `jq .cov_by_func xcover-report.json`. Pass `--report=false`
to skip the file.

### Merging reports

`xcover merge` combines reports from separate runs of the same binary, for
example test shards or retries, into one report:

```shell
$ xcover merge -o merged.json shard-1.json shard-2.json
```

Functions are matched by `build_id` and file offset, a function counts as hit
when any input hit it, and `cov_by_func` is recomputed over the union. Inputs
with different `build_id` values are refused; a report without a `build_id`
cannot be verified and is refused unless `--allow-missing-build-id` is set, in
which case the merged `build_id` is empty. Pass `-` to read one report from
stdin.

Symbol aliases share an offset, and each run keeps one name per offset,
normally the one its include pattern left. When two inputs with the same
`build_id` name one offset differently, the merged report keeps the name that
sorts first in byte order, so the other name leaves `funcs_traced` and
`funcs_ack`. Without a verified `build_id` the conflict is refused. The `pid`
field is kept when every input recorded the same `--pid` filter and omitted
otherwise.

## Use in CI

xcover fits a job that already runs your functional tests. The job needs root
(GitHub-hosted runners allow `sudo`) and a kernel of 6.6 or newer, which
`ubuntu-latest` provides. This example assumes `xcover` is already installed on
the runner (see [Install](#install)), traces the tests and fails when fewer
than 80% of the functions ran:

```yaml
jobs:
  coverage:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Build the binary under test
        run: make build           # produces ./bin/app
      - name: Function coverage
        run: |
          set -e
          sudo xcover run --detach --path ./bin/app --scope project
          sudo xcover wait --timeout 5m
          ./bin/app test1
          ./bin/app test2
          sudo xcover stop
          jq -e '.cov_by_func >= 80' xcover-report.json
```

`set -e` makes the step fail on the first error, including a `wait` timeout.
`xcover stop` writes `xcover-report.json` in the directory where `run` was
executed, so keep both commands in the same working directory. `jq -e` exits
1 when the expression is false, which fails the job. Upload
`/tmp/xcover.log` as an artefact when you need to debug a failed run.

## Overhead

Every probed call traps into the kernel. The [benchmark](benchmark/README.md)
measures the cost per call on one machine:

| Path | Kernel uprobes | Userspace BPF |
|---|---|---|
| Plain call, no probes | 1.2 ns | 1.2 ns |
| Already-seen function | 1230 ns | 426 ns |
| First hit of a function | 2911 ns | 1084 ns |

Numbers from the speaker notes of
[docs/talks/opensouthcode-2026/slides.md](docs/talks/opensouthcode-2026/slides.md):
AMD Ryzen 7 7840U, N=100 probes, `benchstat` over 10 rounds. Reproduce with
`make -C benchmark bench && make -C benchmark bench-compare`; expect different
absolute values on other hardware.

That is roughly a thousand times slower per probed call. Programs that call
many small functions in tight loops slow down noticeably; a sub-second command
tracing 15k functions can take close to a minute. Use `--scope project` or
`--exclude` to keep the probe set small. xcover suits functional test runs, not
latency benchmarks.

The BPF handler's debug logging (`bpf_printk`) is compiled out by default so
it does not add to the per-call cost. `make xcover BPF_DEBUG=1` rebuilds the
BPF object with debug logging and embeds it in the binary, turning it back on
for `trace_pipe` inspection.

## Limitations

- **Inlined functions are invisible.** A uprobe needs an entry point in the
  text. Functions the compiler inlined never fire, yet they still appear in
  `funcs_traced` if a symbol exists, which lowers the ratio.
- **Entry only.** xcover records that a function started. It does not see
  returns, arguments or call counts.
- **First hit only, per session.** The kernel map dedups per function, so the
  report answers "did it run", not "how often".
- **Map sizing.** The kernel `seen_funcs` map is sized to the number of traced
  functions. The `drops` counter records every call the BPF program could not
  deliver: either the ring buffer was full, so no record could be reserved, or
  the kernel rejected the `seen_funcs` insert, so the event was discarded.
  `xcover run` warns on exit with that count. The warning lists both causes
  and cannot tell them apart, because both paths add to the same counter. The
  count is the number of dropped calls and can exceed the number of missing
  functions. If `--ringbuf-size` was lowered below the default, raise it
  first. Otherwise narrow the probe set with `--scope` or `--exclude`.
- **One daemon per host.** State files are fixed under `/tmp`.
- **Kernel 6.6+, Linux only.** On older kernels the attach fails and
  `xcover run` exits with an error before signalling readiness.
- **Project scope is Go only.** For other binaries and single-file Go builds
  xcover logs `project scope unavailable, falling back to binary scope` and
  traces everything. In `--detach` mode the warning is only in
  `/tmp/xcover.log`; the report does not record which scope was used.
- **Binary must not change on disk** while a session is running, because probe
  offsets are computed once at start.

Open feature requests and known gaps are tracked in
[GitHub issues](https://github.com/maxgio92/xcover/issues).

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `timeout waiting for profiler readiness` from `xcover wait` | The daemon is still attaching probes, or exited before it was ready. | Raise `--timeout`, narrow the probe set with `--scope` or `--exclude`, and read `/tmp/xcover.log`. |
| `Daemon already running` from `xcover run --detach` (exit 0, nothing started) | `/tmp/xcover.pid` names a live process. | `sudo xcover status`, then `sudo xcover stop`, then start again. |
| `CAP_BPF and CAP_PERFMON not in the effective set` from `xcover run` | xcover lacks root or `CAP_BPF` plus `CAP_PERFMON`; the preflight check stops the run before any BPF object is loaded. | Run with `sudo`, or grant the two capabilities with `setcap cap_bpf,cap_perfmon+ep /path/to/xcover`. |
| `error initializing BPF probe` with `permission denied` or `operation not permitted` | The BPF load ran without privileges: `--skip-preflight` was set, xcover runs in a user namespace (rootless container) where the capability check cannot see the confinement, or a seccomp profile or LSM policy denies `bpf(2)`. | Run with `sudo` in the initial user namespace, or grant the two capabilities there. Check the container runtime's seccomp profile. |

Every other message xcover prints is covered in
[docs/troubleshooting.md](docs/troubleshooting.md).

## Userspace BPF mode (experimental)

xcover can run its BPF program in userspace through the
[bpftime](https://github.com/eunomia-bpf/bpftime) runtime, avoiding the kernel
trap on every probed call. In the benchmark above this cuts per-call cost by
about 65% on the already-seen path and 63% on the first-hit path, and it needs
no `CAP_BPF`.

Build the dedicated binary and preload the bpftime agent into the tracee:

```shell
$ make xcover-userspace
$ ./xcover-userspace run --detach --path /path/to/bin --userspace-bpf
$ ./xcover-userspace wait
$ LD_PRELOAD=$(./xcover-userspace agent extract) /path/to/bin test_1
$ ./xcover-userspace stop
```

The tracee must be dynamically linked against glibc and must start after the
profiler. Statically linked programs, pure Go programs built with
`CGO_ENABLED=0`, musl programs and setuid programs are not supported. Read
[the userspace BPF guide](docs/userspace-bpf.md) for the requirements and
the full list of limitations.

## CLI reference

{{ .CLI_REFERENCE }}

The `agent extract` subcommand exists only in the userspace build and is not
listed above; see [docs/userspace-bpf.md](docs/userspace-bpf.md).

## Development

Build prerequisites, the Makefile targets, the test layers, how to regenerate
this README and the commit conventions are in
[CONTRIBUTING.md](CONTRIBUTING.md). The internals are described in
[docs/architecture.md](docs/architecture.md).

Common targets:

```shell
make xcover             # build libbpf (submodule), the BPF object and the binary
make test-integration   # unit and integration tests (what CI runs)
make test-e2e           # end-to-end tests against ./xcover; skip without root (see CONTRIBUTING.md)
make docs               # regenerate docs/xcover*.md and README.md from README.md.tpl
```

`README.md` is generated. Edit `README.md.tpl` and run `make docs`.
