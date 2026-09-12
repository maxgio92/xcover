package trace

import (
	"io"

	log "github.com/rs/zerolog"
)

type UserTracerOptions struct {
	cookiesMapName string

	report       bool
	status       bool
	verbose      bool
	userspaceBPF bool
	pid          int
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

// WithTracerProbe injects the Probe implementation the tracer drives. When
// unset, Init builds the real libbpf-backed *probe.Probe sized for the
// tracee function count.
func WithTracerProbe(p Probe) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.probe = p
	}
}

// WithTracerPID restricts tracing to the given process. The default of -1
// traces every process executing the tracee binary.
func WithTracerPID(pid int) UserTracerOpt {
	return func(opts *UserTracer) {
		opts.pid = pid
	}
}
