//go:build linux

package benchmark

import (
	"encoding/json"
	"math"
	"os"
	"sort"
)

// Report holds aggregated latency statistics for every benchmark scenario.
// Each field is a Summary whose duration values are in nanoseconds per call.
//
// Kernel-mode and userspace-mode (bpftime) scenarios are reported separately
// so that a single JSON file captures the full picture when both bench and
// bench-userspace are run.  Fields tagged omitempty are absent when the
// corresponding mode was not exercised.
type Report struct {
	// Baseline: plain function-call overhead with no uprobes attached.
	// This is the reference point for computing uprobe overhead.
	Baseline Summary `json:"baseline"`

	// Kernel-mode scenarios.
	Hit  Summary `json:"hit,omitempty"`
	Idle Summary `json:"idle,omitempty"`
	Miss Summary `json:"miss,omitempty"`

	// Userspace-mode (bpftime) scenarios.
	HitUserspace  Summary `json:"hit_userspace,omitempty"`
	IdleUserspace Summary `json:"idle_userspace,omitempty"`
	MissUserspace Summary `json:"miss_userspace,omitempty"`

	// Overheads holds pairwise relative overhead comparisons between scenarios.
	Overheads Overheads `json:"overheads"`
}

// Overhead holds the slowdown of scenario a relative to scenario b,
// expressed as a multiplier: 2.0 means a takes twice as long as b.
type Overhead struct {
	Mean float64 `json:"mean_x"`
	P50  float64 `json:"p50_x"`
	P99  float64 `json:"p99_x"`
}

// Overheads holds pairwise relative overhead comparisons between scenarios.
type Overheads struct {
	IdleVsBaseline Overhead `json:"idle_vs_baseline,omitempty"`
	HitVsBaseline  Overhead `json:"hit_vs_baseline,omitempty"`
	HitVsIdle      Overhead `json:"hit_vs_idle,omitempty"`
	MissVsBaseline Overhead `json:"miss_vs_baseline,omitempty"`
	MissVsIdle     Overhead `json:"miss_vs_idle,omitempty"`
	MissVsHit      Overhead `json:"miss_vs_hit,omitempty"`

	// Userspace pairwise overheads.
	HitUserspaceVsBaseline  Overhead `json:"hit_userspace_vs_baseline,omitempty"`
	IdleUserspaceVsBaseline Overhead `json:"idle_userspace_vs_baseline,omitempty"`
	MissUserspaceVsBaseline Overhead `json:"miss_userspace_vs_baseline,omitempty"`

	// Kernel vs userspace comparisons (kernel / userspace multiplier).
	HitKernelVsUserspace  Overhead `json:"hit_kernel_vs_userspace,omitempty"`
	MissKernelVsUserspace Overhead `json:"miss_kernel_vs_userspace,omitempty"`
}

// relOverhead returns the element-wise slowdown multiplier a / b.
// Returns a zero Overhead when b is empty (no samples collected).
func relOverhead(a, b Summary) Overhead {
	if b.N == 0 {
		return Overhead{}
	}
	return Overhead{
		Mean: round2(ratio(a.Mean, b.Mean)),
		P50:  round2(ratio(a.P50, b.P50)),
		P99:  round2(ratio(a.P99, b.P99)),
	}
}

// ratio returns a / b, or 0 if b is zero.
func ratio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// Summary holds per-scenario descriptive statistics computed from the
// ns/call samples collected across benchmark iterations.
type Summary struct {
	// Mean is the arithmetic mean of all samples (ns/call).
	Mean float64 `json:"mean_ns"`
	// P50 is the median latency (50th percentile, ns/call).
	P50 float64 `json:"p50_ns"`
	// P99 is the 99th-percentile tail latency (ns/call).
	P99 float64 `json:"p99_ns"`
	// N is the number of samples collected.
	N int `json:"samples"`
}

// summarise computes descriptive statistics from a slice of ns/call samples.
func summarise(samples []float64) Summary {
	if len(samples) == 0 {
		return Summary{}
	}

	sorted := make([]float64, len(samples))
	copy(sorted, samples)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}

	return Summary{
		Mean: round2(sum / float64(len(sorted))),
		P50:  round2(percentile(sorted, 50)),
		P99:  round2(percentile(sorted, 99)),
		N:    len(sorted),
	}
}

// percentile returns the p-th percentile from an already-sorted slice
// using the ceiling-rank method.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

// round2 rounds v to two decimal places.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// reportPath returns the path to write the benchmark report to.
// It honours the BENCH_REPORT_PATH environment variable so that the Makefile
// can direct kernel and userspace runs to separate files without any Go-side
// flag parsing.
func reportPath() string {
	if p := os.Getenv("BENCH_REPORT_PATH"); p != "" {
		return p
	}
	return "bench-report.json"
}

// writeReport serialises the report as indented JSON to the given path.
func writeReport(path string, r *Report) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
