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

func WithTraceeLogger(logger log.Logger) UserTraceeOption {
	return func(o *UserTracee) {
		o.logger = logger
	}
}
