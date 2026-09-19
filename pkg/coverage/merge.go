package coverage

import (
	"sort"
	"time"

	"github.com/pkg/errors"

	"github.com/maxgio92/xcover/internal/settings"
)

var (
	// ErrNoReports is returned when Merge is called without any report.
	ErrNoReports = errors.New("no reports to merge")
	// ErrNilReport is returned when one of the reports passed to Merge is nil.
	ErrNilReport = errors.New("nil report")
	// ErrBuildIDMismatch is returned when two inputs carry different non-empty
	// build_id values: they measured different binaries and cannot be merged.
	ErrBuildIDMismatch = errors.New("build_id mismatch")
	// ErrBuildIDMissing is returned when an input has no build_id, so the
	// merge cannot verify that every report measured the same binary.
	ErrBuildIDMissing = errors.New("build_id missing")
)

type mergeConfig struct {
	allowMissingBuildID bool
	now                 func() time.Time
}

// MergeOption configures Merge.
type MergeOption func(*mergeConfig)

// WithAllowMissingBuildID lets Merge combine reports when one of them has an
// empty build_id. The merged report carries an empty build_id, which stays
// empty across further merges since it can no longer be verified. Reports
// with different non-empty build_id values are still refused.
func WithAllowMissingBuildID() MergeOption {
	return func(c *mergeConfig) {
		c.allowMissingBuildID = true
	}
}

// withClock overrides the time source used for generated_at so tests get a
// fixed value. It is not part of the package API.
func withClock(now func() time.Time) MergeOption {
	return func(c *mergeConfig) {
		c.now = now
	}
}

// mergeFieldPolicy states what Merge does with every CoverageReport field:
// "merged" fields are combined from the inputs, "recomputed" fields are
// derived from the merged functions or set fresh, and "dropped" fields are
// left out of the result with the reason after the colon. Merge builds its
// result field by field at the end of the function and does not read this
// table. The table records the intended policy; TestMergeFieldPolicy fails
// when a CoverageReport field has no entry, so a new field cannot be
// forgotten, but the matching Merge logic still has to be written.
var mergeFieldPolicy = map[string]string{
	"SchemaVersion": "recomputed: the merged report is written in the schema of this build",
	"XcoverVersion": "recomputed: the version of the xcover that merged",
	"GeneratedAt":   "recomputed: the time of the merge",
	"Kernel":        "merged: kept when every input agrees, empty otherwise",
	"ExePath":       "merged: kept when every input agrees, empty otherwise",
	"BuildID":       "merged: shared by every input, empty when a missing one is allowed in",
	"FuncsTraced":   "recomputed: the names of the merged functions",
	"FuncsAck":      "recomputed: the names of the merged functions that were hit",
	"CovByFunc":     "recomputed: ack over traced of the merged functions",
	"Functions":     "merged: union by offset, hit is the OR of the inputs",
}

// Merge combines the reports into one. A function is identified by its
// offset within the binary named by build_id: it is hit in the result if it
// was hit in any input. funcs_traced, funcs_ack and cov_by_func are derived
// from the merged functions list, so duplicate names at different offsets are
// preserved. exe_path and kernel are kept only when every input agrees.
//
// Merge is associative apart from generated_at when every input carries the
// same non-empty build_id: Merge(Merge(A,B),C) equals Merge(A,B,C). Once a
// missing build_id is allowed in, grouping can change whether a merge
// succeeds, because an alias conflict is only tolerated under a verified id.
func Merge(reports []*CoverageReport, opts ...MergeOption) (*CoverageReport, error) {
	cfg := mergeConfig{now: time.Now}
	for _, opt := range opts {
		opt(&cfg)
	}

	if len(reports) == 0 {
		return nil, ErrNoReports
	}
	for i, r := range reports {
		if r == nil {
			return nil, errors.Wrapf(ErrNilReport, "report %d", i)
		}
	}

	buildID, err := mergeBuildID(reports, cfg.allowMissingBuildID)
	if err != nil {
		return nil, err
	}

	// Symbol aliases share an offset and the tracer keeps whichever name its
	// include pattern retained, so shards of the same verified binary can
	// legitimately disagree on the name at one offset: pick the smallest.
	// Without a verified build_id a name conflict is the only evidence that
	// the inputs measured different binaries, so it stays an error.
	byOffset := make(map[uint64]FunctionCoverage)
	for _, r := range reports {
		for _, fn := range r.Functions {
			prev, ok := byOffset[fn.Offset]
			name := fn.Name
			if ok && prev.Name != fn.Name {
				if buildID == "" {
					return nil, errors.Errorf("offset %d is %q in one report and %q in another", fn.Offset, prev.Name, fn.Name)
				}
				name = min(prev.Name, fn.Name)
			}
			byOffset[fn.Offset] = FunctionCoverage{Name: name, Offset: fn.Offset, Hit: prev.Hit || fn.Hit}
		}
	}

	functions := make([]FunctionCoverage, 0, len(byOffset))
	traced := make([]string, 0, len(byOffset))
	ack := make([]string, 0, len(byOffset))
	for _, fn := range byOffset {
		functions = append(functions, fn)
		traced = append(traced, fn.Name)
		if fn.Hit {
			ack = append(ack, fn.Name)
		}
	}
	sort.Slice(functions, func(i, j int) bool {
		if functions[i].Offset != functions[j].Offset {
			return functions[i].Offset < functions[j].Offset
		}
		return functions[i].Name < functions[j].Name
	})
	sort.Strings(traced)
	sort.Strings(ack)

	var cov float64
	if len(traced) > 0 {
		cov = float64(len(ack)) / float64(len(traced)) * 100
	}

	return NewCoverageReport(
		WithReportFuncsTraced(traced),
		WithReportFuncsAck(ack),
		WithReportFuncsCov(cov),
		WithReportFunctions(functions),
		WithReportBuildID(buildID),
		WithReportExePath(commonValue(reports, func(r *CoverageReport) string { return r.ExePath })),
		WithReportKernel(commonValue(reports, func(r *CoverageReport) string { return r.Kernel })),
		WithReportXcoverVersion(settings.Version),
		WithReportGeneratedAt(cfg.now().UTC().Format(time.RFC3339)),
	), nil
}

// mergeBuildID returns the build_id shared by every report. Two different
// non-empty values are always an error. An empty value is an error unless
// allowMissing is set, in which case the result is empty.
func mergeBuildID(reports []*CoverageReport, allowMissing bool) (string, error) {
	var common string
	missing := false
	for _, r := range reports {
		switch {
		case r.BuildID == "":
			if !allowMissing {
				return "", errors.Wrap(ErrBuildIDMissing, "cannot verify the reports measure the same binary")
			}
			missing = true
		case common == "":
			common = r.BuildID
		case r.BuildID != common:
			return "", errors.Wrapf(ErrBuildIDMismatch, "%q vs %q", common, r.BuildID)
		}
	}
	if missing {
		return "", nil
	}

	return common, nil
}

// commonValue returns the value of get when every report agrees on it, or
// the empty string otherwise.
func commonValue(reports []*CoverageReport, get func(*CoverageReport) string) string {
	first := get(reports[0])
	for _, r := range reports[1:] {
		if get(r) != first {
			return ""
		}
	}

	return first
}
