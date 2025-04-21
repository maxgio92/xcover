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

func WithBpfModPath(bpfModPath string) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.bpfModPath = bpfModPath
	}
}

func WithBpfProgName(bpfProgName string) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.bpfProgName = bpfProgName
	}
}

func WithLogger(logger *log.Logger) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.logger = logger
	}
}

func WithCookiesMapName(cookiesMapName string) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.cookiesMapName = cookiesMapName
	}
}

func WithEvtRingBufName(evtRingBufName string) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.evtRingBufName = evtRingBufName
	}
}

func WithReport(report bool) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.report = report
	}
}

func WithVerbose(verbose bool) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.verbose = verbose
	}
}

func WithStatus(status bool) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.status = status
	}
}
