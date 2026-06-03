# xcover

[![CI](https://github.com/maxgio92/xcover/actions/workflows/ci.yml/badge.svg)](https://github.com/maxgio92/xcover/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/tag/maxgio92/xcover)](https://github.com/maxgio92/xcover/releases)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

**Profile functional test coverage without instrumenting your binaries.**

`xcover` (pronounced "cross cover") revolutionizes functional test coverage profiling by leveraging kernel instrumentation to probe userland functions. This cross-language approach measures coverage directly from ELF binaries, eliminating the need for ecosystem-specific tools like [Go cover](https://go.dev/doc/build-cover) or [LLVM cov](https://llvm.org/docs/CommandGuide/llvm-cov.html).

[![asciicast](https://asciinema.org/a/GyzGzTTEP63GJzAG.svg)](https://asciinema.org/a/GyzGzTTEP63GJzAG)

## Quickstart

Get started with xcover in seconds. Launch the profiler in daemon mode, run your tests, then collect comprehensive coverage metrics:

```shell
$ xcover run --detach --path /path/to/bin
$ xcover wait
xcover is ready
$ /path/to/bin test1
$ /path/to/bin test2
$ /path/to/bin test3
$ xcover stop
xcover is stopped
$ cat xcover_report.json | jq .cov_by_func
89.9786897
```

## How It Works

xcover operates at the kernel level, instrumenting your binary's functions through eBPF probes. This unique approach provides several advantages:

- **Language-agnostic profiling**: Works with any compiled binary
- **Non-invasive instrumentation**: No source code modifications required
- **Production-ready**: Minimal overhead with kernel-level efficiency
- **Comprehensive coverage**: Traces all function calls in real-time

The profiler runs as a daemon, monitoring function invocations as your tests execute. Once complete, xcover generates detailed coverage reports showing exactly which functions were exercised.

## Filter

Target specific processes, binaries, or functions to focus your coverage analysis.

### Filter by Process

Profile a specific running process by PID:

```shell
xcover run --pid PID
```

### Filter by Binary

Target a specific executable path:

```shell
xcover run --path EXE_PATH
```

### Filter Functions

Include specific functions using regex patterns:

```shell
xcover run --path EXE_PATH --include "^github.com/maxgio92/xcover"
```

Exclude functions you don't want to trace:

```shell
xcover run --path EXE_PATH --exclude "^runtime.|^internal"
```

### Function Scope

By default, xcover traces every supported function discovered in the target binary:

```shell
xcover run --path EXE_PATH --scope binary
```

For Go binaries, use project scope to trace only functions that belong to the Go module that produced the binary:

```shell
xcover run --path EXE_PATH --scope project
```

Project scope uses Go build metadata to identify the module path, then keeps root `main.*` functions and symbols prefixed by that module path. This excludes functions from the Go standard library and third-party dependencies before `--include` and `--exclude` filters are applied.

Build Go targets as packages or modules, for example `go build -o app .` or `go build -o app ./cmd/app`. Project scope is not available for single-file Go builds such as `go build main.go`, because Go records those binaries as `command-line-arguments` instead of a module-backed package.

## Symbolization

xcover relies on symbolization to discover function names and addresses within target binaries. Understanding how symbolization works helps you maximize coverage profiling effectiveness.

### Symbol Table

ELF binaries contain a symbol table with function metadata, including names and memory addresses. xcover consumes this table to identify traceable functions. When you apply filters (`--include`, `--exclude`), they operate against these discovered symbols.

### Stripped Binaries

Production binaries often strip the standard symbol table (`.symtab`) to reduce size, removing the metadata xcover needs for standard profiling. To handle stripped binaries, xcover implements language-specific fallback mechanisms.

#### Go

xcover seamlessly supports stripped Go binaries by reading the `.gopclntab` (Go Program Counter Line Table) section. This metadata structure persists even after stripping, providing complete function information for coverage analysis.

The Go symbolization fallback:
- Activates automatically when `.symtab` is unavailable
- Supports Go 1.2+ binaries
- Maintains full compatibility with symbol filtering
- Supports project scope for module-backed Go binaries
- Requires no additional configuration

#### Other binaries (separate debug file)

For stripped non-Go binaries, point `--debug-path` at a matching debug file (e.g. `objcopy --only-keep-debug` output, or a distro `-dbg`/debuginfod artifact) to recover real function names from its `.symtab` (or DWARF):

```shell
$ xcover run --path ./app --debug-path ./app.debug
```

Names come from `--debug-path`; uprobe offsets are computed against `--path`. The two are matched by GNU build-id — pass `--no-build-id-check` for toolchains that omit it.

## Daemon Mode

Run xcover as a background daemon for flexible test execution. This mode enables you to start the profiler, run multiple test suites, and collect results when complete.

Launch the daemon with the `--detach` flag:

```shell
$ xcover run --detach --path /path/to/bin
```

Check the profiler status:

```shell
$ xcover status
xcover is running (PID 1234)
```

Stop the daemon and collect results:

```shell
$ xcover stop
xcover is stopped
```

## Report

xcover automatically generates comprehensive coverage reports in JSON format, detailing traced functions, acknowledged functions, coverage percentages, and executable information.

### Report Structure

The coverage report follows this schema:

```go
type CoverageReport struct {
	FuncsTraced []string `json:"funcs_traced"`
	FuncsAck    []string `json:"funcs_ack"`
	CovByFunc   float64  `json:"cov_by_func"`
	ExePath     string   `json:"exe_path"`
}
```

### Generating Reports

Enable reporting with the `--report` flag when running your profiler:

```shell
$ xcover run --path myapp --verbose=false --report
^C5:02PM INF written report to xcover-report.json
$ cat xcover-report.json | jq '.cov_by_func'
15.601900739176347
```

## Synchronization

Synchronize test execution with xcover's readiness state to ensure accurate coverage capture. The `wait` command blocks until the profiler has fully initialized and begun tracing all target functions.

### Synchronization Workflow

Use the `wait` command to coordinate profiler initialization with test execution:

```shell
$ xcover run --detach --path /path/to/bin
$ xcover wait
1:30PM INF waiting for the profiler to be ready
1:30PM INF profiler is ready
$ /path/to/bin test_1
$ /path/to/bin test_2
$ /path/to/bin test_3
$ xcover stop
```

The coverage report will be available as `xcover-report.json` after stopping the profiler.

## CLI Reference

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
* [xcover status](docs/xcover_status.md)	 - Check the the xcover profiler status
* [xcover stop](docs/xcover_stop.md)	 - Stop the xcover profiler daemon
* [xcover wait](docs/xcover_wait.md)	 - Wait for the xcover profiler to be ready



## Development

### Prerequisites

- `bpftool` (to generate vmlinux.h for CORE)
- `clang`
- `go`
- `libbf-dev`

### Build All

By default, xcover compiles statically with libbpfgo, linking libbpfgo with libbpf:

```shell
make xcover
```

### Build BPF Only

Compile only the BPF components:

```shell
make xcover/bpf
```

### Build Frontend Only

Compile only the frontend application:

```shell
make xcover/frontend
```

### Run Tests

Execute the test suite:

```shell
make test
```
