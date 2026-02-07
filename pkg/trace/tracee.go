package trace

import (
	"debug/elf"
	"regexp"

	"github.com/pkg/errors"
)

type UserTracee struct {
	file  *elf.File
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

func (t *UserTracee) Init() error {
	var err error
	if err = t.validate(); err != nil {
		return err
	}
	t.logger = t.logger.With().Str("component", "tracee").Logger()

	t.logger.Info().
		Str("exe_path", t.exePath).
		Str("include", t.symPatternInclude).
		Str("exclude", t.symPatternExclude).
		Msg("collecting functions")

	t.file, err = elf.Open(t.exePath)
	if err != nil {
		return errors.Wrap(err, "filed to open elf file")
	}
	if t.file == nil {
		return ErrElfFileNil
	}

	if err = t.loadFunctions(); err != nil {
		// Note: With gopclntab fallback, we should be able to handle stripped Go binaries
		// Fail fast only if we still can't load any functions after trying all methods
		if errors.Is(err, elf.ErrNoSymbols) || errors.Is(err, ErrNoFunctionSymbols) {
			return err
		}
		t.logger.Warn().Err(err).Msg("failed to load functions")
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

func (t *UserTracee) loadFunctions() error {
	funcSyms, err := t.getFuncSyms()
	if err != nil {
		// If the binary is stripped and has no symbols, try loading from .gopclntab
		// for Go binaries
		if errors.Is(err, elf.ErrNoSymbols) {
			t.logger.Info().Msg("binary is stripped, attempting to load symbols from .gopclntab")
			if goPclnErr := t.loadFunctionsFromGoPclntab(); goPclnErr != nil {
				t.logger.Debug().Err(goPclnErr).Msg("failed to load from .gopclntab")
				return err // Return original error
			}
			// Successfully loaded from gopclntab
			return nil
		}
		return err
	}
	if len(funcSyms) == 0 {
		// Try gopclntab as fallback when no function symbols found
		t.logger.Info().Msg("no function symbols found, attempting to load from .gopclntab")
		if goPclnErr := t.loadFunctionsFromGoPclntab(); goPclnErr != nil {
			t.logger.Debug().Err(goPclnErr).Msg("failed to load from .gopclntab")
			return ErrNoFunctionSymbols
		}
		return nil
	}

	return t.loadFunctionsFromSymbols(funcSyms)
}

func (t *UserTracee) getFuncSyms() ([]elf.Symbol, error) {
	var funcSyms []elf.Symbol
	if t.file == nil {
		return nil, ErrElfFileNil
	}
	syms, err := t.file.Symbols()
	if err != nil {
		return nil, err
	}

	for _, sym := range syms {
		// Exclude non-function symbols.
		if elf.ST_TYPE(sym.Info) != elf.STT_FUNC {
			continue
		}

		if !t.ShouldIncludeSymbol(sym) {
			continue
		}

		funcSyms = append(funcSyms, sym)
	}

	return funcSyms, nil
}

func (t *UserTracee) ShouldIncludeSymbol(sym elf.Symbol) bool {
	// Exclude symbols with specific bind.
	if t.symBindExclude != nil {
		for _, bind := range t.symBindExclude {
			if elf.ST_BIND(sym.Info) == bind {
				return false
			}
		}
	}
	// Include only symbols with specific bind.
	if t.symBindInclude != nil {
		for _, bind := range t.symBindInclude {
			if elf.ST_BIND(sym.Info) == bind {
				return true
			}
		}
		return false
	}
	// Exclude symbols that match a specific regex pattern.
	if t.symPatternExclude != "" {
		if regexp.MustCompile(t.symPatternExclude).MatchString(sym.Name) {
			return false
		}
	}
	// Include only symbols that match a specific regex pattern.
	if t.symPatternInclude != "" {
		if regexp.MustCompile(t.symPatternInclude).MatchString(sym.Name) {
			return true
		}
		return false
	}

	return true
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
	for cookie := range t.funcs {
		cookies = append(cookies, uint64(cookie))
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
