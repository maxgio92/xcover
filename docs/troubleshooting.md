# Troubleshooting

Each entry lists the message you see, why it happens and what to do. Messages
are quoted from the source; the file that prints them is named so you can
check the current wording. In `--detach` mode the daemon's own output goes to
`/tmp/xcover.log`, so read that file when a command prints nothing useful.

## `xcover is not running`

Printed by `xcover wait` (`pkg/cmd/wait`).

**Cause.** `/tmp/xcover.pid` is missing, or the PID it names is not alive.
Either `xcover run --detach` was never started, or the daemon exited before
`wait` ran, most often because attach or BPF load failed.

**Fix.** Run `sudo xcover status`. If it reports not running, read
`/tmp/xcover.log` for the daemon's error and fix that first.

## `timeout waiting for profiler readiness`

Printed by `xcover wait` after `--timeout` (default 2 minutes) elapses
(`pkg/cmd/wait`).

**Cause.** The daemon is alive but has not signalled readiness. Attaching
thousands of functions takes time, in particular in userspace BPF mode, which
attaches one uprobe per function. A daemon stuck before attach also shows
this symptom.

**Fix.** Raise the limit, for example `xcover wait --timeout 10m`, or narrow
the probe set with `--scope project`, `--include` or `--exclude`. Check
`/tmp/xcover.log` for progress or errors.

## `Daemon already running`

Printed by `xcover run --detach` (`pkg/cmd/run`). The command exits 0 and
starts nothing.

**Cause.** `/tmp/xcover.pid` names a live process. Only one daemon can run
per host because the state paths are fixed.

**Fix.** Run `sudo xcover status` to see the PID, then `sudo xcover stop` to
end that session before starting a new one. If the PID belongs to an
unrelated process that reused the number, remove `/tmp/xcover.pid` by hand.

## `xcover not running or PID file not found`

Printed by `xcover stop` (`pkg/cmd/stop`).

**Cause.** `/tmp/xcover.pid` cannot be read. The daemon already exited and
removed it, or it was never started.

**Fix.** Nothing to stop. If you expected a running daemon, read
`/tmp/xcover.log` to learn why it exited.

## `xcover force killed (PID n)`

Printed by `xcover stop` (`pkg/cmd/stop`).

**Cause.** The daemon did not exit within 5 seconds of `SIGTERM`, so `stop`
sent `SIGKILL`. A daemon that is killed writes no report.

**Fix.** Expect `xcover-report.json` to be missing or stale from a previous
run. Shutdown drains the event pipeline before writing the report; a very
busy tracee at stop time makes this slower. Stop the tracee first, then stop
xcover. Report a reproducible case in GitHub issues.

## `path exists but is not a Unix socket: /tmp/xcover.sock`

Printed by `xcover wait` (`pkg/cmd/wait`).

**Cause.** Something other than xcover created `/tmp/xcover.sock`, or the
daemon replaced it with a regular file.

**Fix.** Make sure no daemon is running (`sudo xcover status`), remove the
path and start again.

## `--pid must be a positive PID up to 2147483647 or -1, got n`

Printed by `xcover run` (`pkg/cmd/run`).

**Cause.** `--pid` was 0, below -1 or above the largest 32-bit value. libbpf
maps 0 to xcover's own process and treats every negative value as "all
processes", so xcover rejects anything but a real PID or -1.

**Fix.** Pass the PID of a running process, or omit the flag to trace every
process that runs the executable.

## `xcover was not built with userspace BPF support; rebuild with -tags userspace`

Printed when `--userspace-bpf` is passed to the plain `xcover` binary
(`pkg/bpftime/bpftime_stub.go`).

**Cause.** Userspace BPF mode needs the bpftime libraries embedded at build
time, which the default build does not include.

**Fix.** Build and use `xcover-userspace`: `make xcover-userspace`. See
[userspace-bpf.md](userspace-bpf.md).

## `project scope unavailable, falling back to binary scope`

A warning logged by `xcover run --scope project` (`pkg/trace/resolver_go.go`).
In `--detach` mode it appears only in `/tmp/xcover.log`.

**Cause.** The binary carries no Go build information, was built as
`command-line-arguments` (for example `go build main.go`), or has no main
module path. Non-Go binaries always hit this.

**Fix.** Build the Go target as a package or module (`go build -o app .` or
`go build -o app ./cmd/app`). For non-Go binaries use `--include` and
`--exclude` instead. The report does not record which scope was used, so
check the log.

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

## `calls not recorded because the seen_funcs map is full`

A warning logged on exit (`pkg/trace/tracer.go`), with the number of dropped
first hits.

**Cause.** The BPF hash map that deduplicates functions holds 40960 entries.
Once it is full the first hit of any further function is counted as a drop
instead of an event, so `funcs_ack` and `cov_by_func` undercount.

**Fix.** Narrow the probe set with `--scope project`, `--include` or
`--exclude` so fewer than 40960 functions are traced. In userspace BPF mode
this warning is not produced; see the limitations in
[userspace-bpf.md](userspace-bpf.md).

## `permission denied` or `operation not permitted` while loading or attaching

Reported inside `error initializing BPF probe` or `error attaching probes`
(`pkg/trace/tracer.go`, `pkg/probe/probe.go`).

**Cause.** Loading a BPF program and attaching uprobes need root, or
`CAP_BPF` plus `CAP_PERFMON`. Containers often drop these even for root.

**Fix.** Run `xcover run` with `sudo`, or grant the two capabilities. In a
container, add them explicitly or run privileged. Userspace BPF mode needs
neither; see [userspace-bpf.md](userspace-bpf.md).

## Attach fails on an older kernel

`xcover run` exits with `error attaching probes: ... attach uprobe_multi for
functions with cookies [...]`. libbpf may also log `failed to attach
multi-uprobe`.

**Cause.** xcover attaches with `uprobe_multi` links, which need Linux 6.6 or
newer. Older kernels reject the link.

**Fix.** Upgrade the kernel. Check with `uname -r`.

## `xcover-report.json` is missing after the session

**Cause.** The report is written on `SIGINT` or `SIGTERM`, in the working
directory of the `xcover run` invocation. A daemon that was killed with
`SIGKILL` (including by `xcover stop` after its 5 second grace period), that
failed to attach, or that was started with `--report=false` writes nothing.
An attach failure also means `xcover wait` never returns ready.

**Fix.** Look in the directory where `xcover run --detach` was executed, not
where `xcover stop` ran. Read `/tmp/xcover.log` for `failed to create report
file` or an attach error. Stop with `xcover stop` or `Ctrl-C`, never `kill -9`.
