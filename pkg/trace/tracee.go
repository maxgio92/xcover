package trace

import (
	"context"
	"debug/elf"

	"github.com/maxgio92/xcover/internal/utils"
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
		resolver = SymbolTableResolver(t.exePath, t.logger, t.symPatternInclude, t.symPatternExclude, t.symBindInclude, t.symBindExclude)
	}

	entries, err := resolver(ctx)
	if err != nil {
		t.logger.Info().Msg("symbol resolution failed, falling back to binary recovery")
		entries, err = RecoveryResolver(t.exePath, t.logger)(ctx)
		if err != nil {
			return errors.Wrap(err, "failed to resolve functions")
		}
	}

	for _, e := range entries {
		t.funcs[cookie(utils.Hash(e.Name))] = funcInfo{
			name:   e.Name,
			offset: e.Offset,
		}
	}

	t.logger.Info().
		Int("count", len(t.funcs)).
		Msg("functions collected")

	return nil
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

func (t *UserTracee) GetFuncOffsets() []uint64 {
	offsets := make([]uint64, len(t.funcs))
	for i := range t.funcs {
		offsets = append(offsets, t.funcs[i].offset)
	}
	return offsets
}

func (t *UserTracee) GetFuncCookies() []uint64 {
	cookies := make([]uint64, len(t.funcs))
	for c := range t.funcs {
		cookies = append(cookies, uint64(c))
	}
	return cookies
}

func (t *UserTracee) GetFuncNames() []string {
	names := make([]string, len(t.funcs))
	for i := range t.funcs {
		names = append(names, t.funcs[i].name)
	}
	return names
}
