# Userspace BPF mode (experimental)

xcover can run its BPF program in userspace through the
[bpftime](https://github.com/eunomia-bpf/bpftime) runtime instead of the
kernel. Every probed call then stays in the tracee's process and skips the
kernel trap.

> **Status:** experimental. Check the
> [release notes](https://github.com/maxgio92/xcover/releases) for the first
> release that includes it. The [benchmark](../benchmark/README.md) measures
> about 65% lower per-call cost on the already-seen path and 63% on the
> first-hit path compared to kernel uprobes (one machine, 100 probes, 10
> rounds). It also runs without `CAP_BPF`.

## How it works

xcover preloads a bpftime library into itself so BPF syscalls are served in
userspace, and the tracee must preload a second bpftime library that handles
uprobe hits. The mechanism is described in
[architecture.md](architecture.md#userspace-bpf-mode).

## Requirements

- The tracee must be dynamically linked against glibc. `LD_PRELOAD` is
  processed by the dynamic linker (`ld.so`); a static binary has none and
  ignores it. See [Binary support](#binary-support).
- POSIX shared memory (`/dev/shm`) must be shared between xcover and the
  tracee. It is the channel between the two libraries. Some container setups
  give each container a private `/dev/shm`; run both processes in the same
  one.

## Prerequisites

Build the dedicated binary. The target clones the pinned bpftime commit,
applies `patches/bpftime/*.patch`, builds the two shared libraries, copies
them to `pkg/bpftime/libs/` and compiles xcover with `-tags userspace`:

```sh
make xcover-userspace
```

It needs cmake 3.16+, a C++17 compiler, libelf, zlib and LLVM 18. The plain
`xcover` binary accepts `--userspace-bpf` but fails at startup with
`xcover was not built with userspace BPF support`; use `xcover-userspace`.

The examples below assume `xcover-userspace` is on your `PATH` as `xcover`.

## Usage

**1. Start xcover in userspace BPF mode**

```sh
xcover run --detach --path ./my-binary --userspace-bpf
xcover wait
```

**2. Extract the agent library**

```sh
export XCOVER_AGENT=$(xcover agent extract)
```

`agent extract` writes the embedded agent to a temporary file
(`/tmp/bpftime-agent-*.so`) and prints the path. The file is not removed
automatically.

**3. Run the tracee with the agent preloaded**

```sh
LD_PRELOAD=$XCOVER_AGENT ./my-binary test-1
LD_PRELOAD=$XCOVER_AGENT ./my-binary test-2
```

**4. Stop and read the report**

```sh
xcover stop
jq .cov_by_func xcover-report.json
```

Waiting for readiness, stopping and the report format are the same as in
kernel mode. The two demos in `demo/userspace/` and `demo/userspace-stripped/`
script this flow; see [demo/README.md](../demo/README.md).

## Limitations

- **Not transparent.** You must set `LD_PRELOAD` on the tracee's command
  line. xcover does not launch the tracee and cannot inject the agent for you.
- **Startup-only injection.** `ld.so` reads `LD_PRELOAD` when a process
  starts. A process that is already running cannot be traced.
- **Dynamically linked glibc binaries only.** See
  [Binary support](#binary-support).
- **Self re-exec.** With `--userspace-bpf`, xcover re-executes itself once
  with the syscall server preloaded and `XCOVER_BPFTIME_LOADED=1` set. This
  is invisible in normal use but can confuse process supervisors.
- **One uprobe per function.** bpftime does not implement `uprobe_multi`, so
  xcover attaches one perf-event uprobe per function. Attaching thousands of
  functions is slower than in kernel mode and consumes bpftime handler slots;
  the benchmark raises `BPFTIME_MAX_FD_COUNT` for this reason. A failed
  attach skips the rest of its batch of 128 functions, is logged as a warning
  and does not stop the session.

## Binary support

`ld.so` processes `LD_PRELOAD` before `main()`. Whether a tracee is supported
depends only on whether it uses the glibc dynamic linker.

**Supported:**

- Dynamically linked binaries (C, C++, Rust with glibc, cgo-enabled Go),
  stripped or not. Debug symbols are irrelevant to agent loading.

**Not supported:**

- Statically linked binaries (any libc, any language). No dynamic linker
  means `LD_PRELOAD` is ignored and the agent never loads.
- Pure Go binaries (`CGO_ENABLED=0`). Go's internal linker produces a static
  binary with no `ld.so` dependency.
- musl-linked binaries (for example Alpine). The shipped agent is built
  against glibc and musl's loader fails to relocate it
  (`__libc_single_threaded`, `getcontext` and other glibc-only symbols are
  missing). An agent built against musl would lift this.
- setuid or setgid binaries. The dynamic linker ignores `LD_PRELOAD` for
  privileged executables (secure-execution mode).

For unsupported binaries use the default kernel uprobe mode. It works on any
ELF binary regardless of linking and needs no agent.
