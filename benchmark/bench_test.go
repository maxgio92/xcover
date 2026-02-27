//go:build linux

package benchmark

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maxgio92/xcover/pkg/trace"
	"github.com/rs/zerolog"
)

const (
	hitBinary  = "./target/hit/hit"
	idleBinary = "./target/idle/idle"
	missBinary = "./target/miss/miss"

	reportPath = "bench-report.json"

	// tracerWarmup is the time given to the tracer to attach uprobes
	// before the target binary is executed.
	tracerWarmup = 300 * time.Millisecond
	// tracerCooldown is the time waited after cancelling the tracer to let
	// the kernel fully detach uprobes and unload the BPF program. Without
	// this, uprobe handlers accumulate across -count runs, causing ns/call
	// to grow monotonically with each count iteration.
	tracerCooldown = 500 * time.Millisecond
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
		Deltas: Deltas{
			IdleVsBaseline: diffSummary(report.Idle, report.Baseline),
			HitVsBaseline:  diffSummary(report.Hit, report.Baseline),
			HitVsIdle:      diffSummary(report.Hit, report.Idle),
			MissVsBaseline: diffSummary(report.Miss, report.Baseline),
			MissVsIdle:     diffSummary(report.Miss, report.Idle),
			MissVsHit:      diffSummary(report.Miss, report.Hit),
		},
	}
	if err := writeReport(reportPath, report); err != nil {
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
	go tracer.Run(ctx) //nolint:errcheck

	// Allow time for uprobes to attach before the target runs.
	time.Sleep(tracerWarmup)

	// Wrap cancel to include a cooldown sleep so callers get the teardown
	// delay transparently via defer cancel().
	return func() {
		cancel()
		time.Sleep(tracerCooldown)
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
