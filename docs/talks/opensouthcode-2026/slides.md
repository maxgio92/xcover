---
marp: true
theme: default
paginate: true
html: true
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

  /* Code blocks - gruvbox dark */
  pre {
    background: #282828;
    border-radius: 8px;
    padding: 1em 1.2em;
    margin: 0.6em 0;
    overflow: hidden;
    /* Theme ships light-palette syntax colors; remap them to the
       gruvbox dark palette so tokens are readable on the dark background. */
    --color-prettylights-syntax-comment: #928374;
    --color-prettylights-syntax-constant: #d3869b;
    --color-prettylights-syntax-entity: #fabd2f;
    --color-prettylights-syntax-entity-tag: #8ec07c;
    --color-prettylights-syntax-keyword: #fb4934;
    --color-prettylights-syntax-string: #b8bb26;
    --color-prettylights-syntax-string-regexp: #8ec07c;
    --color-prettylights-syntax-variable: #fe8019;
    --color-prettylights-syntax-storage-modifier-import: #ebdbb2;
    --color-prettylights-syntax-constant-other-reference-link: #b8bb26;
    --color-prettylights-syntax-markup-heading: #83a598;
    --color-prettylights-syntax-markup-list: #fabd2f;
    --color-prettylights-syntax-markup-bold: #fbf1c7;
    --color-prettylights-syntax-markup-italic: #fbf1c7;
    --color-prettylights-syntax-markup-inserted-text: #b8bb26;
    --color-prettylights-syntax-markup-inserted-bg: #32361a;
    --color-prettylights-syntax-markup-deleted-text: #fb4934;
    --color-prettylights-syntax-markup-deleted-bg: #3c1f1e;
  }
  pre code {
    color: #ebdbb2;
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

# `$whoami`

- Massimiliano Giovagnoli
- Software engineer @ Chainguard
- OSS, building and observing all the things
- Music, racing cars, nature
- Newbie dad and experienced cat employee
- `@maxgio92` on GitHub, X and Telegram, `@maxgio92.bsky.social`, `@maxgio92@hachyderm.io`
- linkedin.com/in/maxgio

---

# Agenda

- The standard coverage story
- Profiling with the kernel
- xcover in practice
- Demo
- Challenges with production binaries
- Demo: xcover on a stripped binary
- The cost of profiling with eBPF
- Benchmarking the overhead
- Eliminate the kernel trap
- Demo: userspace eBPF mode
- Limitations & what's next
- Wrapping up

---

# Quality

- How do you ensure that your software works?
- How do you ensure that your strategy is telling you a reliable story?
- How do you ensure that positive signals are meaningful?

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
go build -cover -covermode=count -coverpkg=./... -o myapp ./main.go

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

# Testing Linux distro packages

```yaml
  - name: crane-cov
    description: "Crane compiled for collecting coverage profiles"
    pipeline:
      - uses: go/build
        with:
          extra-args: -cover
    test:
      environment:
        contents:
          packages:
            - busybox
            - go
        environment:
          GOCOVERDIR: /home/build
      pipeline:
        - name: Run a command with the instrumented binary
          runs: |
            crane manifest chainguard/static
        - name: Report function coverage
          runs: |
            go tool covdata func -i=.
```

---

# The problem at scale

```
package-foo/
  build/
    package-foo-1.0.0.apk                ← what you ship
    package-foo-1.0.0-instrumented.apk   ← what you test
```

- Every package needs two build targets
- You maintain **thousands of packages** (Go, C, C++, Rust, Python extensions...)
- Doubled CI time, doubled storage, doubled maintenance
- Reproducibility: **You're not testing what you ship**

<!--
At Chainguard we maintain a large corpus of packages. The moment you try to apply coverage uniformly across that, the operational cost becomes significant.
But the tooling fragmentation isn't even the biggest problem.

This is the core of the problem. The artifact you test is not the artifact you ship.
That gap is real — compiler optimizations, link order, environment differences.
The instrumented build gives you coverage numbers that may not reflect what actually runs in production.
-->

---

<!--
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

This is the second blocker. Even if you were willing to maintain a separate instrumented build, you can't apply it retroactively to a stripped production binary.
The data that coverage tools need just isn't there.

---

-->

<!-- _class: statement -->

# What if coverage didn't require<br>a separate build at all?

<!--
What if the tool could attach to the binary you already built, tested, packaged, and shipped?
To answer that, we need to talk about what the kernel can see.
-->

---

<!-- _class: statement -->

# Profiling with the kernel

---

# The kernel sees everything

- **uprobes**: Linux kernel user-level dynamic tracing
- Available since Linux 3.5 [1]
- Attach to a function by **binary path + offset**
- Works on **any ELF binary**
- Integrated into eBPF since Linux 3.18 [2] - `BPF_PROG_TYPE_UPROBE`

> 1. https://lwn.net/Articles/499190/
> 2. https://lwn.net/Articles/637391/

<!--
uprobes are a kernel feature that lets you intercept the entry point of any function in any userspace process.
They work by patching the first byte of the function with an INT3 (breakpoint) instruction.
When the CPU hits it, the kernel takes over, runs your handler, then resumes execution.
-->

---

# What is a uprobe?

```
Loaded image:
  offset 0x1a40: PUSH RBP        ← function entry
  offset 0x1a41: MOV RBP, RSP

With uprobe attached:
  offset 0x1a40: INT3            ← kernel patches this
                 (original bytes saved)
```

At runtime, on each call:
1. Software interrupt causes a trap into kernel mode
3. BPF program runs
4. Registers restored, execution continues in userland

<!--
The key point: the offset of a function in the binary is fixed. Strip all the symbols you want — the function is still at that address.
If you know the offset, you can attach a uprobe. You don't need the symbol table.
-->

---

# eBPF makes uprobes programmable

- **eBPF**: run sandboxed programs in the kernel, triggered by events
- Loaded programs are **verifier-checked**: no crashes, bounded execution, safe
- **Attach** programs to specific kernel paths - i.e. uprobe
- Access to kernel data and communicate with userland with **maps**

```
userspace (xcover)          kernel
      │                        │
      │── load BPF program ─▶ │  (verifier checks it)
      │── attach uprobe ────▶ │  function entry at offset 0x1a40
      │                        │
      │        [test runs]     │
      │                        │
      │◀── read BPF map ────  │  which functions fired
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
- Attaches to the binary at its path
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
 ┌──────────────────────────────── userspace ──────────────────────────────────┐ 
 │                                                                             │ 
 │  ┌───────────────────────────────┐      ┌──────────────────────────────┐    │ 
 │  │   xcover (daemon)             │      │   your binary (tracee)       │    │ 
 │  │                               │      │                              │    │ 
 │  │  1. resolve function offsets  │      │  foo() ← uprobe attached     │    │ 
 │  │  2. load BPF program          │      │  bar() ← uprobe attached     │    │ 
 │  │  3. attach uprobes            │      │          ▲                   │    │ 
 │  │  4. read ring buffer          │      │          │  │                │    │ 
 │  └────────────┬──────────────────┘      └──────────┼──┼────────────────┘    │ 
 │            ▲  │bpf() syscalls                      │  │                     │ 
 └────────────┼──┼────────────────────────────────────┼──┼─────────────────────┘ 
              │  │                               patch│  │kernel trap            
 ┌────────────┼──┼──────────────── kernel ────────────┼──┼─────────────────────┐ 
 │  evt submit│  ▼                                    │  │                     │ 
 │   ┌────────┴────────────────────┐                  │  │                     │ 
 │   │  xcover (BPF uprobe)        │  ──────────►        ▼                     │ 
 │   │                             │  ◄─────────  uprobes                      │ 
 │   │ 1. get function cookie      │                                           │ 
 │   │ 2. write to ring buffer     │                                           │ 
 │   └─────────────────────────────┘                                           │ 
 │                                                                             │ 
 └─────────────────────────────────────────────────────────────────────────────┘
```

<!--
xcover is two logical pieces: UserTracee resolves the function list (using resurgo for stripped binaries), and UserTracer loads the BPF program, attaches uprobes, and drains the event map.
The tracee (your binary under test) runs completely independently. xcover just observes it.
-->

---

# The CLI workflow

```sh
# Start the profiler (detach to background)
xcover run --path ./myapp --detach

# Wait until all probes are attached (ready to trace)
xcover wait

# Run your tests — nothing changes here
./myapp foo
./myapp bar
./myapp baz

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

# Auto-detect the project scope from the binary
xcover run --path ./myapp \
  --scope project \
  --detach
```

- `--include` and `--exclude` take Go regexes
- Applied to function names at probe-attachment time
- `--scope project` auto-detects the project scope from the binary and filters to it (Go modules supported today)

<!--
Without filtering you'd be tracing every function in the binary, including the Go runtime and all stdlib functions.
Filtering to your own packages is the recommended default.
--scope project is the language-agnostic shortcut: xcover detects the project scope from the binary and builds the filter. Currently implemented for Go modules.
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

- `funcs_traced`: all functions with probes attached
- `funcs_ack`: functions called at least once during the run
- `cov_by_func`: ratio: 72% of functions were exercised

<!--
Language-agnostic JSON. Parse it in CI, diff it across runs, set a coverage gate.
Same format whether you're testing Go, C, or Rust.
-->

---

<!-- _class: break -->

# Demo

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

# Challenges with<br>production binaries

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

$ readelf --sections myapp | grep -E '\.symtab|\.debug'
  [32] .symtab           SYMTAB  ...  ← function names + offsets
  [33] .strtab           STRTAB  ...  ← symbol name strings
  [34] .debug_info       PROGBITS ...
  [35] .debug_line       PROGBITS ...

$ readelf --sections myapp-stripped | grep -E '\.symtab|\.debug'
$
$ readelf --symbols myapp-stripped
$
```

- `strip` removes `.symtab`, `.strtab`, and all `.debug_*` sections
- **The code is still there.** Only the metadata is gone.

<!--
Strip doesn't touch the executable code. It just removes the table of contents.
The functions are still at the same addresses. We just need another way to find them.
-->

---

# What strip does NOT remove

```sh
$ readelf --sections myapp-stripped | grep '\.eh_frame'
  [17] .eh_frame_hdr     PROGBITS ...
  [18] .eh_frame         PROGBITS ...
```

- `.eh_frame`: DWARF Call Frame Information, needed for stack unwinding
- Survives `strip --strip-all`: needed for **exception handling** (e.g. C++)
- Encodes function boundaries and stack layout
- The compiler always emits it. The linker always keeps it.

<!--
This is the key insight that makes resurgo work.
The compiler has to emit .eh_frame for exception handling and stack unwinding to work.
It can't be stripped without breaking the binary.
And it encodes exactly what we need: where functions start and end.
-->

---

# Introducing resurgo library

Static function recovery for stripped ELF binaries.

Deterministic:
1. **DWARF CFI** (`.eh_frame` ELF section): function ranges, survives strip, high confidence

Heuristic:
1. **Prologue patterns**: at function entry, architecture-specific
2. **Call-site analysis**: `call` targets are function entry points by definition
3. **Alignment boundaries**: compilers align functions to 16/32-byte boundaries

Heuristic- and determinism-based validation for confidence.

`github.com/maxgio92/resurgo`

<!--
No single signal is perfect. .eh_frame is the most reliable but can miss inlined functions.
Prologue patterns have false positives. Call-site analysis misses leaf functions that are never called directly.
Combined, they give you a high-confidence function list.
-->

---

# xcover function recovery with resurgo

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
$ xcover run --path ./myapp --detach --log-level debug
DBG  resolved 1842 functions via .symtab

# Stripped binary — same command
$ xcover run --path ./myapp-stripped --detach --log-level debug
DBG  .symtab not found, falling back to static recovery
DBG  resolved 1791 functions via resurgo (eh_frame + prologue analysis)
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

# Demo: xcover on a stripped binary

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

# The cost of profiling with eBPF

---

<!-- _class: statement -->

# Benchmarking the overhead

---

# The overhead model

Every call to a traced function pays:

```
function called
     │
     ▼
  INT3 trap ──▶ kernel mode
                     │
                     ▼
              BPF program runs
              (cookie lookup)
              (potential map and ring buffer update)
                     │
                     ▼
              return to userspace
                     │
                     ▼
function body executes
```

- **Fixed cost per call**, independent of function body size
- Overhead scales with **call frequency**, not binary size or number of functions

<!--
The dominant cost is the kernel mode transition — the INT3 trap and return.
The BPF program itself is small and fast. The map operations are O(1).
What matters is how often your functions are called.
-->

---

# The probe

```c
SEC("uprobe/handle_user_function")
int handle_user_function(struct pt_regs *ctx) {
	__u64 cookie = bpf_get_attach_cookie(ctx);
	u8 seen = 1;

	/* Check if the function has been already reported */
	if (bpf_map_lookup_elem(&seen_funcs, &cookie)) {
		return 0;
	}

	/* Track which functions have been reported */
	bpf_map_update_elem(&seen_funcs, &cookie, &seen, BPF_ANY);

	struct event_t *event = bpf_ringbuf_reserve(&events, sizeof(struct event_t), 0);
	if (!event) {
		return 0;
	}

	event->cookie = cookie;
	bpf_ringbuf_submit(event, ringbuffer_flags);

	return 0;
}
```

---

# Benchmark setup

| Scenario | Description | How |
|---|---|---|
| **Baseline** | No tracing | Tracee runs without uprobes |
| **Idle** | uprobe attached | Probe attached to functions that never run |
| **Hit** | uprobe firing, already seen | Tracee runs the same probed function N times |
| **Miss** | uprobe firing, new function | Tracee runs N probed unique functions only once |

<!--
The "attached, not firing" row matters: once you attach a uprobe, even functions that aren't called pay a small cost on the way past the INT3.
For filtered workloads (only your own packages), this is usually negligible.
-->

---

# Aggregate overhead

| Scenario | Description | Time per call | Overhead vs Baseline |
|---|---|---|---|
| **Baseline** | No tracing | ~1.2 ns | |
| **Idle** | uprobe attached | ~2 ns | |
| **Hit** | uprobe firing, already seen | ~1200 ns | ~1000x |
| **Miss** | uprobe firing, new function | ~2900 ns | ~2500x |

> **+X% wall time for full test coverage on the exact production binary.**

- Filtering to your own packages reduces overhead
- Overhead is deterministic; no jitter, no GC interaction
- Acceptable for CI pipelines; not for latency-sensitive benchmarks

<!--
Measured with benchmark/ (benchstat over 10 runs, N=100 uprobes, AMD Ryzen 7 7840U):
baseline 1.17 ns, idle 1.99 ns, hit 1230 ns, miss 2911 ns per call.
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
| Per-call overhead | ~0 | ~1200 ns |
| Setup complexity | Per-language | Once |

> You pay a small, predictable cost per function call.<br>In exchange: no instrumented builds, one tool, any binary.

<!--
The overhead is real. We didn't hide it. But look at what you get in return.
You test the exact binary that runs in production, with no changes to your build, in any language.
For coverage — a quality signal, not a latency-critical path — this tradeoff is worth it.
-->

---

<!-- _class: statement -->

# Eliminate the kernel trap

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

# eunomia-bpf/bpftime

- Userspace eBPF runtime: `syscall_server.so` and `agent.so` libraries
- `syscall_server` intercepts `perf_event_open` / `bpf_link_create` syscalls via `LD_PRELOAD`
- `syscall_server` creates data structures in a shared memory, read by the `agent`
- `agent` sets up the trampolines at function entry to the BPF `BPF_PROG_TYPE_UPROBE` program
- No kernel trap
- API-compatible with kernel eBPF

```
┌──────────────────────────────────────────────┐
│  tracee process                              │
│  ┌──────────────────┐  ┌──────────────────┐  │
│  │  bpftime agent   │  │  your binary     │  │
│  │  (LD_PRELOAD)    │  │  (unmodified)    │  │
│  │                  │  │                  │  │
│  │  JIT BPF prog ◀─┼──┼── function call  │  │
│  │  (shm)           │  │                  │  │
│  └──────────────────┘  └──────────────────┘  │
└──────────────────────────────────────────────┘
```

<!--
bpftime is a research project out of PKU and industry collaborators.
It's compatible enough with kernel BPF that existing programs often run unmodified.
-->

---

# xcover + bpftime: the experimental path

```sh
# Build xcover with userspace BPF support
make xcover-userspace

# Start the bpftime syscall server (intercepts BPF syscalls from xcover)
xcover-userspace run --path ./myapp --userspace-bpf --detach

# Inject the bpftime agent into the tracee
LD_PRELOAD=$(xcover-userspace agent extract) ./myapp
```

- `agent extract` unpacks the embedded bpftime shared library
- bpftime `agent` intercepts uprobe hits in-process
- Everything else is identical: same report, same workflow
- **Merged**: opt-in build (`-tags userspace`), flag `--userspace-bpf` (experimental)

<!--
The LD_PRELOAD step is currently explicit; we're working on making it transparent via process injection.
The mode is merged in main as an opt-in build; the overhead numbers are on the next slide.
-->

---

# Let's benchmark again!

| Scenario | Description | Overhead reduction Userspace vs Kernel |
|---|---|---|
| **Baseline** | No tracing | ~ |
| **Idle** | uprobe attached | **-43%** |
| **Hit** | uprobe firing, already seen | **-65%** |
| **Miss** | uprobe firing, new function | **-63%** |

<!--
Measured with benchmark/ (benchstat over 10 runs each, N=100 uprobes, AMD Ryzen 7 7840U):
kernel hit 1230 ns vs userspace 426 ns; kernel miss 2911 ns vs userspace 1084 ns;
kernel idle 1.99 ns vs userspace 1.13 ns; baseline unchanged (p=0.72).
-->

---

<!-- _class: break -->

# Demo: userspace eBPF mode

---

# Current status of the userspace mode

## What works
- Single-uprobe attachment (perf-event based) ✅
- bpf_cookie propagation (function identification) ✅
- Coverage report generation ✅
- Unprivileged run (no `CAP_BPF`)

## Known limitations
- No `uprobe_multi` link support (perf-based)
- Requires `LD_PRELOAD` injection of the agent (manual step; children inherit it through the environment, `execve` included)
- Statically linked / musl binaries: no agent injection ⚠️
- Frida Gum interceptor: some aggressive compiler optimisations (tail-call elision, LTO) are not yet handled

> If you've worked with userspace eBPF runtimes, find me after the talk.

<!--
We're being honest about where this is: it works, but it has rough edges.
The hypothesis held up. The implementation is merged in main as an opt-in build.
This is worth exploring out loud, which is why it's in the talk.
-->

---

# Limitations & what's next

## Limitations
- Aggressive compiling optimizations may defeat uprobe-based interception
- There is an overhead cost

## Upstream gaps
- **bpftime**: some patches that are going to be proposed upstream

## Landed upstream
- **libbpfgo**: `AttachUprobeWithOpts` (single-uprobe attach with per-probe `bpf_cookie`)
  - https://github.com/aquasecurity/libbpfgo/pull/530

## What I'm working on
- Rolling out xcover on thousands of packages
- Performance optimizations
- Stress test the userspace mode

<!--
Be honest about the rough edges. The LD_PRELOAD requirement is the most visible one — users notice it immediately.
The upstream gaps are being addressed: bpftime PR #570 already landed the bpf_cookie fix, and we're tracking the rest.
The goal is to make the userspace path as frictionless as the kernel path.
-->

---

# Wrapping up

**The binary you ship is the binary you test.**

- Coverage tooling today assumes you control the build. At scale, that assumption breaks
- Kernel instrumentation allows to observe the runtime
- Recent work improves Go project scoping and uses debug files when available
- The overhead is real, measured. It can be acceptable for test workloads
- If you want to cut the overhead, userspace BPF runtimes can reduce it up to 4x

<br>

`github.com/maxgio92/xcover` · `github.com/maxgio92/resurgo`

<!--
That's the full arc. If you take one thing away: the binary you ship doesn't have to be different from the binary you test.
The tooling to close that gap exists today. Try it.
-->

---

<!-- _class: break -->

# Questions?

@maxgio92 on Telegram
github.com/maxgio92/resurgo

<div style="display:flex; gap:64px; justify-content:center; align-items:flex-start; margin-top:16px;">
  <figure style="margin:0; text-align:center;">
    <img src="github-qr.png" width="160" />
    <figcaption style="color:#8b949e; font-size:1rem; margin-top:8px;">github.com/maxgio92/xcover</figcaption>
  </figure>
  <figure style="margin:0; text-align:center;">
    <img src="linkedin-qr.png" width="160" />
    <figcaption style="color:#8b949e; font-size:1rem; margin-top:8px;">linkedin.com/in/maxgio</figcaption>
  </figure>
</div>
