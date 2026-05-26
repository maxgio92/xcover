---
marp: true
theme: default
paginate: true
style: |
  section {
    background: #ffffff;
    color: #1a1a1a;
    font-family: system-ui, -apple-system, BlinkMacSystemFont, sans-serif;
    font-size: 1.35rem;
    padding: 56px 80px;
  }

  /* Section break slides */
  section.break {
    background: #0d1117;
    color: #ffffff;
    text-align: center;
    justify-content: center;
    align-items: center;
    display: flex;
    flex-direction: column;
  }
  section.break h1 {
    font-size: 3rem;
    color: #ffffff;
    border: none;
  }
  section.break p {
    color: #8b949e;
    font-size: 1.1rem;
    margin-top: 0.4em;
  }

  /* Title slide */
  section.title {
    justify-content: flex-end;
    padding-bottom: 80px;
  }
  section.title h1 {
    font-size: 2.6rem;
    line-height: 1.2;
    border: none;
    margin-bottom: 0.3em;
  }
  section.title p {
    color: #555;
    font-size: 1rem;
  }

  /* Statement slides */
  section.statement {
    justify-content: center;
    align-items: flex-start;
  }
  section.statement h1 {
    border: none;
    font-size: 2.4rem;
    line-height: 1.3;
    color: #1a1a1a;
  }

  /* Regular slides */
  h1 {
    font-size: 1.8rem;
    color: #1a1a1a;
    border-bottom: 2px solid #e8e8e8;
    padding-bottom: 0.3em;
    margin-bottom: 0.6em;
  }
  h2 {
    font-size: 1.4rem;
    color: #444;
  }
  ul {
    margin-top: 0.4em;
    padding-left: 1.4em;
  }
  li {
    margin: 0.45em 0;
    line-height: 1.5;
  }
  strong {
    color: #1a1a1a;
  }
  blockquote {
    border-left: 3px solid #d0d0d0;
    padding-left: 1em;
    color: #444;
    margin: 0.8em 0;
    font-style: italic;
  }
  table {
    font-size: 0.9em;
    width: 100%;
    border-collapse: collapse;
  }
  th {
    background: #f6f6f6;
    border-bottom: 2px solid #ddd;
    padding: 0.5em 0.8em;
    text-align: left;
  }
  td {
    padding: 0.45em 0.8em;
    border-bottom: 1px solid #eee;
  }

  /* Code blocks - github-dark style */
  pre {
    background: #0d1117;
    border-radius: 8px;
    padding: 1em 1.2em;
    margin: 0.6em 0;
    overflow: hidden;
  }
  pre code {
    color: #e6edf3;
    font-size: 0.78em;
    line-height: 1.6;
    background: none;
    padding: 0;
    border-radius: 0;
  }
  code {
    background: #f0f0f0;
    padding: 0.1em 0.35em;
    border-radius: 4px;
    font-size: 0.85em;
  }

  /* Page number */
  section::after {
    font-size: 0.6rem;
    color: #bbb;
    content: attr(data-marpit-pagination) ' / ' attr(data-marpit-pagination-total);
  }
---

<!-- _class: title -->

# The Binary You Ship<br>Is the Binary You Test

Massimiliano · OpenSouthCode 2026

<!--
Welcome. Quick show of hands: how many of you run coverage as part of CI?
Now keep your hand up if you're running it on the exact same binary that goes to production.
That's the gap we're here to talk about.
-->

---

# Who is this talk for?

- You ship compiled software (Go, C, C++, Rust...)
- You care about test coverage as a quality signal
- You've felt the pain of maintaining separate instrumented builds
- Or you just think eBPF is interesting

<!--
I'm not going to try to convince you that coverage matters. You're here, so you already know.
What I want to talk about is a different way of getting it — one that doesn't require you to change how you build.
-->

---

<!-- _class: statement -->

# The standard coverage story

<!--
Let's start from the beginning. How does coverage work today?
-->

---

# Coverage today: the build-time approach

```sh
# Go
go test -coverprofile=coverage.out ./...

# C/C++ with GCC
gcc -fprofile-arcs -ftest-coverage -o myapp src/*.c

# LLVM source-based
clang -fprofile-instr-generate -fcoverage-mapping -o myapp src/*.c
```

