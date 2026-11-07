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

xcover traces every function it resolves in the binary. Three flags narrow
that set.

### Include and exclude by name

Both flags take a Go (RE2) regular expression, matched against the raw symbol
name and, for C++ and Rust, the demangled name: `^app::net::` and `^_ZN3app`
both select the same namespace. Exclude wins over include. RE2 has no negative
lookahead, so express "everything but" with `--exclude` or `--scope`.

```shell
xcover run --path EXE_PATH --include "^github.com/maxgio92/xcover"
xcover run --path EXE_PATH --exclude "^runtime\.|^internal"
xcover run --path EXE_PATH --include "^app::net::" --exclude 'parse\('
xcover run --path EXE_PATH --include '^<?mycrate::'
```

Names come from
[github.com/ianlancetaylor/demangle](https://github.com/ianlancetaylor/demangle),
which is close to `c++filt` but keeps typedefs unexpanded (`std::ostream&`).
C++ overloads keep their parameter list and template instantiations start
with the return type, so leave the pattern unanchored to catch them. An
unanchored pattern also matches parameter types; anchor on the raw name
(`^_ZN[KVRO]*3app3net`) when that matters.

For Rust, the demangler drops the legacy hash suffix and the v0 crate
disambiguator, so monomorphizations share one name. v0 wraps a method's type
in angle brackets (`<mycrate::net::Conn>::open`, closures inside it
`<mycrate::net::Conn>::open::{closure#0}`); legacy names do not
(`mycrate::net::Conn::open`). Free functions, their closures and generic
instantiations keep the `mycrate::` prefix. `^<?mycrate::` catches all of
these. A trait impl for a foreign type renders as
`<i32 as mycrate::net::MyTrait>::run`, which only an unanchored `mycrate::`
matches.

### Scope

```shell
xcover run --path EXE_PATH --scope binary    # default: all resolved functions
xcover run --path EXE_PATH --scope project   # Go binaries: only your module
```

Project scope reads the main module path from the Go build info in the binary.
It keeps `main.*` and functions under that path, and drops the standard
library and dependencies. Include and exclude still apply on top.

Build the target as a package (`go build -o app .`). A single-file
`go build main.go` has no module metadata, so xcover warns and falls back to
binary scope. Non-Go binaries always use binary scope.

### Process filter

```shell
xcover run --path EXE_PATH --pid PID
```

`--pid` restricts tracing to one process. The value is a thread-group leader
PID in xcover's own PID namespace, so a container-local PID selects a
different process on the host. The process must be running when the probes
attach. Without `--pid`, every process that runs the binary counts.

The filter matches the whole thread group. Kernels without commit 46ba0e49b642
(6.6.0 to 6.6.34, 6.9.0 to 6.9.4, and 6.7 or 6.8 without the backport) match
one thread and lose the rest, which is most of a Go program. xcover detects
this at attach and warns with the kernel release; use 6.6.35, 6.9.5, 6.10 or
newer, or drop `--pid`. See [docs/troubleshooting.md](docs/troubleshooting.md).

`--pid` is refused with `--userspace-bpf`: bpftime does not enforce the
uprobe PID.

### Ring buffer size

```shell
xcover run --path EXE_PATH --ringbuf-size SIZE
```

`--ringbuf-size` sets the buffer that carries first hits from the BPF program
to xcover. The default is `16MiB`. The value is a byte count or a number with
a case-sensitive `KiB`, `MiB` or `GiB` suffix. It must be a power of two, a
multiple of the page size and at most `2GiB`; anything else is refused before
the daemon forks.

The kernel allocates the whole buffer at load. A failed load names the size
and, when memory is short, suggests lowering it. Each first hit is one 16-byte
record, so 16 MiB holds about one million. When the buffer is full the call is
dropped and counted in `drops`, and `xcover run` warns on exit with that count.

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
recovery refuses `--include` and `--exclude` (and the library bind
filters) and the run stops with `symbol filters need a symbol table; this
binary has none`. A Go binary with both `.symtab` and `.gopclntab` removed
(for example by `strip` followed by `objcopy --remove-section .gopclntab`) but
readable build info refuses `--scope=project` the same way, because recovery
cannot tell module functions apart. To filter a stripped binary, pass
a matching debug file with `--debug-path` or trace an unstripped build.

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

`wait` polls the socket every 500 ms; `--timeout` sets the limit. `stop` sends
`SIGTERM`, waits up to 30 seconds (`--timeout`) for the report, then sends
`SIGKILL` and exits with an error. A killed daemon writes no report.

If `/tmp/xcover.pid` names a live process, `run --detach` prints
`Daemon already running` and exits 0; stop that session first. `run --detach`
exits 0 as soon as the daemon starts, so a later error such as a failed BPF
load lands in `/tmp/xcover.log`. `wait` and `stop` print its last 20 lines on
stderr when the daemon is gone.

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
directory of `xcover run` when it stops, overwriting an existing file. Pass
`--report=false` to skip it.

| Field | Content |
|---|---|
| `schema_version` | Report schema version, currently 1. |
| `funcs_traced` | Every resolved function, raw symbol names, sorted. |
| `funcs_ack` | Functions that ran at least once, sorted. |
| `cov_by_func` | `len(funcs_ack) / len(funcs_traced) * 100`. |
| `exe_path` | The `--path` argument. |
| `pid` | The `--pid` filter, when one was set. |
| `build_id` | GNU build-id of the binary, read when functions were resolved. |
| `kernel` | `uname -r` of the tracing host. |
| `xcover_version`, `generated_at` | Producer version and RFC 3339 UTC timestamp. |
| `functions` | One entry per probed function, sorted by offset. |

Each `functions` entry holds `name` (raw symbol), `demangled` (C++ or Rust
name, only when it differs), `offset` (file offset of the probe) and `hit`.

Coverage is per function: no lines, branches or call counts. Without `--pid`,
hits aggregate across every process that ran the binary. A failed attach
exits with an error before readiness and writes no report. Print the ratio
with `jq .cov_by_func xcover-report.json`.

### Merging reports

`xcover merge` combines reports from runs of the same binary, such as test
shards or retries, into one; a function is hit when any input hit it.

```shell
$ xcover merge -o merged.json shard-1.json shard-2.json
```

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

Every probed call traps into the kernel, about a thousand times slower than a
plain call, so keep the probe set small with `--scope project` or `--exclude`.
Cost per call from the [benchmark](benchmark/README.md), numbers from
[docs/talks/opensouthcode-2026/slides.md](docs/talks/opensouthcode-2026/slides.md):

| Path | Kernel uprobes | Userspace BPF |
|---|---|---|
| Plain call, no probes | 1.2 ns | 1.2 ns |
| Already-seen function | 1230 ns | 426 ns |
| First hit of a function | 2911 ns | 1084 ns |

## Limitations

- **Inlined functions are invisible:** they never fire but still count in `funcs_traced`.
- **Entry only:** no returns, arguments or call counts.
- **First hit only, per session:** the report answers "did it run", not "how often".
- **Drops:** a full ring buffer or a rejected `seen_funcs` insert drops the call; raise `--ringbuf-size` or narrow the probe set.
- **One daemon per host:** state files are fixed under `/tmp`.
- **Kernel 6.6+, Linux only:** older kernels fail to attach.
- **Project scope is Go only:** other binaries fall back to binary scope with a warning.
- **The binary must not change on disk** during a session: offsets are computed once at start.

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

## xcover

xcover is a functional test coverage profiler

### Synopsis


xcover is a functional test coverage profiler.

Run the 'run' command to run the profiler that will trace all the functions of the tracee program.
Wait for the profiler to be ready before running your tests, with the 'wait' command.
Once the profiler is ready to trace all the functions, you can start running your tests.
At the end of your tests, the profiler can be stopped and a report being collected.


### Options

```
  -h, --help               help for xcover
      --log-level string   Log level (trace, debug, info, warn, error, fatal, panic) (default "info")
```

### SEE ALSO

* [xcover merge](docs/xcover_merge.md)	 - Merge coverage reports of the same binary into one
* [xcover run](docs/xcover_run.md)	 - Run the coverage profiling for a program
* [xcover status](docs/xcover_status.md)	 - Check the xcover profiler status
* [xcover stop](docs/xcover_stop.md)	 - Stop the xcover profiler daemon
* [xcover wait](docs/xcover_wait.md)	 - Wait for the xcover profiler to be ready



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
