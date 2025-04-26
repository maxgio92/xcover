package cmd

import (
	"context"

	log "github.com/rs/zerolog"
)

type CommonOptions struct {
	Ctx          context.Context
	Logger       log.Logger
	Probe        []byte
	ProbeObjName string
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

func WithProbeObjName(name string) Option {
	return func(o *CommonOptions) {
		o.ProbeObjName = name
	}
}

func WithProbe(probe []byte) Option {
	return func(o *CommonOptions) {
		o.Probe = probe
	}
}
