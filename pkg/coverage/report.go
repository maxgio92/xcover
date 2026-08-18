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

// ReadReport decodes a CoverageReport from r, as written by WriteReport.
func ReadReport(r io.Reader) (*CoverageReport, error) {
	report := new(CoverageReport)
	if err := json.NewDecoder(r).Decode(report); err != nil {
		return nil, err
	}

	return report, nil
}
