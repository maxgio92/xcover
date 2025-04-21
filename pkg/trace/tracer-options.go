package trace

import (
	log "github.com/rs/zerolog"
)

type UserTracerOptions struct {
	bpfModPath     string
	bpfProgName    string
	cookiesMapName string
	evtRingBufName string

	report  bool
	status  bool
	verbose bool

	logger *log.Logger
}

type UserTracerOpt func(*UserTracer)

func WithTracerBpfModPath(bpfModPath string) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.bpfModPath = bpfModPath
	}
}

func WithTracerBpfProgName(bpfProgName string) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.bpfProgName = bpfProgName
	}
}

func WithTracerLogger(logger *log.Logger) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.logger = logger
	}
}

func WithTracerCookiesMapName(cookiesMapName string) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.cookiesMapName = cookiesMapName
	}
}

func WithTracerEvtRingBufName(evtRingBufName string) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.evtRingBufName = evtRingBufName
	}
}

func WithTracerReport(report bool) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.report = report
	}
}

func WithTracerVerbose(verbose bool) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.verbose = verbose
	}
}

func WithTracerStatus(status bool) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.status = status
	}
}