- Instrumentation is **injected at compile time**
- Produces a separate artifact: the instrumented build
- Run tests against that artifact, collect the report

<!--
This is the happy path. For a single project with a single language, it works well.
The toolchain does the heavy lifting, and you get line-level or branch-level coverage.
-->

---

# The problem at scale

- You maintain **hundreds of packages**
- Multiple languages: Go, C, C++, Rust, Python extensions...
- Each language has its own tool, its own flags, its own report format
- None of them work together

<!--
At Chainguard we maintain a large corpus of packages. The moment you try to apply coverage uniformly across that, the operational cost becomes significant.
But the tooling fragmentation isn't even the biggest problem.
-->

---

# The doubled build matrix

```
package-foo/
  build/
    package-foo-1.0.0.tar.gz          ← what you ship
    package-foo-1.0.0-instrumented/   ← what you test
```

- Every package needs two build targets
- Doubled CI time, doubled storage, doubled maintenance
- The instrumented build diverges from production over time
- **You're not testing what you ship**

<!--
This is the core of the problem. The artifact you test is not the artifact you ship.
That gap is real — compiler optimizations, link order, environment differences.
The instrumented build gives you coverage numbers that may not reflect what actually runs in production.
-->

---

# Production binaries are stripped

```sh
$ file /usr/bin/containerd
/usr/bin/containerd: ELF 64-bit LSB executable, x86-64, stripped

$ nm /usr/bin/containerd
nm: /usr/bin/containerd: no symbols
```

- Stripped binaries: smaller, faster, no debug info leaking
- `.symtab` removed — no symbol names, no function list
- Instrumentation-based tools **require symbols baked in at build time**
- You can't instrument a binary you've already shipped

<!--
This is the second blocker. Even if you were willing to maintain a separate instrumented build, you can't apply it retroactively to a stripped production binary.
The data that coverage tools need just isn't there.
-->

---

<!-- _class: statement -->

# What if coverage didn't require<br>a separate build at all?

<!--
What if the tool could attach to the binary you already built, tested, packaged, and shipped?
To answer that, we need to talk about what the kernel can see.
-->

---

# The kernel sees everything

- **uprobes**: Linux kernel mechanism to intercept userspace function calls
- Available since kernel 3.5
- Attach to a function by **binary path + offset**
- No source changes. No build flags. No recompilation.
- Works on **any ELF binary** — Go, C, C++, Rust...

<!--
uprobes are a kernel feature that lets you intercept the entry point of any function in any userspace process.
They work by patching the first byte of the function with an INT3 (breakpoint) instruction.
When the CPU hits it, the kernel takes over, runs your handler, then resumes execution.
-->

---

# What is a uprobe?

```
Binary on disk:
  offset 0x1a40: PUSH RBP        ← function entry

With uprobe attached:
  offset 0x1a40: INT3            ← kernel patches this
                 (original bytes saved)

At runtime, on each call:
  1. CPU hits INT3
  2. Kernel trap handler fires
  3. BPF program runs (map update, bookkeeping)
  4. Original instruction restored, execution continues
```

<!--
The key point: the offset of a function in the binary is fixed. Strip all the symbols you want — the function is still at that address.
If you know the offset, you can attach a uprobe. You don't need the symbol table.
-->

---

# eBPF makes uprobes programmable

- **eBPF**: run sandboxed programs in the kernel, triggered by events
- uprobe fires → eBPF program runs → write to a map → userspace reads the map
- Programs are **verifier-checked**: no crashes, bounded execution, safe
- No kernel modules. No patching. Just load and go.

```
userspace (xcover)          kernel
      │                        │
      │── load BPF program ──▶ │  (verifier checks it)
      │── attach uprobe ─────▶ │  function entry at offset 0x1a40
      │                        │
      │        [test runs]     │
      │                        │
      │◀── read BPF map ──────  │  which functions fired
```

<!--
The BPF verifier is the safety net. It proves, statically, that your program terminates and doesn't crash the kernel.
This is why uprobe-based tools can be run in production — the kernel guarantees they're safe.
-->

---

# Introducing xcover

> The binary you ship is the binary you test.

