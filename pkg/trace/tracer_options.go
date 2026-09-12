package trace

import (
	"io"

	log "github.com/rs/zerolog"
)

type UserTracerOptions struct {
	cookiesMapName string

	// pid restricts tracing to one process; probe.PIDAll traces every process.
	pid int

	report       bool
	status       bool
	verbose      bool
	userspaceBPF bool
	writer       io.Writer

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

func WithTracerUserspaceBPF(enabled bool) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.userspaceBPF = enabled
	}
}

// WithTracerPID restricts tracing to the process with the given PID. The
// default, probe.PIDAll, traces every process running the executable.
func WithTracerPID(pid int) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.pid = pid
	}
}
