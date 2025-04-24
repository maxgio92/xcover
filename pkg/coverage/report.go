package coverage

import (
	"encoding/json"
	"io"
)

type CoverageReport struct {
	FuncsTraced []string `json:"funcs_traced"`
	FuncsAck    []string `json:"funcs_ack"`
	CovByFunc   float64  `json:"cov_by_func"`
	ExePath     string   `json:"exe_path"`
}

type CoverageReportOption func(*CoverageReport)

func NewCoverageReport(opts ...CoverageReportOption) *CoverageReport {
	report := new(CoverageReport)
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

func (r *CoverageReport) WriteReport(w io.Writer) error {
	encoder := json.NewEncoder(w)
	return encoder.Encode(r)
}
