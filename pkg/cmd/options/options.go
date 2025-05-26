package options

import (
	"context"

	log "github.com/rs/zerolog"
)

type Options struct {
	Ctx      context.Context
	Logger   log.Logger
	LogLevel string
}

type Option func(o *Options)

func NewOptions(opts ...Option) *Options {
	o := new(Options)

	for _, f := range opts {
		f(o)
	}

	return o
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
