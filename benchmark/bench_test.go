//go:build linux

package benchmark

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxgio92/xcover/pkg/bpftime"
	"github.com/maxgio92/xcover/pkg/trace"
	"github.com/rs/zerolog"
)

const (
	hitBinary  = "./target/hit/hit"
	idleBinary = "./target/idle/idle"
	missBinary = "./target/miss/miss"

	// tracerWarmup is the time given to the tracer to attach uprobes
	// before the target binary is executed.
	tracerWarmup = 300 * time.Millisecond
)

// agentPath holds the path to the extracted bpftime agent library.
// It is populated once in TestMain and reused by all userspace benchmarks.
var agentPath string

// Package-level sample slices accumulate ns/call values across all -count
// rounds. With -count=N each Benchmark* function runs N times; appending
// here (rather than using a local slice per run) means summarise() in
// TestMain sees the full population, not just the last round's samples.
var (
	baselineSamples      []float64
	hitSamples           []float64
	idleSamples          []float64
	missSamples          []float64
	hitUserspaceSamples  []float64
	idleUserspaceSamples []float64
	missUserspaceSamples []float64
)

func TestMain(m *testing.M) {
	if err := buildTargets(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build targets: %v\n", err)
		os.Exit(1)
	}

	var err error
	agentPath, err = bpftime.ExtractAgent()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to extract bpftime agent: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(agentPath)

	code := m.Run()

	report := &Report{
		Baseline:      summarise(baselineSamples),
		Hit:           summarise(hitSamples),
		Idle:          summarise(idleSamples),
		Miss:          summarise(missSamples),
		HitUserspace:  summarise(hitUserspaceSamples),
		IdleUserspace: summarise(idleUserspaceSamples),
		MissUserspace: summarise(missUserspaceSamples),
	}

	baseline := report.Baseline
	hit := report.Hit
	idle := report.Idle
	miss := report.Miss
	hitUS := report.HitUserspace
	idleUS := report.IdleUserspace
	missUS := report.MissUserspace

	report.Overheads = Overheads{
		IdleVsBaseline: relOverhead(idle, baseline),
		HitVsBaseline:  relOverhead(hit, baseline),
		HitVsIdle:      relOverhead(hit, idle),
		MissVsBaseline: relOverhead(miss, baseline),
		MissVsIdle:     relOverhead(miss, idle),
		MissVsHit:      relOverhead(miss, hit),

		HitUserspaceVsBaseline:  relOverhead(hitUS, baseline),
		IdleUserspaceVsBaseline: relOverhead(idleUS, baseline),
		MissUserspaceVsBaseline: relOverhead(missUS, baseline),

		HitKernelVsUserspace:  relOverhead(hit, hitUS),
		MissKernelVsUserspace: relOverhead(miss, missUS),
	}

	if err := writeReport(reportPath(), report); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write report: %v\n", err)
	}

	os.Exit(code)
}

// buildTargets runs make in the benchmark directory to compile all C targets.
func buildTargets() error {
	cmd := exec.Command("make", "all")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runTarget executes the binary and returns the ns/call value it prints.
// Optional env entries (e.g. "LD_PRELOAD=/path/to/agent.so") are appended
// to the current environment.
func runTarget(binary string, env ...string) (float64, error) {
	cmd := exec.Command(binary)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
}

// startTracer initialises and starts an xcover tracer in the background for
// the given binary and symbol include pattern. When userspace is true the
// tracer uses the bpftime userspace BPF runtime instead of the kernel.
// The returned cancel function must be called to stop the tracer.
func startTracer(tb testing.TB, binary, include string, userspace bool) context.CancelFunc {
	tb.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	logger := zerolog.Nop()

	tracee := trace.NewUserTracee(
		trace.WithTraceeExePath(binary),
		trace.WithTraceeSymPatternInclude(include),
		trace.WithTraceeLogger(logger),
	)
	tracer := trace.NewUserTracer(
		trace.WithTracerTracee(tracee),
		trace.WithTracerReport(false),
		trace.WithTracerStatus(false),
		trace.WithTracerLogger(logger),
		trace.WithTracerUserspaceBPF(userspace),
	)

	if err := tracer.Init(ctx); err != nil {
		cancel()
		tb.Fatalf("tracer init: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tracer.Run(ctx) //nolint:errcheck
	}()

	// Allow time for uprobes to attach before the target runs.
	time.Sleep(tracerWarmup)

	return func() {
		cancel()
		wg.Wait()
	}
}

// cleanBptimeSHM removes any stale bpftime shared memory segments left by a
// previous run. Must be called before starting a new userspace benchmark
// iteration to avoid the fresh agent inheriting stale state.
func cleanBptimeSHM() {
	matches, _ := filepath.Glob("/dev/shm/bpftime_*")
	for _, f := range matches {
		os.Remove(f)
	}
}

// bptimeEnv returns the environment entries required to run a target binary
// with the bpftime agent injected.
func bptimeEnv() []string {
	return []string{
		"LD_PRELOAD=" + agentPath,
		"BPFTIME_SHM_MEMORY_MB=2048",
	}
}

// ---------------------------------------------------------------------------
// Kernel-mode benchmarks
// ---------------------------------------------------------------------------

// BenchmarkBaseline measures plain function-call overhead with no probes
// attached. This is the reference point for computing uprobe overhead.
func BenchmarkBaseline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ns, err := runTarget(hitBinary)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(ns, "ns/call")
		baselineSamples = append(baselineSamples, ns)
	}
}

