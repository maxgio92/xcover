# Userspace BPF mode (experimental)

xcover can optionally run BPF programs entirely in userspace via the
[bpftime](https://github.com/eunomia-bpf/bpftime) runtime, eliminating the
kernel trap cost on every traced function call.

> **Status:** this is an experimental PoC. Real-world performance numbers have
> not been collected yet.

## How it works

Two bpftime shared libraries are involved:

- **bpftime-syscall-server.so** - intercepts BPF syscalls inside xcover so
  that map and program management stays in userspace. xcover injects this into
  itself at startup via `memfd_create` and self re-exec.

- **bpftime-agent.so** - handles uprobe hits in userspace inside the tracee.
  xcover injects it automatically into the running tracee via the embedded
  bpftime CLI, which uses Frida Core's ptrace-based injection under the hood.
  No manual LD_PRELOAD step required.

## How agent injection works

xcover embeds the bpftime CLI binary and agent library. On first use, both are
extracted to `~/.cache/xcover/bpftime/`. When `--userspace-bpf` is set, xcover
watches `/proc` for a process running the binary at `--path`. Once the tracee
is detected, xcover runs:

```
bpftime -i ~/.cache/xcover/bpftime attach <pid>
```

This delegates Frida Core injection to bpftime's own CLI, which calls
`bpftime_agent_main` in the agent library after loading it. The `--path`
interface is unchanged; no PID flag is required.

## Requirements

- POSIX shared memory (`/dev/shm`) must be accessible and shared between
  xcover and the tracee. This is the coordination channel between the
  syscall-server (xcover side) and the agent (tracee side). Some container
  setups mount `/dev/shm` as private per-container; ensure both processes
  share the same shm namespace.
- `CAP_SYS_PTRACE` or same UID as the tracee. Frida Core uses ptrace to
  inject the agent. Docker's default seccomp profile blocks ptrace; it must
  be explicitly allowed (`--cap-add SYS_PTRACE` or `--security-opt
  seccomp=unconfined`).

## Prerequisites

Build the bpftime libraries and CLI binary and embed them into xcover:

```sh
make bpftime-libs
go build .
```

## Usage

**1. Start xcover in userspace BPF mode**

```sh
xcover run --path ./my-binary --userspace-bpf
```

xcover will block waiting for the tracee to appear.

**2. Run the tracee normally**

```sh
./my-binary
```

xcover detects the process, injects the bpftime agent automatically, and
begins tracing. Everything else (stopping, collecting the report) works the
same as in kernel mode.

## Limitations

- The `--userspace-bpf` flag triggers a self re-exec on first invocation.
  This is transparent in normal usage but may interact unexpectedly with
  process supervisors.
- There is a small race window between process start and injection. If the
  tracee executes target functions before xcover completes injection (within
  ~250ms of startup), those calls are missed. Acceptable for test suite
  workloads; documented in `pkg/procwatch`.

## Binary support

Frida Core injects the agent by locating `dlopen` in the tracee's address
space and calling it via ptrace. Whether a tracee is supported depends on
whether Frida can resolve `dlopen` and whether the agent's initialization
requirements are met.

**Supported:**
- Dynamically linked binaries (C, C++, Rust/glibc, CGO-enabled Go), stripped
  or not. `dlopen` lives in `libc.so` or `libdl.so`; its symbol is in
  `.dynsym`, which is structurally required for runtime dynamic linking and is
  never removed by `strip`.
- Statically linked glibc binaries, **unstripped only**. No `.dynsym` (no
  dynamic linking), but Frida falls back to scanning `.symtab`. `strip
  --strip-all` removes `.symtab`, so stripped static glibc binaries are not
  supported.

**Not supported:**
- Statically linked glibc binaries, stripped. `.dynsym` absent (no dynamic
  linking); `.symtab` removed by `strip`. No path to `dlopen`.
- Statically linked musl binaries (stripped or not). musl's static libc does
  not include `dlopen` - static is fully static by design.
- Pure Go binaries (`CGO_ENABLED=0`): no libc for Frida's bootstrapper to
  scan for `dlopen`, and Go's goroutine scheduler conflicts with the
  `pthread_create` calls the agent makes at initialization.

For unsupported binary types, fall back to kernel uprobe mode (default). This
is not a regression: kernel uprobes work on any ELF binary regardless of
linking and require no agent injection.

## Conclusion

The main tension is at tracee-level, not tracer level. The xcover tracer
controls its own startup: it embeds the bpftime syscall-server library and
re-execs itself with `LD_PRELOAD` pointing to it via `memfd_create`. The
challenge is the tracee, which we do not launch or control, and for which we
want the most transparent UX possible.

The ways we can integrate bpftime into the xcover's tracee binary is either via:

- **LD_PRELOAD**: the bpftime agent library is loaded at ld.so preload.
  _Cons_: it does not work for statically linked binaries, as it needs the
  dynamic linker (ld.so) to be loaded and run. In particular, Go binaries
  (`CGO_ENABLED=0`) as they do not link to any libc.

- **Frida/dlopen**: Frida mechanism uses ptrace to inject the `dlopen()` of the
  bpftime agent library. The agent library then initializes from the info in the
  shared memory and injects the trampolines at uprobe points - got from the
  shared memory - to the BPF JIT-compiled program.
  _Pros_: it works for dynamically (stripped or not) and non-stripped glibc
  statically linked binaries.
  _Cons_: see the Binary support section for the full matrix of unsupported
  binary types and the reasons behind each.
