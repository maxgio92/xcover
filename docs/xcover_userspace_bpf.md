# Userspace BPF mode (experimental)

xcover can optionally run BPF programs entirely in userspace via the
[bpftime](https://github.com/eunomia-bpf/bpftime) runtime, eliminating the
kernel trap cost on every traced function call.

> **Status:** experimental. Benchmarks (see `benchmark/`) measure a ~65% lower
> per-call overhead on the hit path and ~63% on the miss path compared to
> kernel uprobes.

## How it works

Two bpftime shared libraries are involved:

- **bpftime-syscall-server.so** - intercepts BPF syscalls inside xcover so
  that map and program management stays in userspace. xcover injects this into
  itself at startup via `memfd_create` and self re-exec.

- **bpftime-agent.so** - handles uprobe hits in userspace inside the tracee.
  Must be preloaded manually by the user before the tracee starts (see Usage).
  For automatic injection see the `xcover-userspace-dlopen` branch.

## Requirements

- Tracee must be dynamically linked. `LD_PRELOAD` is processed by the dynamic
  linker (`ld.so`); statically linked binaries have no dynamic linker and
  silently ignore it.
- POSIX shared memory (`/dev/shm`) must be accessible and shared between
  xcover and the tracee. This is the coordination channel between the
  syscall-server (xcover side) and the agent (tracee side). Some container
  setups mount `/dev/shm` as private per-container; ensure both processes
  share the same shm namespace.

## Prerequisites

Build the bpftime libraries and embed them into xcover:

```sh
make bpftime-libs
go build .
```

## Usage

**1. Extract the agent library**

```sh
export LD_PRELOAD=$(xcover agent extract)
```

**2. Run the tracee with the agent preloaded**

```sh
LD_PRELOAD=$LD_PRELOAD ./my-binary &
```

**3. Run xcover in userspace BPF mode**

```sh
xcover run --path ./my-binary --userspace-bpf
```

Everything else (waiting for readiness, stopping, collecting the report) works
the same as in kernel mode.

## Limitations

The xcover tracer controls its own startup: it embeds the bpftime
syscall-server library and re-execs itself with `LD_PRELOAD` pointing to it
via `memfd_create`. The challenge is the tracee, which we do not launch or
control, and for which we want the most transparent UX possible.

- **Not transparent.** The user must set `LD_PRELOAD` before launching the
  tracee. This requires modifying the launch command and leaks xcover
  internals to the consumer.
- **Startup-only injection.** `LD_PRELOAD` is processed by `ld.so` at process
  startup; it cannot be used to inject into an already-running process.
- **Dynamically linked binaries only.** See Requirements and the Binary
  support section.
- **Self re-exec on first invocation.** The `--userspace-bpf` flag causes
  xcover to re-exec itself with the syscall-server preloaded. Transparent in
  normal usage but may interact unexpectedly with process supervisors.

## Binary support

`LD_PRELOAD` is processed by the dynamic linker (`ld.so`) before `main()` is
called. Whether a tracee binary is supported depends entirely on whether it
uses a dynamic linker.

**Supported:**
- Dynamically linked binaries (C, C++, Rust/glibc, CGO-enabled Go), stripped
  or not. `ld.so` loads the agent before `main()` regardless of whether debug
  symbols are present.

**Not supported:**
- Statically linked binaries (any libc, any language). No dynamic linker means
  `LD_PRELOAD` is silently ignored and the agent is never loaded.
- Pure Go binaries (`CGO_ENABLED=0`): same reason - Go's internal linker
  produces a static binary with no `ld.so` dependency.
- musl-linked binaries (e.g. Alpine). The shipped agent is built against
  glibc and musl's loader fails to relocate it (`__libc_single_threaded`,
  `getcontext` and other glibc-only symbols are missing). An agent built
  against musl would lift this.
- setuid/setgid binaries. The dynamic linker ignores `LD_PRELOAD` for
  privileged executables (secure-execution mode).

For unsupported binary types, fall back to kernel uprobe mode (default). This
is not a regression: kernel uprobes work on any ELF binary regardless of
linking and require no agent injection.


