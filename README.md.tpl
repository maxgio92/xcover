# xcover

[![CI](https://github.com/maxgio92/xcover/actions/workflows/ci.yml/badge.svg)](https://github.com/maxgio92/xcover/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/tag/maxgio92/xcover)](https://github.com/maxgio92/xcover/releases)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

**Function-level test coverage for any ELF binary, with no instrumentation.**

`xcover` (pronounced "cross cover") records which functions a program runs
during your functional tests, through eBPF uprobes on the binary you ship. You
need no instrumented build and no language tool such as
[Go cover](https://go.dev/doc/build-cover) or
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

More pages, grouped by task: [docs/README.md](docs/README.md).

## Requirements

| Requirement | Detail |
|---|---|
| OS and architecture | Linux on x86_64 or arm64. |
| Kernel | 6.6 or newer, or a distribution kernel that backports `uprobe_multi` (RHEL 9.4 on 5.14). `xcover run` warns when the kernel looks older; see [docs/troubleshooting.md](docs/troubleshooting.md). |
| Privileges | Root, or `CAP_BPF` plus `CAP_PERFMON`; `xcover run` names any missing capability at start. Use `sudo` unless you run the [userspace BPF mode](#userspace-bpf-mode-experimental). |
| Target binary | An ELF executable with a `.symtab`, a Go `.gopclntab` section, or a debug file passed with `--debug-path`. Static or dynamic linking both work. |

The end-to-end suite runs on Linux 6.6, 6.12 and the Ubuntu 24.04 runner
kernel; see the [kernel matrix](CONTRIBUTING.md#kernel-matrix).

With its privileges xcover:

- Loads one small BPF program and attaches it to the functions of the named binary; see [docs/architecture.md](docs/architecture.md#3-and-4-load-and-attach).
- Reads the target binary and, with `--debug-path`, the debug file; modifies neither.
- Writes `xcover-report.json` in the current directory, plus `/tmp/xcover.pid`, `/tmp/xcover.log` and `/tmp/xcover.sock`.
- Makes no network calls.

## Install

Download the archive for your architecture from
[GitHub Releases](https://github.com/maxgio92/xcover/releases), unpack it and
put `xcover` on your `PATH`.

### Build from source

Building needs a kernel with BTF (`/sys/kernel/btf/vmlinux`); running does not.

```shell
git clone --recurse-submodules https://github.com/maxgio92/xcover.git
cd xcover
make xcover            # needs clang, bpftool, gcc, libbpf, libelf and zlib headers
sudo install -m 0755 xcover /usr/local/bin/xcover
```

Without the toolchain, build in the pinned container image (needs Docker and a
BTF-enabled host kernel):

```shell
make xcover-container
```

[CONTRIBUTING.md](CONTRIBUTING.md) lists every build prerequisite.

## Quickstart

Start the daemon, wait for the probes, run your tests, stop, read the report:

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

The report lands where you ran `xcover run`. In the foreground, `Ctrl-C` stops
xcover and writes the report too.

## How it works

1. **Resolve functions.** xcover reads the ELF and turns each function address
   into a file offset; see [Symbolization](#symbolization).
2. **Attach probes.** It attaches one small BPF program to every offset through
   `uprobe_multi` links, keyed by the executable's inode, so every current or
   future process running that file is traced.
3. **Record first hits.** The first run of a probed function emits one event
   to a ring buffer; later runs return at once.
4. **Report.** On stop, xcover writes the resolved functions, the ones that
   ran, and their ratio.

The full pipeline is in [docs/architecture.md](docs/architecture.md).

## Filtering functions

xcover traces every function it resolves. Three flags narrow the set.

### Include and exclude by name

Both flags take a Go (RE2) regular expression matched against the raw symbol
name and, for C++ and Rust, the demangled name, so `^app::net::` and `^_ZN3app`
select the same namespace. Exclude wins over include.

RE2 has no negative lookahead: express "everything but" with `--exclude` or
`--scope`.

```shell
xcover run --path EXE_PATH --include "^github.com/maxgio92/xcover"
xcover run --path EXE_PATH --exclude "^runtime\.|^internal"
xcover run --path EXE_PATH --include "^app::net::" --exclude 'parse\('
xcover run --path EXE_PATH --include '^<?mycrate::'
```

C++ names keep their parameter list and Rust v0 methods wrap the type in angle
brackets, so `^<?mycrate::` and unanchored patterns catch more;
[docs/symbol-patterns.md](docs/symbol-patterns.md) shows how names render.

### Scope

```shell
xcover run --path EXE_PATH --scope binary    # default: all resolved functions
xcover run --path EXE_PATH --scope project   # Go binaries: only your module
```

Project scope reads the main module path from the Go build info, keeps `main.*`
and functions under that path, and drops the standard library and
dependencies. Include and exclude still apply.

Build the target as a package (`go build -o app .`); a single-file
`go build main.go` has no module metadata, so xcover warns and uses binary
scope. Non-Go binaries always use binary scope.

### Process filter

```shell
xcover run --path EXE_PATH --pid PID
```

`--pid` traces one process, which must be running when the probes attach;
without it, every process running the binary counts. The value is a
thread-group leader PID in xcover's PID namespace, so a container-local PID
selects a different process on the host.

Kernels 6.6.0 to 6.6.34 and 6.9.0 to 6.9.4 filter by thread and lose most
hits; xcover warns at attach, see
[docs/troubleshooting.md](docs/troubleshooting.md).

`--pid` is refused with `--userspace-bpf`: bpftime does not enforce the
uprobe PID.

### Ring buffer size

```shell
xcover run --path EXE_PATH --ringbuf-size SIZE
```

`--ringbuf-size` sets the buffer that carries first hits to xcover: default
`16MiB`, a power of two up to `2GiB`, with a `KiB`, `MiB` or `GiB` suffix. A
full buffer drops calls, counted in `drops`; see
[docs/troubleshooting.md](docs/troubleshooting.md).

## Symbolization

xcover needs a name and an address for each function and takes them from the
first of these sources that applies.

### 1. ELF symbol table

`.symtab` lists functions with names and virtual addresses. Filters match
these names and, for C++ and Rust, their demangled form.

### 2. Go `.gopclntab` (stripped Go binaries)

Stripping removes `.symtab` but keeps `.gopclntab`, the Go runtime's
program-counter table, which xcover reads in every format since Go 1.2. The
fallback is automatic and works with all filters and project scope.

### 3. Function recovery (stripped non-Go binaries)

With neither section, xcover uses [resurgo](https://github.com/maxgio92/resurgo)
to detect function entry points from `.eh_frame` unwind data; only
high-confidence candidates are kept, named `func_0x<offset>`. Recall drops
sharply on optimised builds, so prefer a debug file.

Recovery refuses `--include`, `--exclude` and `--scope=project`, since patterns
cannot match synthetic names; pass `--debug-path` or trace an unstripped build
instead.

### 4. Separate debug file

Point `--debug-path` at a debug file matching the stripped binary, such as
`objcopy --only-keep-debug` output or a distribution `-dbg` or debuginfod
artefact:

```shell
xcover run --path ./app --debug-path ./app.debug
```

Names come from the debug file's `.symtab`, or its DWARF `DW_TAG_subprogram`
entries when it has none; probe offsets are always computed against `--path`.

Both files must carry the same GNU build-id; pass `--no-build-id-check` for
toolchains that omit it. Split DWARF (`-gsplit-dwarf`) and `dwz` supplementary
files are not supported.

## Daemon mode

`--detach` re-executes xcover in a new session and returns once the daemon PID
is written. State lives in fixed paths, so one daemon runs per host:

| File | Purpose |
|---|---|
| `/tmp/xcover.pid` | PID of the running profiler. |
| `/tmp/xcover.log` | Daemon stdout and stderr, including scope fallback warnings and attach errors. |
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

`wait` polls the socket every 500 ms, up to `--timeout`. `stop` sends
`SIGTERM`, waits up to 30 seconds (`--timeout`) for the report, then sends
`SIGKILL` and exits with an error; a killed daemon writes no report.

If `/tmp/xcover.pid` names a live process, `run --detach` prints
`Daemon already running` and exits 0; stop that session first. `run --detach`
also exits 0 as soon as the daemon starts, so later errors such as a failed
BPF load land in `/tmp/xcover.log`, whose last 20 lines `wait` and `stop`
print when the daemon is gone.

## Output and logging

- `--verbose` prints each function's demangled name to stdout the first time it runs.
- `--status` (default on) redraws a status line on stderr every second: coverage so far, events in the last second and ring buffer channel usage. Pass `--status=false` when stderr is a file.
- `--log-level` sets the stderr logger level for every subcommand: `trace`, `debug`, `info` (default), `warn`, `error`, `fatal` or `panic`.
- With `--detach`, all of the above goes to `/tmp/xcover.log`; read it when a session misbehaves.

## Report

`xcover run` writes `xcover-report.json` to its working directory on stop
(`--report`, default on), overwriting any existing file; `--report=false`
skips it.

| Field | Content |
|---|---|
| `schema_version` | Report schema version, currently 1. |
| `funcs_traced` | Every resolved function, raw names, sorted. |
| `funcs_ack` | Functions that ran at least once, sorted. |
| `cov_by_func` | `len(funcs_ack) / len(funcs_traced) * 100`. |
| `exe_path` | The `--path` argument. |
| `pid` | The `--pid` filter, when set. |
| `build_id` | GNU build-id of the binary. |
| `kernel` | `uname -r` of the tracing host. |
| `xcover_version`, `generated_at` | Producer version and RFC 3339 UTC timestamp. |
| `functions` | One entry per probed function, sorted by offset. |

Each `functions` entry holds `name` (raw symbol), `demangled` (C++ or Rust
name, when it differs), `offset` (probe file offset) and `hit`.

Coverage is per function: no lines, branches or call counts. Without `--pid`,
hits aggregate across every process that ran the binary.

### Merging reports

`xcover merge` combines reports from runs of the same binary, such as test
shards or retries; a function is hit when any input hit it.

```shell
$ xcover merge -o merged.json shard-1.json shard-2.json
```

Stdin input and `--allow-missing-build-id` are in [xcover merge](docs/xcover_merge.md).

## Use in CI

xcover fits a job that already runs your functional tests. The job needs root
(GitHub-hosted runners allow `sudo`) and a 6.6 or newer kernel, which
`ubuntu-latest` provides.

This example assumes `xcover` is [installed](#install) on the runner and fails
below 80% function coverage:

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

`set -e` fails the step on the first error, including a `wait` timeout;
`jq -e` exits 1 when the expression is false.

Keep `run` and `stop` in the same working directory, where the report lands.
Upload `/tmp/xcover.log` as an artefact to debug a failed run.

## Overhead

Every probed call traps into the kernel, about a thousand times slower than a
plain call, so keep the probe set small with `--scope project` or `--exclude`.
The [benchmark](benchmark/README.md) measures the cost per call; the numbers
below are from
[docs/talks/opensouthcode-2026/slides.md](docs/talks/opensouthcode-2026/slides.md).

| Path | Kernel uprobes | Userspace BPF |
|---|---|---|
| Plain call, no probes | 1.2 ns | 1.2 ns |
| Already-seen function | 1230 ns | 426 ns |
| First hit of a function | 2911 ns | 1084 ns |

xcover suits functional test runs, not latency benchmarks: a sub-second
command tracing 15k functions can take close to a minute.

## Limitations

- **Inlined functions are invisible:** they never fire but still count in `funcs_traced`.
- **Entry only:** no returns, arguments or call counts.
- **First hit only, per session:** the report answers "did it run", not "how often".
- **Drops:** a full ring buffer or a rejected kernel map insert loses the call; see [Coverage is lower than expected](docs/troubleshooting.md#coverage-is-lower-than-expected).
- **One daemon per host:** state files are fixed under `/tmp`.
- **Kernel 6.6+, Linux only:** older kernels fail to attach.
- **Project scope is Go only:** other binaries fall back to binary scope with a warning.
- **The binary must not change on disk** during a session: offsets are computed once at start.

Feature requests and known gaps: [GitHub issues](https://github.com/maxgio92/xcover/issues).

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `timeout waiting for profiler readiness` from `xcover wait` | The daemon is still attaching probes, or exited first. | Raise `--timeout`, narrow the probe set with `--scope` or `--exclude`, read `/tmp/xcover.log`. |
| `Daemon already running` from `xcover run --detach` (exit 0, nothing started) | `/tmp/xcover.pid` names a live process. | `sudo xcover status`, `sudo xcover stop`, start again. |
| `CAP_BPF and CAP_PERFMON not in the effective set` from `xcover run` | xcover lacks root or `CAP_BPF` plus `CAP_PERFMON`; the preflight check stops before any BPF load. | Run with `sudo`, or `setcap cap_bpf,cap_perfmon+ep /path/to/xcover`. |
| `error initializing BPF probe` with `permission denied` or `operation not permitted` | Unprivileged BPF load: `--skip-preflight` was set, xcover runs in a user namespace (rootless container) the capability check cannot see, or a seccomp profile or LSM policy denies `bpf(2)`. | Run with `sudo` in the initial user namespace, or grant the two capabilities there; check the container runtime's seccomp profile. |

Other messages: [docs/troubleshooting.md](docs/troubleshooting.md).

## Userspace BPF mode (experimental)

xcover can run its BPF program in userspace through
[bpftime](https://github.com/eunomia-bpf/bpftime), skipping the kernel trap on
every probed call. In the benchmark above this cuts per-call cost by about 65%
on the already-seen path and 63% on the first hit, and needs no `CAP_BPF`.

Release archives do not include this binary; build it, then preload the
bpftime agent into the tracee:

```shell
$ make xcover-userspace
$ ./xcover-userspace run --detach --path /path/to/bin --userspace-bpf
$ ./xcover-userspace wait
$ LD_PRELOAD=$(./xcover-userspace agent extract) /path/to/bin test_1
$ ./xcover-userspace stop
```

The tracee must be dynamically linked against glibc and start after the
profiler; static, musl, setuid and `CGO_ENABLED=0` Go programs are not
supported. [The userspace BPF guide](docs/userspace-bpf.md) lists the
requirements and every limitation.

## CLI reference

{{ .CLI_REFERENCE }}

`agent extract` exists only in the userspace build and is not listed above;
see [docs/userspace-bpf.md](docs/userspace-bpf.md).

## Development

[CONTRIBUTING.md](CONTRIBUTING.md) covers build prerequisites, test layers,
documentation generation and commit conventions;
[docs/architecture.md](docs/architecture.md) covers the internals.

Common targets:

```shell
make xcover             # build the BPF object and the binary
make test-integration   # unit and integration tests
make test-e2e           # end-to-end tests; needs root
make docs               # regenerate the CLI reference and this README
```
