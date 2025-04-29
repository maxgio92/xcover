package profile

import (
	"context"

	log "github.com/rs/zerolog"

	"github.com/maxgio92/xcover/pkg/cmd/options"
)

type Options struct {
	Probe        []byte
	ProbeObjName string

	comm string
	pid  int

	symExcludePattern string
	symIncludePattern string

	verbose bool
	report  bool
	status  bool

	*options.CommonOptions
}

type Option func(o *Options)

func NewOptions(opts ...Option) *Options {
	o := new(Options)
	o.CommonOptions = new(options.CommonOptions)

	for _, f := range opts {
		f(o)
	}

	return o
}

func WithProbeObjName(name string) Option {
	return func(o *Options) {
		o.ProbeObjName = name
	}
}

func WithProbe(probe []byte) Option {
	return func(o *Options) {
		o.Probe = probe
	}
}

func WithContext(ctx context.Context) Option {
	return func(o *Options) {
		o.Ctx = ctx
	}
}

func WithLogger(logger log.Logger) Option {
	return func(o *Options) {
		o.Logger = logger
	}
}

func WithLogLevel(level string) Option {
	return func(o *Options) {
		o.LogLevel = level
	}
}