- eBPF uprobe-based coverage profiler for compiled binaries
- Runs as a **daemon** alongside your test suite
- Attaches to the binary at its path — no process management
- Reports function-level coverage when you stop it
- Ships as a **single static binary**, zero runtime dependencies

`github.com/maxgio92/xcover`

<!--
xcover is the concrete implementation of this idea.
You point it at a binary, it attaches uprobes to every function it finds, and it counts which ones get called during your test run.
-->

---

<!-- _class: statement -->

# xcover in practice

<!--
Let me show you what this actually looks like.
-->

---

# Architecture

```
┌─────────────────────────────────────────────────────────┐
│  xcover (daemon)                                        │
│  ┌─────────────┐    ┌──────────────┐                   │
│  │ UserTracee  │    │  UserTracer  │                   │
│  │             │    │              │                   │
│  │ resolve     │───▶│ load BPF     │                   │
│  │ functions   │    │ attach probes│                   │
│  │ (resurgo)   │    │ read events  │                   │
│  └─────────────┘    └──────┬───────┘                   │
└─────────────────────────── │ ──────────────────────────┘
                             │ BPF map
┌─────────────────────────── │ ──────────────────────────┐
│  kernel                    │                            │
│              uprobes ◀─────┘                           │
└─────────────────────────────────────────────────────────┘
                    ↕ calls traced functions
┌─────────────────────────────────────────────────────────┐
│  your binary (tracee)   ← runs independently           │
└─────────────────────────────────────────────────────────┘
```

<!--
xcover is two logical pieces: UserTracee resolves the function list (using resurgo for stripped binaries), and UserTracer loads the BPF program, attaches uprobes, and drains the event map.
The tracee — your binary under test — runs completely independently. xcover just observes it.
-->

---

# The tracer API

```go
tracee := trace.NewUserTracee(
    trace.WithTraceeExePath("./myapp"),
    trace.WithTraceeSymPatternInclude(`^github\.com/myorg`),
    trace.WithTraceeSymPatternExclude(`_test$`),
)

tracer := trace.NewUserTracer(
    trace.WithTracerLogger(logger),
    trace.WithTracerReport(true),
    trace.WithTracerTracee(tracee),
)

if err := tracer.Init(ctx); err != nil { ... }
if err := tracer.Run(ctx); err != nil { ... }
```

<!--
This is the Go API, if you want to embed xcover in your own tooling.
But most people will just use the CLI.
-->

---

# The CLI workflow

```sh
# Start the profiler (detach to background)
xcover run --path ./myapp --detach

# Wait until all probes are attached (ready to trace)
xcover wait

# Run your tests — nothing changes here
go test ./...

# Stop the profiler and collect the report
xcover stop
```

<!--
Notice: `go test` is completely unchanged. No flags, no environment variables, no wrapper.
xcover is watching from the side, not in the middle.
-->

---

# Filtering functions

```sh
# Only trace functions in your own packages
xcover run --path ./myapp \
  --include '^github\.com/myorg/myapp' \
  --detach

# Exclude generated code
xcover run --path ./myapp \
  --include '^github\.com/myorg' \
  --exclude '\.pb\.go' \
  --detach
```

- `--include` and `--exclude` take Go regexes
- Applied to function names at probe-attachment time
- Fewer probes = lower overhead

<!--
Without filtering you'd be tracing every function in the binary, including the Go runtime and all stdlib functions.
Filtering to your own packages is the recommended default.
-->

---

# The coverage report

```json
{
  "exe_path": "./myapp",
  "funcs_traced": [
    "github.com/myorg/myapp/pkg/server.HandleRequest",
    "github.com/myorg/myapp/pkg/server.parseConfig",
    "github.com/myorg/myapp/pkg/db.Connect",
    ...
  ],
  "funcs_ack": [
    "github.com/myorg/myapp/pkg/server.HandleRequest",
    "github.com/myorg/myapp/pkg/db.Connect"
  ],
  "cov_by_func": 0.72
}
```

- `funcs_traced` — all functions with probes attached
- `funcs_ack` — functions called at least once during the run
- `cov_by_func` — ratio: 72% of functions were exercised

<!--
Language-agnostic JSON. Parse it in CI, diff it across runs, set a coverage gate.
Same format whether you're testing Go, C, or Rust.
-->

