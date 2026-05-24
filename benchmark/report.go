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
type Report struct {
	// Baseline: plain function-call overhead with no uprobes attached.
	// This is the reference point for computing uprobe overhead.
	Baseline Summary `json:"baseline"`
	// Idle: overhead on an unprobed function while a probe is attached to a
	// different function in the same binary. Expected to be ≈ Baseline.
	Idle Summary `json:"idle"`
	// Hit: uprobe overhead on the steady-state path — the probed function's
	// cookie is already in seen_funcs, so firings take the fast map-lookup path.
	Hit Summary `json:"hit"`
	// Miss: uprobe overhead on the cold path — every function is called once,
	// so every firing hits the slow path (map update + ringbuf submit).
	Miss Summary `json:"miss"`
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
	IdleVsBaseline Overhead `json:"idle_vs_baseline"`
	HitVsBaseline  Overhead `json:"hit_vs_baseline"`
	HitVsIdle      Overhead `json:"hit_vs_idle"`
	MissVsBaseline Overhead `json:"miss_vs_baseline"`
	MissVsIdle     Overhead `json:"miss_vs_idle"`
	MissVsHit      Overhead `json:"miss_vs_hit"`
}

// relOverhead returns the element-wise slowdown multiplier a / b.
func relOverhead(a, b Summary) Overhead {
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
