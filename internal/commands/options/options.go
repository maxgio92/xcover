package options

import (
	"context"

	log "github.com/rs/zerolog"
)

type CommonOptions struct {
	Ctx    context.Context
	Debug  bool
	Logger log.Logger
	Probe  []byte
}

type Option func(o *CommonOptions)

func NewCommonOptions(opts ...Option) *CommonOptions {
	o := new(CommonOptions)
	for _, f := range opts {
		f(o)
	}

	return o
}

func WithContext(ctx context.Context) Option {
	return func(o *CommonOptions) {
		o.Ctx = ctx
	}
}

func WithDebug(debug bool) Option {
	return func(o *CommonOptions) {
		o.Debug = debug
	}
}

func WithLogger(logger log.Logger) Option {
	return func(o *CommonOptions) {
		o.Logger = logger
	}
}

func WithProbe(probe []byte) Option {
	return func(o *CommonOptions) {
		o.Probe = probe
	}
}
