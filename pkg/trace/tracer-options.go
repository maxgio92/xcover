package trace

import (
	log "github.com/rs/zerolog"
	"io"
)

type UserTracerOptions struct {
	bpfModPath     string
	bpfProgName    string
	cookiesMapName string
	evtRingBufName string

	report  bool
	status  bool
	verbose bool
	writer  io.Writer

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

func WithTracerWriter(w io.Writer) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.writer = w
	}
}

func WithTracerTracee(tracee *UserTracee) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.tracee = tracee
	}
}
