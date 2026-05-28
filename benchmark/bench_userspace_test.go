//go:build linux && userspace

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

	"github.com/maxgio92/xcover/pkg/bpftime"
	"github.com/maxgio92/xcover/pkg/trace"
	"github.com/rs/zerolog"
)

const (
	reportPathUserspace = "results/bench-report-userspace.json"

	// Userspace attach via bpftime SHM negotiation needs a longer warmup
	// than kernel uprobes.
	tracerWarmupUserspace = 1 * time.Second
)

// agentLibPath is the path to the extracted bpftime agent library, set in
// TestMain and used by runTargetUserspace.
var agentLibPath string

var (
	baselineSamples []float64
	hitSamples      []float64
	idleSamples     []float64
	missSamples     []float64
)

func TestMain(m *testing.M) {
	// Ensure the bpftime syscall-server is loaded into this process before
	// any BPF operations. On the first invocation this re-execs the process
	// with LD_PRELOAD set; the re-exec'd process finds the sentinel and
	// continues normally.
	if err := bpftime.EnsureSyscallServer(); err != nil {
		fmt.Fprintf(os.Stderr, "bpftime: load syscall-server: %v\n", err)
		os.Exit(1)
	}

	// Extract the bpftime agent library to a temp file so it can be
	// injected into the tracee via LD_PRELOAD.
	var err error
	agentLibPath, err = bpftime.ExtractAgent()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bpftime: extract agent: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(agentLibPath)

	if err := buildTargets(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build targets: %v\n", err)
		os.Exit(1)
	}

	// Do NOT call cleanSHM() here. The syscall-server's start_up() fires
	// during library initialisation (before TestMain runs), via the openat
	// interception. Removing the SHM here kills that session and call_once
	// prevents it from being recreated, leaving all tracees unable to open it.
	//
	// Handler cleanup between benchmarks is handled naturally: probe.CloseBPFMod
	// closes every BPF fd, the server intercepts close() and frees the slot.
	// Any truly stale SHM from a previous crashed run is cleared by bpftime's
	// own begin_new_session() → reset_server_state() on re-init.

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
	if err := writeReport(reportPathUserspace, report); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write report: %v\n", err)
	}

	os.Exit(code)
}

// cleanSHM removes stale bpftime shared memory segments.
func cleanSHM() {
	entries, err := os.ReadDir("/dev/shm")
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "bpftime_") {
			os.Remove("/dev/shm/" + e.Name())
		}
	}
}

// runTargetUserspace executes the target binary with the bpftime agent
// injected via LD_PRELOAD and returns the ns/call value the binary prints.
func runTargetUserspace(binary string) (float64, error) {
	// Strip any LD_PRELOAD inherited from the test process (the syscall-server
	// memfd path set by EnsureSyscallServer) so only the agent gets loaded.
	env := filterEnv(os.Environ(), "LD_PRELOAD")
	env = append(env,
		fmt.Sprintf("LD_PRELOAD=%s", agentLibPath),
		"BPFTIME_VM_NAME=ubpf",
	)
	cmd := exec.Command(binary)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	// bpftime's bpf_printk writes to stdout, so the output may contain log
	// lines before the timing result. The binary always prints the float last.
	last := lastLine(string(out))
	return strconv.ParseFloat(last, 64)
}

// runTargetBaseline executes the target binary without bpftime injection.
func runTargetBaseline(binary string) (float64, error) {
	// Strip LD_PRELOAD so the syscall-server doesn't load in the baseline
	// process — baseline must be a clean run with no bpftime overhead.
	cmd := exec.Command(binary)
	cmd.Env = filterEnv(os.Environ(), "LD_PRELOAD")
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
}

// lastLine returns the last non-empty line of s, trimmed of whitespace.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return strings.TrimSpace(s)
}

// filterEnv returns a copy of env with all entries for the given key removed.
func filterEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

// startTracerUserspace initialises and starts an xcover tracer in userspace
// BPF mode. The returned cancel function must be called to stop the tracer.
func startTracerUserspace(tb testing.TB, binary, include string) context.CancelFunc {
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
		trace.WithTracerUserspaceBPF(true),
	)

	if err := tracer.Init(ctx); err != nil {
		cancel()
		tb.Fatalf("tracer init (userspace): %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tracer.Run(ctx) //nolint:errcheck
	}()

	time.Sleep(tracerWarmupUserspace)

	if os.Getenv("XCOVER_PAUSE_TRACEE") == "1" {
		fmt.Printf("\n>>> SHM ready. In another terminal run:\n    catchsegv env LD_PRELOAD=%s BPFTIME_VM_NAME=ubpf BPFTIME_LOG_OUTPUT=console SPDLOG_LEVEL=debug %s\n>>> Press Enter when done.\n", agentLibPath, binary)
		if tty, err := os.Open("/dev/tty"); err == nil {
			buf := make([]byte, 1)
			tty.Read(buf) //nolint:errcheck
			tty.Close()
		}
	}

	return func() {
		cancel()
		wg.Wait()
	}
}

// BenchmarkBaseline measures plain function-call overhead with no probes
// attached. Reference point for computing userspace BPF overhead.
func BenchmarkBaseline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ns, err := runTargetBaseline(hitBinary)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(ns, "ns/call")
		baselineSamples = append(baselineSamples, ns)
	}
}

// BenchmarkHit measures userspace BPF overhead on the steady-state hit path.
// No kernel trap — the BPF program runs inside the tracee via the bpftime
// agent.
func BenchmarkHit(b *testing.B) {
	cancel := startTracerUserspace(b, hitBinary, `^target_func$`)
	defer cancel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ns, err := runTargetUserspace(hitBinary)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(ns, "ns/call")
		hitSamples = append(hitSamples, ns)
	}
}

// BenchmarkIdle measures overhead on unprobed code while a probe is attached
// to a different function. Expected result: idle ≈ baseline.
func BenchmarkIdle(b *testing.B) {
	cancel := startTracerUserspace(b, idleBinary, `^target_func$`)
	defer cancel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ns, err := runTargetUserspace(idleBinary)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(ns, "ns/call")
		idleSamples = append(idleSamples, ns)
	}
}

// BenchmarkMiss measures userspace BPF overhead on the cold path.
// Every firing hits the slow path (map update + ringbuf reserve + submit).
func BenchmarkMiss(b *testing.B) {
	cancel := startTracerUserspace(b, missBinary, `^func_[0-9]+$`)
	defer cancel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ns, err := runTargetUserspace(missBinary)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(ns, "ns/call")
		missSamples = append(missSamples, ns)
	}
}