---

# Daemon mode: across multiple test runs

```sh
# Start once
xcover run --path ./myapp --detach
xcover wait

# Run your test suite in parallel
go test ./pkg/server/... &
go test ./pkg/db/... &
wait

# One combined report for everything
xcover stop
```

- xcover accumulates events across all runs until you stop it
- Useful for splitting a test suite across multiple processes
- Coverage is union of all functions called in any run

<!--
This is where the daemon model really shines. You can run your test suite in parallel, across multiple processes, and get one combined coverage report.
A per-process coverage tool can't do this cleanly.
-->

---

<!-- _class: break -->

# Demo 🎬

<!--
[Live demo or asciinema]
Show: xcover run --path ./demo/demo --detach
      xcover wait
      ./demo/demo (run tests)
      xcover stop
      cat xcover-report.json
Optionally: show --include filtering, then show the difference in funcs_traced count.
-->

---

<!-- _class: statement -->

# One catch:<br>stripped binaries

<!--
We said uprobes work on any ELF binary. That's true.
But there's a problem: xcover still needs to know *where* the functions are to attach probes.
-->

---

# What strip actually removes

```sh
$ ls -lh myapp myapp-stripped
-rwxr-xr-x 1 user user 14M myapp
-rwxr-xr-x 1 user user  6M myapp-stripped

$ readelf -S myapp | grep -E '\.symtab|\.debug'
  [32] .symtab           SYMTAB  ...  ← function names + offsets
  [33] .strtab           STRTAB  ...  ← symbol name strings
  [34] .debug_info       PROGBITS ...
  [35] .debug_line       PROGBITS ...

$ readelf -S myapp-stripped | grep -E '\.symtab|\.debug'
  (nothing)
```

- `strip` removes `.symtab`, `.strtab`, and all `.debug_*` sections
- **The code is still there.** Only the metadata is gone.
- Function entry points are at fixed offsets — the compiler put them there

<!--
Strip doesn't touch the executable code. It just removes the table of contents.
The functions are still at the same addresses. We just need another way to find them.
-->

---

# What strip does NOT remove

```sh
$ readelf -S myapp-stripped | grep '\.eh_frame'
  [17] .eh_frame_hdr     PROGBITS ...
  [18] .eh_frame         PROGBITS ...
```

- `.eh_frame` — DWARF Call Frame Information, needed for stack unwinding
- Survives `strip --strip-all` because the **C++ runtime needs it**
- Encodes function boundaries and stack layout
- The compiler always emits it. The linker always keeps it.

<!--
This is the key insight that makes resurgo work.
The compiler has to emit .eh_frame for exception handling and stack unwinding to work.
It can't be stripped without breaking the binary.
And it encodes exactly what we need: where functions start and end.
-->

---

# Introducing resurgo

Static function recovery for stripped ELF binaries.

Four complementary signals:

1. **DWARF CFI** (`.eh_frame`) — function ranges, survives strip, high confidence
2. **Prologue patterns** — `push rbp; mov rbp, rsp` at function entry, architecture-specific
3. **Call-site analysis** — `call` targets are function entry points by definition
4. **Alignment boundaries** — compilers align functions to 16/32-byte boundaries

Cross-validated: a candidate confirmed by 3+ signals is a function.

`github.com/maxgio92/resurgo`

<!--
No single signal is perfect. .eh_frame is the most reliable but can miss inlined functions.
Prologue patterns have false positives. Call-site analysis misses leaf functions that are never called directly.
Combined, they give you a high-confidence function list.
-->

---

# resurgo: the recovery pipeline

```
ELF binary (stripped)
       │
       ├── parse .eh_frame ──────▶ function ranges (high confidence)
       │
       ├── scan for prologues ───▶ entry candidates (medium confidence)
       │
       ├── walk call graph ──────▶ call targets (medium confidence)
       │
       └── check alignment ──────▶ alignment candidates (low confidence)
                │
                ▼
         cross-validate
                │
                ▼
         function list + offsets  ──▶  xcover attaches uprobes
```

<!--
The output is a list of function entry offsets with confidence scores.
xcover uses this list exactly the same way it would use .symtab — attach a uprobe at each offset.
From the user's perspective, nothing changes.
-->

