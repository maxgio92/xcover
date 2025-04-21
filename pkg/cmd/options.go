package cmd

import (
	"context"

	log "github.com/rs/zerolog"
)

type CommonOptions struct {
	Ctx       context.Context
	Logger    log.Logger
	Probe     []byte
	ProbePath string
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

func WithLogger(logger log.Logger) Option {
	return func(o *CommonOptions) {
		o.Logger = logger
	}
}

func WithProbePath(probePath string) Option {
	return func(o *CommonOptions) {
		o.ProbePath = probePath
	}
}
