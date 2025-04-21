package trace

import (
	"encoding/json"
	"io"
)

type UserTraceReport struct {
	FuncsTraced []string `json:"func_syms_traced"`
	FuncsAck    []string `json:"func_syms_ack"`
	CovByFunc   float64  `json:"cov_by_func"`
}

type UserTraceReportOption func(*UserTraceReport)

func NewReport(opts ...UserTraceReportOption) *UserTraceReport {
	report := new(UserTraceReport)
	for _, opt := range opts {
		opt(report)
	}

	return report
}

func WithReportFuncsTraced(traced []string) UserTraceReportOption {
	return func(o *UserTraceReport) {
		o.FuncsTraced = traced
	}
}

func WithReportFuncsAck(ack []string) UserTraceReportOption {
	return func(o *UserTraceReport) {
		o.FuncsAck = ack
	}
}

func WithReportFuncsCov(cov float64) UserTraceReportOption {
	return func(o *UserTraceReport) {
		o.CovByFunc = cov
	}
}

func (r *UserTraceReport) WriteReport(w io.Writer) error {
	encoder := json.NewEncoder(w)
	return encoder.Encode(r)
}
