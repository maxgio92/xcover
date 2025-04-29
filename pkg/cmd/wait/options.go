package wait

import (
	"context"
	"time"

	log "github.com/rs/zerolog"

	"github.com/maxgio92/xcover/pkg/cmd/options"
)

type Options struct {
	socketPath string
	timeout    time.Duration

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
