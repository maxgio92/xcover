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

	logger *log.Logger
}

type UserTraceeOption func(*UserTracee)

func WithExePath(path string) UserTraceeOption {
	return func(o *UserTracee) {
		o.exePath = path
	}
}

func WithSymPatternInclude(patternInclude string) UserTraceeOption {
	return func(o *UserTracee) {
		o.symPatternInclude = patternInclude
	}
}

func WithSymPatternExclude(patternExclude string) UserTraceeOption {
	return func(o *UserTracee) {
		o.symPatternExclude = patternExclude
	}
}

func WithSymBindInclude(symBind ...elf.SymBind) UserTraceeOption {
	return func(o *UserTracee) {
		o.symBindInclude = symBind
	}
}

func WithSymBindExclude(symBind ...elf.SymBind) UserTraceeOption {
	return func(o *UserTracee) {
		o.symBindExclude = symBind
	}
}

func WithTraceeLogger(logger *log.Logger) UserTraceeOption {
	return func(o *UserTracee) {
		o.logger = logger
	}
}
