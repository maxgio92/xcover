//go:build linux && !userspace

package benchmark

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxgio92/xcover/pkg/trace"
	"github.com/rs/zerolog"
)

const (
	reportPath = "results/bench-report-kernel.json"

	// tracerWarmup is the time given to the tracer to attach uprobes
	// before the target binary is executed.
	tracerWarmup = 300 * time.Millisecond
)

// Package-level sample slices accumulate ns/call values across all -count
// rounds. With -count=N each Benchmark* function runs N times; appending
// here (rather than using a local slice per run) means summarise() in
// TestMain sees the full population, not just the last round's samples.
var (
	baselineSamples []float64
	hitSamples      []float64
	idleSamples     []float64
	missSamples     []float64
)

func TestMain(m *testing.M) {
	if err := buildTargets(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build targets: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	report := &Report{
		Baseline: summarise(baselineSamples),
		Hit:      summarise(hitSamples),
		Idle:     summarise(idleSamples),
		Miss:     summarise(missSamples),
	}
	report.Overheads = Overheads{
		IdleVsBaseline: relOverhead(report.Idle, report.Baseline),
		HitVsBaseline:  relOverhead(report.Hit, report.Baseline),
		HitVsIdle:      relOverhead(report.Hit, report.Idle),
		MissVsBaseline: relOverhead(report.Miss, report.Baseline),
		MissVsIdle:     relOverhead(report.Miss, report.Idle),
		MissVsHit:      relOverhead(report.Miss, report.Hit),
	}
	if err := os.MkdirAll("results", 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create results dir: %v\n", err)
	}
	if err := writeReport(reportPath, report); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write report: %v\n", err)
	}

	os.Exit(code)
}

// runTarget executes the binary and returns the ns/call value it prints.
func runTarget(binary string) (float64, error) {
	out, err := exec.Command(binary).Output()
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
}

// startTracer initialises and starts an xcover tracer in the background for
// the given binary and symbol include pattern. The returned cancel function
// must be called to stop the tracer.
func startTracer(tb testing.TB, binary, include string) context.CancelFunc {
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

	// Cancel the context and wait for Run() to return. Run() defers
	// CloseBPFMod(), so by the time Wait() unblocks all BPF links have
	// been destroyed and the kernel has detached the uprobes. No sleep
	// needed - the next tracer only starts once the previous one is gone.
	return func() {
		cancel()
		wg.Wait()
	}
}

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
	cancel := startTracer(b, hitBinary, `^target_func$`)
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
	cancel := startTracer(b, idleBinary, `^target_func$`)
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
	cancel := startTracer(b, missBinary, `^func_[0-9]+$`)
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
