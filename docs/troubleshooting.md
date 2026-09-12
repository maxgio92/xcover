# Troubleshooting

Each entry lists the message you see, why it happens and what to do. Messages
are quoted from the source and the file that prints them is named, so you can
check the current wording. In `--detach` mode the daemon's own output goes to
`/tmp/xcover.log`; `xcover run --detach` itself exits 0 as soon as the daemon
is started, even if the daemon fails a moment later. Read that file when a
command prints nothing useful.

## `xcover is not running`

Printed by `xcover wait` (`pkg/cmd/wait/wait.go`) and by `xcover status`
(`pkg/cmd/status/status.go`).

**Cause.** `/tmp/xcover.pid` is missing, or the PID it names is not alive.
Either `xcover run --detach` was never started, or the daemon exited before
`wait` ran, most often because the BPF program failed to load or the function
list was empty.

**Fix.** Read `/tmp/xcover.log` for the daemon's error and fix that first.

## `xcover exited before becoming ready`

Printed by `xcover wait` as soon as the daemon is no longer running
(`pkg/cmd/wait/wait.go`).

**Cause.** The daemon failed after start-up: symbol resolution, BPF load or
uprobe attach returned an error, so it exited instead of signalling readiness.

**Fix.** Read `/tmp/xcover.log` for the error and see the matching entry in
this page.

## `timeout waiting for profiler readiness`

Printed by `xcover wait` after `--timeout` (default 2 minutes) elapses
(`pkg/cmd/wait/wait.go`).

**Cause.** The daemon is alive but has not signalled readiness. Attaching
thousands of functions takes time, in particular in userspace BPF mode, which
attaches one uprobe per function. A daemon stuck before attach shows the same
symptom.

**Fix.** Raise the limit, for example `xcover wait --timeout 10m`, or narrow
the probe set with `--scope project`, `--include` or `--exclude`. Check
`/tmp/xcover.log` for progress or errors.

## `Daemon already running`

Printed by `xcover run --detach` (`pkg/cmd/run/run.go`). The command exits 0
and starts nothing.

**Cause.** `/tmp/xcover.pid` names a live process. Only one daemon can run
per host because the state paths are fixed.

**Fix.** Run `sudo xcover status` to see the PID, then `sudo xcover stop` to
end that session before starting a new one. If the PID belongs to an
unrelated process that reused the number, remove `/tmp/xcover.pid` by hand.

## `xcover not running or PID file not found`

Printed by `xcover stop` (`pkg/cmd/stop/stop.go`).

**Cause.** `/tmp/xcover.pid` cannot be read. The daemon already exited and
removed it, or it was never started. A PID file with unparsable content gives
`invalid PID file` instead.

**Fix.** Nothing to stop. If you expected a running daemon, read
`/tmp/xcover.log` to learn why it exited. Delete a corrupt PID file by hand.

## `xcover force killed (PID n), the coverage report may be missing`

Printed by `xcover stop` (`pkg/cmd/stop/stop.go`), which then exits with a
non-zero status.

**Cause.** The daemon did not exit within the grace period (`--timeout`,
default 30 seconds) after `SIGTERM`, so `stop` sent `SIGKILL`. A killed daemon
writes no report.

**Fix.** Expect `xcover-report.json` to be missing, or stale from a previous
run. Shutdown detaches the probes and drains the event pipeline before
writing the report, so a very large binary can need more time: raise the
limit with `xcover stop --timeout 2m`. Report a reproducible case in GitHub
issues.

## `path exists but is not a Unix socket: /tmp/xcover.sock`

Printed by `xcover wait` (`pkg/cmd/wait/wait.go`).

**Cause.** Something other than xcover created `/tmp/xcover.sock`.

**Fix.** Make sure no daemon is running (`sudo xcover status`), remove the
path and start again.

## `unknown scope "x": must be "binary" or "project"`

Printed by `xcover run` (`pkg/trace/scope.go`).

**Cause.** `--scope` accepts only `binary` and `project`.

**Fix.** Pass one of the two values, or omit the flag for `binary`.

## `invalid log level "x"`

Printed by every subcommand (`pkg/cmd/cmd.go`).

**Cause.** `--log-level` accepts `trace`, `debug`, `info`, `warn`, `error`,
`fatal` and `panic` only.

**Fix.** Pass one of those values.

## `xcover was not built with userspace BPF support; rebuild with -tags userspace`

Printed when `--userspace-bpf` is passed to the plain `xcover` binary
(`pkg/bpftime/bpftime_stub.go`), wrapped in
`failed to inject bpftime syscall-server`.

**Cause.** Userspace BPF mode needs the bpftime libraries embedded at build
time, which the default build does not include.