---

# Transparent to the user

```sh
# Unstripped binary
$ xcover run --path ./myapp --detach
INFO  resolved 1842 functions via .symtab

# Stripped binary — same command
$ xcover run --path ./myapp-stripped --detach
INFO  .symtab not found, falling back to static recovery
INFO  resolved 1791 functions via resurgo (eh_frame + prologue analysis)
```

- xcover detects the binary is stripped and calls resurgo automatically
- No flags, no config change, no separate step
- Coverage report is identical in format

<!--
The 51-function difference is expected — resurgo may miss some functions that are heavily inlined or have unusual prologues.
For coverage purposes, this is an acceptable tradeoff compared to maintaining a separate instrumented build.
-->

---

<!-- _class: break -->

# Benchmark time 📊

<!--
We've claimed this works. Now let's talk about whether it's fast enough.
This is important: we're not trying to hide the cost. We measured it, and the data is what closes the argument.
-->

---

# The overhead model

Every call to a traced function pays:

```
function called
     │
     ▼
  INT3 trap ──▶ kernel mode  (~X ns)
                     │
                     ▼
              BPF program runs        (~X ns)
              (map update, cookie lookup)
                     │
                     ▼
              return to userspace     (~X ns)
                     │
                     ▼
function body executes
```

- **Fixed cost per call** — independent of function body size
- Overhead scales with **call frequency**, not binary size or number of functions

<!--
The dominant cost is the kernel mode transition — the INT3 trap and return.
The BPF program itself is small and fast. The map operations are O(1).
What matters is how often your functions are called.
-->

---

# What we measured

Setup:
- Binary: `[placeholder — describe test binary]`
- Functions traced: `[N]`
- Test suite: `[describe]`
- Machine: `[CPU, Linux kernel version]`

Metrics:
- Per-call overhead (hot path, no filtering)
- Wall clock time: instrumented run vs. non-instrumented run
- Memory overhead (BPF maps)

<!--
Replace these placeholders with your actual benchmark setup.
Describing the setup is as important as the numbers — the audience needs to know whether this benchmark resembles their workload.
-->

---

# Per-call overhead

| Scenario | Overhead per call |
|---|---|
| No tracing (baseline) | 0 ns |
| uprobe attached, not firing | ~X ns |
| uprobe firing, BPF running | ~X ns |
| uprobe firing, map full | ~X ns |

- **Overhead floor**: ~X ns per call even when not hit (kernel patch is in place)
- **Typical case**: ~X ns per call end-to-end
- Comparable to: `[relatable comparison — e.g. one syscall, one cache miss]`

<!--
The "attached, not firing" row matters: once you attach a uprobe, even functions that aren't called pay a small cost on the way past the INT3.
For filtered workloads (only your own packages), this is usually negligible.
-->

---

# Aggregate overhead: full test suite

| | Wall time | Delta |
|---|---|---|
| Baseline (no tracing) | X.Xs | — |
| xcover, full binary | X.Xs | +X% |
| xcover, filtered (own pkgs) | X.Xs | +X% |

> **+X% wall time for full test coverage on the exact production binary.**

- Filtering to your own packages reduces overhead by ~X%
- Overhead is deterministic — no jitter, no GC interaction
- Acceptable for CI pipelines; not for latency-sensitive benchmarks

<!--
[Replace with real numbers]
The key message: for a test suite that runs in minutes, a small percentage overhead is a reasonable tradeoff.
You're not paying this in production — only during test runs.
-->

---

# Memory: BPF maps

```
Map: per-function event counter
  Key size:   8 bytes (cookie / function ID)
  Value size: 8 bytes (uint64 hit count)
  Max entries: N (one per traced function)

Total: N × 16 bytes
```

- 1000 functions → 16 KB
- 10,000 functions → 160 KB
- **Negligible** compared to typical process memory

<!--
BPF maps are pre-allocated. The memory cost is fixed at attach time.
Even a large binary with 50k functions would use less than 1 MB of BPF map memory.
-->

---

# The tradeoff

|  | Instrumented build | xcover |
|---|---|---|
| Build changes required | Yes | **No** |
| Works on stripped binaries | No | **Yes** |
| Cross-language | No | **Yes** |
| Same binary as production | No | **Yes** |
| Per-call overhead | ~0 | ~X ns |
| Setup complexity | Per-language | Once |

