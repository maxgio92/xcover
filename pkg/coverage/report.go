package coverage

import (
	"encoding/json"
	"io"
)

// SchemaVersion identifies the report layout. Bump it when a field changes
// meaning or is removed; adding fields is backward compatible.
const SchemaVersion = 1

// FunctionCoverage is the per-function entry of the report.
type FunctionCoverage struct {
	Name   string `json:"name"`
	Offset uint64 `json:"offset"`
	Hit    bool   `json:"hit"`
}

type CoverageReport struct {
	SchemaVersion int                `json:"schema_version"`
	XcoverVersion string             `json:"xcover_version"`
	GeneratedAt   string             `json:"generated_at"`
	Kernel        string             `json:"kernel"`
	ExePath       string             `json:"exe_path"`
	BuildID       string             `json:"build_id"`
	FuncsTraced   []string           `json:"funcs_traced"`
	FuncsAck      []string           `json:"funcs_ack"`
	CovByFunc     float64            `json:"cov_by_func"`
	Functions     []FunctionCoverage `json:"functions"`
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
