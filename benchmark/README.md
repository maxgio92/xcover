# xcover latency benchmark

Measures the per-call overhead that xcover's uprobe-based tracing adds to a target binary, across three distinct execution paths.

## Background

xcover attaches a BPF program to every traced function via `uprobe_multi`. When a traced function is called, the kernel transfers control to the BPF handler before resuming the function.

**How the kernel intercepts the call:** the target function's entry point is patched *in the process's virtual memory* (the on-disk binary is not modified) with a breakpoint instruction (`int3`). The original instruction is saved. When the CPU hits `int3` it raises a trap, transitioning to kernel mode. The kernel runs the uprobe BPF handler, then single-steps the saved original instruction and returns to userspace. This kernel/user round-trip on every call is the core **cost of uprobe**-based tracing.

The handler does one of two things depending on whether it has seen this function before:

**Hit path** - the function's cookie is already in the `seen_funcs` BPF hash map:
```
uprobe fires → BPF handler runs → map lookup (hit) → return
```

**Miss path** - first time this function is called:
```
uprobe fires → BPF handler runs → map lookup (miss) → map update → ringbuf reserve → write event → submit
```

The hit path is the steady state for long-running programs. The miss path only fires once per unique function per tracer session. The cost difference between the two is the price of a BPF hash map insert plus a ring buffer write.

## Scenarios

| Scenario | What it measures |
|---|---|
| **baseline** | Plain function-call overhead with no probes attached. Reference point. |
| **idle** | A probe is attached to a different function in the same binary; the timed function is never probed. Should be ≈ baseline. |
| **hit** | The probed function is called N=10,000 times. After the first call its cookie is in `seen_funcs`, so all subsequent firings take the fast path. |
| **miss** | N=10,000 distinct functions are each called exactly once. Every firing hits the slow path. |

## How it works

Each scenario has a C target binary that times its own execution using `clock_gettime(CLOCK_MONOTONIC)` around its main loop and prints `ns/call` to stdout. The Go benchmark driver starts the xcover tracer, runs the binary, and collects the reported value. The Go testing timer (`ns/op`) is not used for measurement - it only reflects total wall time including tracer startup and teardown.

`-benchtime=1x` is intentional: it runs each benchmark body exactly once per `-count` round. Time-based benchtime would let BPF map state carry over between `b.N` iterations, turning miss measurements into hit measurements after the first iteration.

## Running

```sh
# Build targets and run 100 rounds
make bench
```

> Requires `sudo` (because of needed `CAP_BPF` + `CAP_PERFMON`) to load BPF programs.

### Custom rounds

```
make -e COUNT=ROUNDS bench
```

where `ROUNDS` is a integer number that is passed to `go test -bench -count=ROUNDS`.

## Report

Results are written to `bench-report.json`. Summaries are computed across all `-count` rounds.

```json
{
  "baseline": { "mean_ns": 1.66, "p50_ns": 1.30, "p99_ns": 2.58, "samples": 100 },
  "idle":     { "..." },
  "hit":      { "..." },
  "miss":     { "..." },
  "overheads": {
    "hit_vs_baseline":  { "mean_pct": 799.29, "p50_pct": "...", "p99_pct": "..." },
    "miss_vs_hit":      { "mean_pct": 0.80,   "p50_pct": "...", "p99_pct": "..." }
  }
}
```

Overhead values are `(a - b) / b` as a decimal fraction: `0.80` means scenario `a` costs 80% more than `b` (i.e., `a = 1.80 × b`).