> You pay a small, predictable cost per function call.<br>In exchange: no instrumented builds, one tool, any binary.

<!--
The overhead is real. We didn't hide it. But look at what you get in return.
You test the exact binary that runs in production, with no changes to your build, in any language.
For coverage — a quality signal, not a latency-critical path — this tradeoff is worth it.
-->

---

<!-- _class: statement -->

# What's next:<br>eliminate the kernel trap

<!--
The overhead comes from the kernel mode transition on every uprobe hit.
What if we could eliminate that entirely?
-->

---

# The cost is in the trap

```
Current: kernel uprobe
  function call → INT3 → kernel trap → BPF runs → return to userspace

Hypothesis: userspace BPF
  function call → intercept in-process → BPF runs → continue
                  (no kernel transition)
```

- Userspace eBPF runtimes: run BPF programs in the same process as the tracee
- No kernel involved → no trap → potentially zero context-switch overhead
- The BPF program runs as a JIT-compiled function in userspace

<!--
This is the direction we're exploring. The idea is to move the BPF execution into the tracee's address space, so the overhead is just a function call.
-->

---

# bpftime

- Userspace eBPF runtime, based on LLVM JIT + Frida injection
- Intercepts `perf_event_open` / `bpf_link_create` syscalls via LD_PRELOAD
- Runs BPF programs in the tracee process — no kernel involvement
- API-compatible with kernel BPF: same programs, same maps

```
┌──────────────────────────────────────────────┐
│  tracee process                              │
│  ┌──────────────────┐  ┌──────────────────┐ │
│  │  bpftime agent   │  │  your binary     │ │
│  │  (LD_PRELOAD)    │  │  (unmodified)    │ │
│  │                  │  │                  │ │
│  │  JIT BPF prog ◀──┼──┼── function call  │ │
│  └──────────────────┘  └──────────────────┘ │
└──────────────────────────────────────────────┘
```

<!--
bpftime is a research project out of PKU and industry collaborators.
It's compatible enough with kernel BPF that existing programs often run unmodified.
-->

---

# xcover + bpftime: the experimental path

```sh
# Start the bpftime syscall server (intercepts BPF syscalls from xcover)
xcover run --path ./myapp --userspace-bpf --detach

# Inject the bpftime agent into the tracee
LD_PRELOAD=$(xcover agent extract) ./myapp
```

- `xcover agent extract` unpacks the embedded bpftime shared library
- Agent intercepts uprobe hits in-process — no kernel trap
- Everything else is identical: same report, same workflow
- Flag: `--userspace-bpf` (experimental)

<!--
The LD_PRELOAD step is currently explicit — we're working on making it transparent via process injection.
The overhead numbers for this path are still being validated. That's the open experiment.
-->

---

# Current status of the userspace path

What works:
- Single-uprobe attachment (perf-event based) ✅
- bpf_cookie propagation (function identification) ✅
- Coverage report generation ✅

Known limitations:
- Requires LD_PRELOAD injection (not transparent for dynamically-started processes)
- Statically linked / musl binaries: no agent injection ⚠️
- Performance numbers: still measuring

> If you've worked with userspace eBPF runtimes — find me after the talk.

<!--
We're being honest about where this is: it works, but it has rough edges.
The hypothesis is sound. The implementation is in progress.
This is worth exploring out loud, which is why it's in the talk.
-->

---

# Wrapping up

**The binary you ship is the binary you test.**

- Coverage tooling today assumes you control the build — at scale, that assumption breaks
- eBPF uprobes let you attach to any binary at runtime, no changes required
- resurgo recovers function entry points from stripped binaries transparently
- The overhead is real, measured, and acceptable for test workloads
- Userspace BPF is the next step: same approach, lower cost

<br>

`github.com/maxgio92/xcover` · `github.com/maxgio92/resurgo`

<!--
That's the full arc. If you take one thing away: the binary you ship doesn't have to be different from the binary you test.
The tooling to close that gap exists today. Try it.
-->

---

<!-- _class: break -->

# Questions?

`github.com/maxgio92/xcover`
`github.com/maxgio92/resurgo`
