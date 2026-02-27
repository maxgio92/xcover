package trace

import (
	"debug/elf"
	log "github.com/rs/zerolog"
)

type UserTraceeOptions struct {
	exePath           string
	symPatternInclude string
	symPatternExclude string
	symBindInclude    []elf.SymBind
	symBindExclude    []elf.SymBind

	// resolver is the strategy used to resolve functions from the binary.
	// If nil, SymbolTableResolver is used as the default.
	resolver FunctionResolver

	logger log.Logger
}

type UserTraceeOption func(*UserTracee)

func WithTraceeExePath(path string) UserTraceeOption {
	return func(o *UserTracee) {
		o.exePath = path
	}
}

func WithTraceeSymPatternInclude(patternInclude string) UserTraceeOption {
	return func(o *UserTracee) {
		o.symPatternInclude = patternInclude
	}
}

func WithTraceeSymPatternExclude(patternExclude string) UserTraceeOption {
	return func(o *UserTracee) {
		o.symPatternExclude = patternExclude
	}
}

func WithTraceeSymBindInclude(symBind ...elf.SymBind) UserTraceeOption {
	return func(o *UserTracee) {
		o.symBindInclude = symBind
	}
}

func WithTraceeSymBindExclude(symBind ...elf.SymBind) UserTraceeOption {
	return func(o *UserTracee) {
		o.symBindExclude = symBind
	}
}

func WithTraceeResolver(r FunctionResolver) UserTraceeOption {
	return func(o *UserTracee) {
		o.resolver = r
	}
}

func WithTraceeLogger(logger log.Logger) UserTraceeOption {
	return func(o *UserTracee) {
		o.logger = logger
	}
}