// BenchmarkHit measures uprobe overhead on the steady-state hit path.
// target_func is probed and called N times: after the first call its cookie
// is already in seen_funcs, so all subsequent firings take the fast path
// (map lookup hit → early return).
func BenchmarkHit(b *testing.B) {
	cancel := startTracer(b, hitBinary, `^target_func$`, false)
	defer cancel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ns, err := runTarget(hitBinary)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(ns, "ns/call")
		hitSamples = append(hitSamples, ns)
	}
}

// BenchmarkIdle measures the overhead on code that is not probed while a
// probe is attached to a different function in the same binary.
// target_func is probed but never called; idle_func is timed instead.
// Expected result: idle ≈ baseline, showing probes don't affect unprobed paths.
func BenchmarkIdle(b *testing.B) {
	cancel := startTracer(b, idleBinary, `^target_func$`, false)
	defer cancel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ns, err := runTarget(idleBinary)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(ns, "ns/call")
		idleSamples = append(idleSamples, ns)
	}
}

// BenchmarkMiss measures uprobe overhead on the miss path.
// N distinct functions are each called exactly once, so every uprobe firing
// hits the full slow path (cookie not in seen_funcs → map update →
// ringbuf reserve → submit).
func BenchmarkMiss(b *testing.B) {
	cancel := startTracer(b, missBinary, `^func_[0-9]+$`, false)
	defer cancel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ns, err := runTarget(missBinary)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(ns, "ns/call")
		missSamples = append(missSamples, ns)
	}
}

// ---------------------------------------------------------------------------
// Userspace-mode benchmarks (bpftime)
//
// These mirror the kernel-mode benchmarks above. The only differences are:
//   - the tracer uses WithTracerUserspaceBPF(true)
//   - the target binary is launched with the bpftime agent preloaded
//   - stale bpftime SHM segments are cleared before each run
//
// Run via: make bench-userspace
// The Makefile sets LD_PRELOAD for the syscall-server so that xcover's BPF
// syscalls are intercepted and handled in userspace from process start.
// ---------------------------------------------------------------------------

// BenchmarkHitUserspace measures uprobe overhead on the steady-state hit path
// using the bpftime userspace BPF runtime.
func BenchmarkHitUserspace(b *testing.B) {
	cleanBptimeSHM()
	cancel := startTracer(b, hitBinary, `^target_func$`, true)
	defer cancel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ns, err := runTarget(hitBinary, bptimeEnv()...)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(ns, "ns/call")
		hitUserspaceSamples = append(hitUserspaceSamples, ns)
	}
}

// BenchmarkIdleUserspace measures the overhead on an unprobed function while
// a bpftime probe is attached to a different function in the same binary.
func BenchmarkIdleUserspace(b *testing.B) {
	cleanBptimeSHM()
	cancel := startTracer(b, idleBinary, `^target_func$`, true)
	defer cancel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ns, err := runTarget(idleBinary, bptimeEnv()...)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(ns, "ns/call")
		idleUserspaceSamples = append(idleUserspaceSamples, ns)
	}
}

// BenchmarkMissUserspace measures uprobe overhead on the miss path using the
// bpftime userspace BPF runtime. Every uprobe firing hits the full slow path.
func BenchmarkMissUserspace(b *testing.B) {
	cleanBptimeSHM()
	cancel := startTracer(b, missBinary, `^func_[0-9]+$`, true)
	defer cancel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ns, err := runTarget(missBinary, bptimeEnv()...)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(ns, "ns/call")
		missUserspaceSamples = append(missUserspaceSamples, ns)
	}
}
