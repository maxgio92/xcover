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
- [Report](#report)
- [Overhead](#overhead)
- [Limitations](#limitations)
- [Userspace BPF mode (experimental)](#userspace-bpf-mode-experimental)
- [CLI reference](#cli-reference)
- [Development](#development)

## Requirements

| Requirement | Detail |
|---|---|
| OS and architecture | Linux on x86_64 or arm64. |
| Kernel | 6.6 or newer. xcover attaches probes with `uprobe_multi` links, which landed in Linux 6.6. Attach also relies on BPF cookies (5.15) and memcg-based BPF memory accounting (5.11). |
| Privileges | Root, or `CAP_BPF` plus `CAP_PERFMON`. Run xcover with `sudo` unless you use the [userspace BPF mode](#userspace-bpf-mode-experimental). |
| Target binary | An ELF executable with function symbols (`.symtab`), a Go `.gopclntab` section, or a separate debug file passed with `--debug-path`. Static or dynamic linking both work. |

A kernel with BTF (`/sys/kernel/btf/vmlinux`) is needed to build xcover, not to run it.

## Install

Release archives named `xcover_<version>_linux_x86_64.tar.gz` and
`xcover_<version>_linux_arm64.tar.gz` are the intended distribution channel on
[GitHub Releases](https://github.com/maxgio92/xcover/releases). At the time of
writing no release carries archives, so build from source:

```shell
git clone --recurse-submodules https://github.com/maxgio92/xcover.git
cd xcover
make xcover            # needs clang, bpftool, gcc, libelf and zlib headers
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
   resolved offset through `uprobe_multi` links, in batches of 128 functions.
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

Both flags take a Go (RE2) regular expression matched against the symbol name.
Exclude is evaluated first: a function that matches `--exclude` is dropped even
if it also matches `--include`. RE2 has no negative lookahead, so use `--scope`
or `--exclude` rather than trying to express "everything but" in `--include`.

```shell
xcover run --path EXE_PATH --include "^github.com/maxgio92/xcover"
xcover run --path EXE_PATH --exclude "^runtime\.|^internal"
```

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

Probes attach to the executable file, so by default they fire for every
process that runs it, including processes started after xcover. Pass `--pid`
to record hits from one running process only:

```shell
xcover run --path EXE_PATH --pid 1234
```

The process must exist when xcover attaches, otherwise the attach fails and
xcover exits. Hits from other processes of the same executable are ignored.

## Symbolization

xcover needs a name and an address for each function. It tries these sources in
order.

### 1. ELF symbol table

The `.symtab` section lists functions with names and virtual addresses. Filters
match against these names.

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
| `/tmp/xcover.log` | stdout and stderr of the daemon. Warnings about scope fallback and any attach error land here. |
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
sends `SIGTERM`, waits up to 5 seconds for the daemon to write the report, then
sends `SIGKILL`. A daemon killed with `SIGKILL` writes no report.

## Report

By default (`--report`) xcover writes `xcover-report.json` to the working
directory of the `xcover run` invocation when it stops. An existing file is
overwritten.

```go
type CoverageReport struct {
	FuncsTraced []string `json:"funcs_traced"` // every resolved function, probed or not
	FuncsAck    []string `json:"funcs_ack"`    // functions that ran at least once
	CovByFunc   float64  `json:"cov_by_func"`  // len(funcs_ack) / len(funcs_traced) * 100
	ExePath     string   `json:"exe_path"`
}
```

Notes on the numbers:

- Coverage is per function. There is no line, branch or call-count information.
- By default hits are aggregated across every process that ran the binary
  during the session, and the report does not say which process exercised a
  function. Pass `--pid` to restrict tracing to one process.
- An attach failure aborts the run before readiness is signalled and no report
  is written, so a report always covers every function in `funcs_traced`.
- Past 40960 distinct functions the kernel map is full and further functions
  are not recorded; xcover warns on exit with the number of unrecorded calls.
  See [Limitations](#limitations).

Print the ratio with `jq .cov_by_func xcover-report.json`. Pass `--report=false`
to skip the file.

## Overhead

Every probed call traps into the kernel. The [benchmark](benchmark/README.md)
measures the cost per call on one machine (AMD Ryzen 7 7840U, 100 probes):

| Path | Kernel uprobes | Userspace BPF |
|---|---|---|
| Plain call, no probes | 1.2 ns | 1.2 ns |
| Already-seen function | 1230 ns | 426 ns |
| First hit of a function | 2911 ns | 1084 ns |

That is roughly a thousand times slower per probed call. Programs that call
many small functions in tight loops slow down noticeably; a sub-second command
tracing 15k functions can take close to a minute. Use `--scope project` or
`--exclude` to keep the probe set small. xcover suits functional test runs, not
latency benchmarks.

## Limitations

- **Inlined functions are invisible.** A uprobe needs an entry point in the
  text. Functions the compiler inlined never fire, yet they still appear in
  `funcs_traced` if a symbol exists, which lowers the ratio.
- **Entry only.** xcover records that a function started. It does not see
  returns, arguments or call counts.
- **First hit only, per session.** The kernel map dedups per function, so the
  report answers "did it run", not "how often".
- **At most 40960 distinct functions per session.** Beyond that the kernel map
  is full and the first hit of any further function is dropped, so the report
  undercounts. xcover counts the unrecorded calls and warns on exit. Narrow the
  probe set with `--scope` or `--exclude`.
- **One daemon per host.** State files are fixed under `/tmp`.
- **Kernel 6.6+, Linux only.** On older kernels the attach fails and xcover
  exits with an error.
- **Project scope is Go only** and falls back silently to binary scope for other
  binaries or single-file Go builds. Watch the log.
- **Binary must not change on disk** while a session is running, because probe
  offsets are computed once at start.
- **No merge of multiple reports yet.** Each run writes a fresh file.

Open feature requests and known gaps are tracked in
[GitHub issues](https://github.com/maxgio92/xcover/issues).

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
[the userspace BPF guide](docs/xcover_userspace_bpf.md) for the mechanism,
requirements and full list of limitations.

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

* [xcover run](docs/xcover_run.md)	 - Run the coverage profiling for a program
* [xcover status](docs/xcover_status.md)	 - Check the xcover profiler status
* [xcover stop](docs/xcover_stop.md)	 - Stop the xcover profiler daemon
* [xcover wait](docs/xcover_wait.md)	 - Wait for the xcover profiler to be ready



The `agent extract` subcommand exists only in the userspace build and is not
listed above; see [docs/xcover_userspace_bpf.md](docs/xcover_userspace_bpf.md).

## Development

Build prerequisites, the Makefile targets, the test layers, how to regenerate
this README and the commit conventions are in
[CONTRIBUTING.md](CONTRIBUTING.md). The internals are described in
[docs/architecture.md](docs/architecture.md).

Common targets:

```shell
make xcover             # build libbpf (submodule), the BPF object and the binary
make test-integration   # unit and integration tests (what CI runs)
make test-e2e           # end-to-end tests, needs root (see CONTRIBUTING.md)
make docs               # regenerate docs/xcover*.md and README.md from README.md.tpl
```

`README.md` is generated. Edit `README.md.tpl` and run `make docs`.
