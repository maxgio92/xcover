//go:build linux && !userspace

package benchmark

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maxgio92/xcover/pkg/healthcheck"
	"github.com/maxgio92/xcover/pkg/trace"
	"github.com/rs/zerolog"
)

const (
	reportPath = "results/bench-report-kernel.json"

	// tracerReadyTimeout bounds how long startTracer waits for the tracer
	// to report readiness on the health check socket, that is, for the
	// uprobes to be attached and the ring buffer to be consumed.
	tracerReadyTimeout = 30 * time.Second

	// readyDialInterval is the retry period while the health check socket
	// does not accept connections yet.
	readyDialInterval = 10 * time.Millisecond
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
// the given binary and symbol include pattern, and returns once the tracer
// reports readiness on its health check socket. The returned cancel
// function must be called to stop the tracer; it fails the benchmark if
// Run returned an error.
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

	runErr := make(chan error, 1)
	go func() {
		runErr <- tracer.Run(ctx)
	}()

	// Block until the uprobes are attached and events are consumed, or
	// fail fast if Run gives up before signalling readiness.
	ready := make(chan error, 1)
	go func() {
		ready <- waitTracerReady(ctx, trace.HealthCheckSockPath, tracerReadyTimeout)
	}()
	select {
	case err := <-runErr:
		cancel()
		tb.Fatalf("tracer exited before readiness: %v", err)
	case err := <-ready:
		if err != nil {
			cancel()
			<-runErr
			tb.Fatalf("tracer readiness: %v", err)
		}
	}

	// Cancel the context and wait for Run() to return. Run() defers
	// CloseBPFMod(), so by the time it returns all BPF links have been
	// destroyed and the kernel has detached the uprobes. No sleep needed:
	// the next tracer only starts once the previous one is gone.
	return func() {
		cancel()
		if err := <-runErr; err != nil {
			tb.Errorf("tracer run: %v", err)
		}
	}
}

// waitTracerReady connects to the tracer health check socket and blocks
// until the server writes healthcheck.ReadyMsg, which it does only once the
// tracer is consuming function events.
func waitTracerReady(ctx context.Context, sockPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	var conn net.Conn
	for {
		var err error
		conn, err = net.DialTimeout("unix", sockPath, readyDialInterval)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("dialing %s: %w", sockPath, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readyDialInterval):
		}
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(deadline); err != nil {
		return err
	}
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err != nil {
		return fmt.Errorf("reading readiness from %s: %w", sockPath, err)
	}
	if buf[0] != healthcheck.ReadyMsg {
		return fmt.Errorf("unexpected readiness byte %#x from %s", buf[0], sockPath)
	}
	return nil
}

// BenchmarkBaseline measures plain function-call overhead with no probes
// attached. This is the reference point for computing uprobe overhead.
func BenchmarkBaseline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ns, err := runTarget(idleBinary)
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
