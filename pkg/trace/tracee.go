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
		if !errors.Is(err, ErrNoSymbolTable) {
			return errors.Wrap(err, "failed to resolve functions")
		}
		t.logger.Info().Msg("binary is stripped, falling back to binary recovery")
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

// GetFuncProbes returns the uprobe attach offsets and their corresponding
// cookies, collected in a single pass over the function map so that offsets[i]
// and cookies[i] always describe the same function.
//
// Callers attaching a uprobe_multi link (which consumes the offsets and cookies
// as parallel arrays) must use this method. Pairing the results of
// GetFuncOffsets and GetFuncCookies is unsafe: each ranges the map
// independently and Go randomizes map iteration order, so the two slices may
// describe the functions in different orders.
func (t *UserTracee) GetFuncProbes() (offsets, cookies []uint64) {
	offsets = make([]uint64, 0, len(t.funcs))
	cookies = make([]uint64, 0, len(t.funcs))
	for c, fn := range t.funcs {
		offsets = append(offsets, fn.offset)
		cookies = append(cookies, uint64(c))
	}
	return offsets, cookies
}

// GetFuncOffsets returns the attach offsets of all collected functions, in
// unspecified order. To attach probes, use GetFuncProbes instead, which keeps
// offsets and cookies aligned.
func (t *UserTracee) GetFuncOffsets() []uint64 {
	offsets := make([]uint64, 0, len(t.funcs))
	for c := range t.funcs {
		offsets = append(offsets, t.funcs[c].offset)
	}
	return offsets
}

// GetFuncCookies returns the cookies of all collected functions, in unspecified
// order. To attach probes, use GetFuncProbes instead, which keeps offsets and
// cookies aligned.
func (t *UserTracee) GetFuncCookies() []uint64 {
	cookies := make([]uint64, 0, len(t.funcs))
	for c := range t.funcs {
		cookies = append(cookies, uint64(c))
	}
	return cookies
}

func (t *UserTracee) GetFuncNames() []string {
	names := make([]string, 0, len(t.funcs))
	for c := range t.funcs {
		names = append(names, t.funcs[c].name)
	}
	return names
}
