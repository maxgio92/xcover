package trace

import (
	"io"

	log "github.com/rs/zerolog"
)

type UserTracerOptions struct {
	cookiesMapName string

	report  bool
	status  bool
	verbose bool
	writer  io.Writer

	logger log.Logger
}

type UserTracerOpt func(*UserTracer)

func WithTracerLogger(logger log.Logger) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.logger = logger
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
