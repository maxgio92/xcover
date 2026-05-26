# xcover userspace BPF latency benchmark

Measures the per-call overhead that xcover's **userspace BPF** path (via
[bpftime](https://github.com/eunomia-bpf/bpftime)) adds to a target binary.
Scenarios and methodology are identical to the kernel uprobe benchmark in
`../benchmark/`; the key difference is that BPF programs execute entirely in
the tracee process via the bpftime agent — no kernel mode transition on each
call.

## How it differs from the kernel benchmark

| | Kernel (`../benchmark/`) | Userspace (this suite) |
|---|---|---|
| BPF execution | kernel, via uprobe trap | tracee process, via bpftime agent |
| Privileges | `sudo` + `CAP_BPF`/`CAP_PERFMON` | unprivileged |
| tracee invocation | plain binary | binary + `LD_PRELOAD=<agent>` |
| Extra prereqs | none | `make bpftime-libs` |

## Prerequisites

1. Build the bpftime shared libraries (once, from the project root):

   ```sh
   make -C .. bpftime-libs
   ```

2. Build libbpfgo:

   ```sh
   make -C .. libbpfgo-static
   ```

## Running

```sh
make bench
```

Results are written to `bench-report-userspace.json`.

### Custom rounds

```sh
make -e COUNT=50 bench
```

## Scenarios

Same four scenarios as the kernel benchmark:

| Scenario | What it measures |
|---|---|
| **baseline** | Plain function-call overhead, no probes attached. Reference point. |
| **idle** | Probe attached to a different function; timed function is not probed. Expected ≈ baseline. |
| **hit** | Probed function called N=10,000 times. Steady-state fast path after the first call. |
| **miss** | N=10,000 distinct functions each called once. Every firing hits the slow path. |

## Report

```json
{
  "baseline": { "mean_ns": ..., "p50_ns": ..., "p99_ns": ..., "samples": 100 },
  "idle":     { "..." },
  "hit":      { "..." },
  "miss":     { "..." },
  "overheads": {
    "hit_vs_baseline":  { "mean_x": ..., "p50_x": ..., "p99_x": ... },
    "miss_vs_hit":      { "..." }
  }
}
```

Compare `hit_vs_baseline` here against the same field in
`../benchmark/bench-report.json` to quantify the kernel trap elimination.
