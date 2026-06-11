package trace

import (
	"context"
	"debug/elf"

	"github.com/pkg/errors"
)

type UserTracee struct {
	funcs map[cookie]funcInfo
	*UserTraceeOptions
}

type cookie uint64

type funcInfo struct {
	name   string
	offset uint64
}

func NewUserTracee(opts ...UserTraceeOption) *UserTracee {
	tracee := &UserTracee{
		UserTraceeOptions: &UserTraceeOptions{},
		funcs:             make(map[cookie]funcInfo, 0),
	}
	for _, opt := range opts {
		opt(tracee)
	}
	return tracee
}

func (t *UserTracee) Init(ctx context.Context) error {
	if err := t.validate(); err != nil {
		return err
	}
	t.logger = t.logger.With().Str("component", "tracee").Logger()

	t.logger.Info().
		Str("exe_path", t.exePath).
		Str("include", t.symPatternInclude).
		Str("exclude", t.symPatternExclude).
		Msg("collecting functions")

	resolver := t.resolver
	if resolver == nil {
		resolver = t.defaultResolver()
	}

	entries, err := resolver(ctx)
	if err != nil {
		if !errors.Is(err, ErrNoSymbolTable) {
			return errors.Wrap(err, "failed to resolve functions")
		}
		t.logger.Info().Msg("binary is stripped, falling back to binary recovery")
		entries, err = RecoveryResolver(t.exePath, t.logger)(ctx)
		if err != nil {
			return errors.Wrap(err, "failed to resolve functions")
		}
	}

	// Key by file offset, which is unique per function location, rather than by
	// a hash of the name. Distinct functions can share a name (C statics in
	// different translation units, C++ overloads sharing a DWARF DW_AT_name),
	// and name-keying would silently drop all but one of them. Functions that
	// share an offset (weak aliases, identical-code folding) are genuinely the
	// same code and collapse to a single entry.
	for _, e := range entries {
		t.funcs[cookie(e.Offset)] = funcInfo{
			name:   e.Name,
			offset: e.Offset,
		}
	}

	t.logger.Info().
		Int("count", len(t.funcs)).
		Msg("functions collected")

	return nil
}

// defaultResolver returns the appropriate FunctionResolver for the tracee's
// configured scope. If scope is empty, it defaults to ScopeBinary.
//
// When ScopeProject is requested, the resolver automatically falls back to
// binary scope if the binary does not carry the metadata required for project
// scoping (e.g. missing Go build info, built as command-line-arguments). A
// warning is logged in that case.
func (t *UserTracee) defaultResolver() FunctionResolver {
	binary := SymbolTableResolver(t.exePath, t.logger, t.symPatternInclude, t.symPatternExclude, t.symBindInclude, t.symBindExclude)
	switch t.scope {
	case ScopeProject:
		project := GoProjectResolver(t.exePath, t.logger, t.symPatternInclude, t.symPatternExclude, t.symBindInclude, t.symBindExclude)
		return withProjectFallback(project, binary, t.logger)
	default:
		return binary
	}
}

func (t *UserTracee) validate() error {
	if t.exePath == "" {
		return ErrExePathEmpty
	}
	return nil
}

// ShouldIncludeSymbol reports whether sym passes the tracee's include/exclude filters.
// Kept for backward compatibility; prefer the package-level shouldInclude for new code.
func (t *UserTracee) ShouldIncludeSymbol(sym elf.Symbol) bool {
	return shouldInclude(sym, t.symPatternInclude, t.symPatternExclude, t.symBindInclude, t.symBindExclude)
}

// GetFuncProbes returns the uprobe attach offsets and their corresponding
// cookies, collected in a single pass over the function map so that offsets[i]
// and cookies[i] always describe the same function. Callers attaching a
// uprobe_multi link consume the two slices as parallel arrays.
func (t *UserTracee) GetFuncProbes() (offsets, cookies []uint64) {
	offsets = make([]uint64, 0, len(t.funcs))
	cookies = make([]uint64, 0, len(t.funcs))
	for c, fn := range t.funcs {
		offsets = append(offsets, fn.offset)
		cookies = append(cookies, uint64(c))
	}
	return offsets, cookies
}

func (t *UserTracee) GetFuncNames() []string {
	names := make([]string, 0, len(t.funcs))
	for c := range t.funcs {
		names = append(names, t.funcs[c].name)
	}
	return names
}
