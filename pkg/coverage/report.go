package coverage

import (
	"encoding/json"
	"io"

	"github.com/pkg/errors"
)

// SchemaVersion identifies the report layout. Bump it when a field changes
// meaning or is removed; adding fields is backward compatible.
const SchemaVersion = 1

// FunctionCoverage is the per-function entry of the report.
type FunctionCoverage struct {
	Name string `json:"name"`
	// Demangled is the human-readable form of a mangled C++ or Rust Name. It
	// is set only when it differs from Name, so Go and C entries omit it.
	Demangled string `json:"demangled,omitempty"`
	Offset    uint64 `json:"offset"`
	Hit       bool   `json:"hit"`
}

// UnmarshalJSON decodes a function record and rejects one whose offset or
// hit is missing or null. Left to the default decoder both would read as
// zero: the record would land at offset 0 and count as not hit, which is
// indistinguishable from a real function there. An explicit 0 and false are
// accepted. The name is checked by ReadReport, since it stays a string.
func (f *FunctionCoverage) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return errors.New("function record is null")
	}

	var record struct {
		Name   string  `json:"name"`
		Offset *uint64 `json:"offset"`
		Hit    *bool   `json:"hit"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return err
	}
	if record.Offset == nil {
		return errors.Errorf("function %q has no offset", record.Name)
	}
	if record.Hit == nil {
		return errors.Errorf("function %q has no hit", record.Name)
	}

	*f = FunctionCoverage{Name: record.Name, Offset: *record.Offset, Hit: *record.Hit}

	return nil
}

type CoverageReport struct {
	SchemaVersion int    `json:"schema_version"`
	XcoverVersion string `json:"xcover_version"`
	GeneratedAt   string `json:"generated_at"`
	Kernel        string `json:"kernel"`
	ExePath       string `json:"exe_path"`
	BuildID       string `json:"build_id"`
	// PID is the process the trace was restricted to; omitted when every
	// process executing ExePath was traced.
	PID         int                `json:"pid,omitempty"`
	FuncsTraced []string           `json:"funcs_traced"`
	FuncsAck    []string           `json:"funcs_ack"`
	CovByFunc   float64            `json:"cov_by_func"`
	Functions   []FunctionCoverage `json:"functions"`
}

type CoverageReportOption func(*CoverageReport)

func NewCoverageReport(opts ...CoverageReportOption) *CoverageReport {
	report := &CoverageReport{SchemaVersion: SchemaVersion}
	for _, opt := range opts {
		opt(report)
	}

	return report
}

func WithReportFuncsTraced(traced []string) CoverageReportOption {
	return func(o *CoverageReport) {
		o.FuncsTraced = traced
	}
}

func WithReportFuncsAck(ack []string) CoverageReportOption {
	return func(o *CoverageReport) {
		o.FuncsAck = ack
	}
}

func WithReportFuncsCov(cov float64) CoverageReportOption {
	return func(o *CoverageReport) {
		o.CovByFunc = cov
	}
}

func WithReportExePath(exePath string) CoverageReportOption {
	return func(o *CoverageReport) {
		o.ExePath = exePath
	}
}

// WithReportPID records the PID filter used for the trace. Values that are
// not positive mean no filter and leave the field unset.
func WithReportPID(pid int) CoverageReportOption {
	return func(o *CoverageReport) {
		if pid > 0 {
			o.PID = pid
		}
	}
}

func WithReportBuildID(buildID string) CoverageReportOption {
	return func(o *CoverageReport) {
		o.BuildID = buildID
	}
}

func WithReportKernel(kernel string) CoverageReportOption {
	return func(o *CoverageReport) {
		o.Kernel = kernel
	}
}

func WithReportXcoverVersion(version string) CoverageReportOption {
	return func(o *CoverageReport) {
		o.XcoverVersion = version
	}
}

func WithReportGeneratedAt(generatedAt string) CoverageReportOption {
	return func(o *CoverageReport) {
		o.GeneratedAt = generatedAt
	}
}

func WithReportFunctions(functions []FunctionCoverage) CoverageReportOption {
	return func(o *CoverageReport) {
		o.Functions = functions
	}
}

func (r *CoverageReport) WriteReport(w io.Writer) error {
	encoder := json.NewEncoder(w)
	return encoder.Encode(r)
}
