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
	// ErrBuildIDMismatch is returned when the inputs do not share a build_id.
	ErrBuildIDMismatch = errors.New("build_id mismatch")
	// ErrBuildIDMissing is returned when an input has no build_id, so the
	// merge cannot verify that every report measured the same binary.
	ErrBuildIDMissing = errors.New("build_id missing")
)

type mergeConfig struct {
	allowMismatchedBuildID bool
	now                    func() time.Time
}

// MergeOption configures Merge.
type MergeOption func(*mergeConfig)

// WithAllowMismatchedBuildID lets Merge combine reports whose build_id
// values differ or are empty. The merged report carries an empty build_id,
// which stays empty across further merges since it can no longer be verified.
func WithAllowMismatchedBuildID() MergeOption {
	return func(c *mergeConfig) {
		c.allowMismatchedBuildID = true
	}
}

// WithClock overrides the time source used for generated_at.
func WithClock(now func() time.Time) MergeOption {
	return func(c *mergeConfig) {
		c.now = now
	}
}

// Merge combines the reports into one. A function is identified by its
// offset within the binary named by build_id: it is hit in the result if it
// was hit in any input. funcs_traced, funcs_ack and cov_by_func are derived
// from the merged functions list, so duplicate names at different offsets are
// preserved. exe_path and kernel are kept only when every input agrees.
//
// Merge is associative apart from generated_at: Merge(Merge(A,B),C) equals
// Merge(A,B,C).
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

	buildID, err := mergeBuildID(reports, cfg.allowMismatchedBuildID)
	if err != nil {
		return nil, err
	}

	byOffset := make(map[uint64]FunctionCoverage)
	for _, r := range reports {
		for _, fn := range r.Functions {
			prev, ok := byOffset[fn.Offset]
			if ok && prev.Name != fn.Name {
				return nil, errors.Errorf("offset %d is %q in one report and %q in another", fn.Offset, prev.Name, fn.Name)
			}
			byOffset[fn.Offset] = FunctionCoverage{Name: fn.Name, Offset: fn.Offset, Hit: prev.Hit || fn.Hit}
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

// mergeBuildID returns the build_id shared by every report. Without
// allowMismatch, an empty or differing build_id is an error; with it, the
// result is empty unless all inputs agree on a non-empty value.
func mergeBuildID(reports []*CoverageReport, allowMismatch bool) (string, error) {
	for _, r := range reports {
		if r.BuildID != "" {
			continue
		}
		if !allowMismatch {
			return "", errors.Wrap(ErrBuildIDMissing, "cannot verify the reports measure the same binary")
		}
		return "", nil
	}

	common := commonValue(reports, func(r *CoverageReport) string { return r.BuildID })
	if common == "" && !allowMismatch {
		return "", errors.Wrapf(ErrBuildIDMismatch, "%q vs %q", reports[0].BuildID, firstDifferent(reports, reports[0].BuildID))
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

func firstDifferent(reports []*CoverageReport, buildID string) string {
	for _, r := range reports {
		if r.BuildID != buildID {
			return r.BuildID
		}
	}

	return ""
}
