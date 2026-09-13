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
	// Symbols maps a raw name from FuncsTraced or FuncsAck to its demangled
	// C++ or Rust form. Only names that differ are listed, so the field is
	// absent for Go and C binaries.
	Symbols map[string]string `json:"symbols,omitempty"`
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

func WithReportSymbols(symbols map[string]string) CoverageReportOption {
	return func(o *CoverageReport) {
		o.Symbols = symbols
	}
}

func (r *CoverageReport) WriteReport(w io.Writer) error {
	encoder := json.NewEncoder(w)
	return encoder.Encode(r)
}
