package coverage

import (
	"errors"
	"sort"
)

// ErrNoReports is returned when Merge is called without any report.
var ErrNoReports = errors.New("no reports to merge")

// Merge combines multiple coverage reports into a single aggregate report.
//
// Reports are merged by function name: a function is traced in the result if it
// was traced in any input, and acknowledged (covered) if it was acknowledged in
// any input. Coverage is recomputed as the ratio of the merged acknowledged set
// to the merged traced set, as a percentage.
//
// Function identity is the symbol name emitted by xcover, so the inputs must
// come from the same binary (or binaries sharing a symbol namespace); merging
// reports from unrelated binaries yields meaningless numbers. Merge preserves a
// single ExePath only when every input agrees on it, so callers can detect a
// cross-binary merge by an empty ExePath on the result.
func Merge(reports ...*CoverageReport) (*CoverageReport, error) {
	if len(reports) == 0 {
		return nil, ErrNoReports
	}

	tracedSet := make(map[string]struct{})
	ackSet := make(map[string]struct{})
	exePaths := make(map[string]struct{})

	for _, r := range reports {
		if r == nil {
			continue
		}
		for _, f := range r.FuncsTraced {
			tracedSet[f] = struct{}{}
		}
		for _, f := range r.FuncsAck {
			ackSet[f] = struct{}{}
		}
		if r.ExePath != "" {
			exePaths[r.ExePath] = struct{}{}
		}
	}

	traced := sortedKeys(tracedSet)
	ack := sortedKeys(ackSet)

	var cov float64
	if len(traced) > 0 {
		cov = float64(len(ack)) / float64(len(traced)) * 100
	}

	merged := NewCoverageReport(
		WithReportFuncsTraced(traced),
		WithReportFuncsAck(ack),
		WithReportFuncsCov(cov),
	)

	if len(exePaths) == 1 {
		merged.ExePath = sortedKeys(exePaths)[0]
	}

	return merged, nil
}

// ExePaths returns the distinct, sorted ExePath values across the reports,
// letting callers warn when a merge spans more than one binary.
func ExePaths(reports ...*CoverageReport) []string {
	set := make(map[string]struct{})
	for _, r := range reports {
		if r != nil && r.ExePath != "" {
			set[r.ExePath] = struct{}{}
		}
	}

	return sortedKeys(set)
}

func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}