**Fix.** Build and use `xcover-userspace`: `make xcover-userspace`. See
[userspace-bpf.md](userspace-bpf.md).

## `project scope unavailable, falling back to binary scope`

A warning logged by `xcover run --scope project`
(`pkg/trace/resolver_go.go`). In `--detach` mode it appears only in
`/tmp/xcover.log`.

**Cause.** The binary carries no Go build information, was built as
`command-line-arguments` (for example `go build main.go`), or has no main
module path. Non-Go binaries always hit this. xcover then traces every
function in the binary.

**Fix.** Build the Go target as a package or module (`go build -o app .` or
`go build -o app ./cmd/app`). For non-Go binaries use `--include` and
`--exclude` instead. The report does not record which scope was used, so
check the log.

## `no functions found`

Printed by `xcover run`, wrapped in `failed to resolve functions` and
`failed to init tracer` (`pkg/trace/resolver.go`, `pkg/trace/tracee.go`).
The daemon exits and `xcover wait` reports `xcover is not running`.

**Cause.** No function passed the filters. The usual reason is an
`--include` pattern that matches nothing, or an `--exclude` pattern that
matches everything. With `--scope project`, a module whose functions were all
filtered out gives `no functions found for module "..."` instead.

**Fix.** Check the patterns against `nm --defined-only ./app | grep ' T '` or
`go tool nm ./app`. Patterns are Go (RE2) regular expressions matched against
the full symbol name, for example `^github.com/org/app/`.

## `cannot verify the debug file belongs to the executable` and other build-id errors

Printed by `xcover run --debug-path` (`pkg/trace/resolver_debug.go`). The
underlying errors are `executable or debug file has no GNU build-id` and
`debug file build-id does not match executable`.

**Cause.** xcover compares the GNU build-id note of `--path` and
`--debug-path` so probe offsets from one file are not paired with names from
another. One file has no note, or the two differ.

**Fix.** Use the debug file produced from the same link as the executable.
If your toolchain does not emit build-ids at all, pass
`--no-build-id-check`. Do not use it to silence a mismatch: the names would
not belong to the code being probed.

## `error initializing BPF probe` with `permission denied` or `operation not permitted`

Printed by `xcover run` (`pkg/trace/tracer.go`, `pkg/probe/probe.go`). The
process exits 1 before any probe is attached.

**Cause.** Loading a BPF program needs root, or `CAP_BPF` plus
`CAP_PERFMON`. Containers often drop these even for root.

**Fix.** Run `xcover run` with `sudo`, or grant the two capabilities. In a
container, add them explicitly or run privileged. Userspace BPF mode needs
neither; see [userspace-bpf.md](userspace-bpf.md).

## `error attaching probe: error attaching uprobe for functions with cookies: [...]`

Returned by `xcover run` when `uprobe_multi` refuses a batch of functions
(`pkg/probe/probe.go`). The run exits before signalling readiness and writes
no report; `xcover wait` returns `xcover exited before becoming ready`.

**Cause.** The kernel rejected the `uprobe_multi` link. The most common
reason is a kernel older than 6.6 without a distribution backport. The wrapped
libbpf error names the actual cause.

**Fix.** Check `uname -r` and upgrade to 6.6 or newer, or a kernel that
backports `uprobe_multi`. In `--detach` mode the error is in `/tmp/xcover.log`.

## `xcover-report.json` is missing after the session

**Cause.** The report is written when the daemon receives `SIGINT` or
`SIGTERM`, in the working directory of the `xcover run` invocation. A daemon
that was killed with `SIGKILL` (including by `xcover stop` after its grace
period, 30 seconds by default), that failed before attaching, or that was
started with `--report=false` writes nothing.

**Fix.** Look in the directory where `xcover run --detach` was executed, not
where `xcover stop` ran. Read `/tmp/xcover.log` for `failed to create report
file` or an earlier error. Stop with `xcover stop` or `Ctrl-C`, never
`kill -9`.

## Coverage is lower than expected

**Cause.** Several things lower `cov_by_func` without an error:

- Functions the compiler inlined have a symbol but no entry point, so their
  probe never fires.
- The kernel rejected a `seen_funcs` insert. The map is sized to the traced
  function count, so this is rare; when it happens `xcover run` logs a warning
  on exit with the number of dropped first hits (`bpf/trace.bpf.c`,
  `drops` map).
- `--pid` was set. The flag is parsed but not applied
  (`pkg/cmd/run/run.go`, `pkg/probe/probe.go`), so hits from every process
  running the binary are counted and the report does not describe a single
  process.

**Fix.** Read `/tmp/xcover.log`, then narrow the probe set with
`--scope project`, `--include` or `--exclude`. Compare `funcs_traced` with
`funcs_ack` to see which functions never fired.
