package trace

import (
	"context"
	"debug/elf"
	"debug/gosym"
	"fmt"
	"regexp"

	"github.com/maxgio92/resurgo"
	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
)

// FunctionEntry represents a function resolved from a binary, ready for uprobe attachment.
type FunctionEntry struct {
	Name   string
	Offset uint64
}

// FunctionResolver resolves the set of functions to trace from a binary.
// It is self-contained: it owns its own I/O and closes any resources it opens.
// ctx is checked between steps; cancellation aborts the resolution early.
type FunctionResolver func(ctx context.Context) ([]FunctionEntry, error)

// SymbolTableResolver returns a FunctionResolver backed by the ELF symbol table,
// with a .gopclntab fallback for stripped Go binaries.
// path is the binary to open; the resolver opens and closes it itself.
func SymbolTableResolver(path string, logger log.Logger, include, exclude string, bindInclude, bindExclude []elf.SymBind) FunctionResolver {
	return func(ctx context.Context) ([]FunctionEntry, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		filter, err := newSymFilter(include, exclude, bindInclude, bindExclude)
		if err != nil {
			return nil, err
		}

		f, err := elf.Open(path)
		if err != nil {
			return nil, errors.Wrap(err, "failed to open binary")
		}
		defer f.Close()

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		syms, err := funcSymsFromELF(f, filter)
		if err != nil {
			if errors.Is(err, elf.ErrNoSymbols) {
				logger.Info().Msg("binary is stripped, attempting .gopclntab fallback")
				entries, err := funcEntriesFromGoPclntab(f, filter, logger)
				if err != nil {
					return nil, errors.Wrap(ErrNoSymbolTable, err.Error())
				}
				return entries, nil
			}
			return nil, err
		}
		if len(syms) == 0 {
			logger.Info().Msg("no function symbols found, attempting .gopclntab fallback")
			entries, goPclnErr := funcEntriesFromGoPclntab(f, filter, logger)
			if goPclnErr != nil {
				return nil, ErrNoFunctionSymbols
			}
			return entries, nil
		}

		return funcEntriesFromSymbols(syms, func(va uint64) (uint64, error) {
			return vaToFileOffset(f, va)
		}, logger)
	}
}

// funcSymsFromELF returns filtered function symbols from the ELF symbol table.
func funcSymsFromELF(f *elf.File, filter symFilter) ([]elf.Symbol, error) {
	syms, err := f.Symbols()
	if err != nil {
		return nil, err
	}

	return filterFuncSyms(syms, filter), nil
}

// goTextMarkerRe matches the zero-size STT_FUNC symbols the Go linker emits to
// delimit code: runtime.text, runtime.text.N for split text sections, and
// runtime.etext (cmd/link/internal/ld/symtab.go). lld's etext/_etext are
// STT_NOTYPE and never reach this check.
var goTextMarkerRe = regexp.MustCompile(`^runtime\.(text(\.[0-9]+)?|etext)$`)

// filterFuncSyms keeps the STT_FUNC symbols that name code present in the file
// and pass filter.
//
// Undefined imports (e.g. puts@GLIBC_2.2.5: STT_FUNC, SHN_UNDEF, Value 0) are
// dropped: on a PIE the first PT_LOAD has Vaddr 0, so Value 0 would map to file
// offset 0 (the ELF header) and be probed as a function that can never fire.
// The Go linker's zero-size text markers are dropped by name. Every other
// zero-size symbol is kept: some toolchains emit assembly functions with Size
// 0, including ones at the very end of .text.
func filterFuncSyms(syms []elf.Symbol, filter symFilter) []elf.Symbol {
	var out []elf.Symbol
	for _, sym := range syms {
		if elf.ST_TYPE(sym.Info) != elf.STT_FUNC {
			continue
		}
		if !isDefinedFunc(sym) {
			continue
		}
		if isGoTextMarker(sym) {
			continue
		}
		if !filter.shouldInclude(sym) {
			continue
		}
		out = append(out, sym)
	}
	return out
}

// isGoTextMarker reports whether sym is one of the Go linker's zero-size
// text delimiters (see goTextMarkerRe).
func isGoTextMarker(sym elf.Symbol) bool {
	return sym.Size == 0 && goTextMarkerRe.MatchString(sym.Name)
}

// isDefinedFunc reports whether sym refers to code present in this file rather
// than an undefined import or a zero-address placeholder.
func isDefinedFunc(sym elf.Symbol) bool {
	return sym.Section != elf.SHN_UNDEF && sym.Value != 0
}

// funcEntriesFromSymbols converts a list of ELF function symbols to FunctionEntry values.
// toOffset converts sym.Value (a virtual address) to the file offset required for uprobe attachment.
func funcEntriesFromSymbols(syms []elf.Symbol, toOffset func(uint64) (uint64, error), logger log.Logger) ([]FunctionEntry, error) {
	var entries []FunctionEntry
	for _, sym := range syms {
		offset, err := toOffset(sym.Value)
		if err != nil {
			logger.Debug().Str("symbol", sym.Name).Err(err).Msg("failed to resolve file offset, skipping")
			continue
		}
		entries = append(entries, FunctionEntry{Name: sym.Name, Offset: offset})
	}
	if len(entries) == 0 {
		return nil, ErrNoOffsets
	}
	return entries, nil
}

// funcEntriesFromGoPclntab extracts function entries from the .gopclntab section,
// which is retained even in stripped Go binaries.
func funcEntriesFromGoPclntab(f *elf.File, filter symFilter, logger log.Logger) ([]FunctionEntry, error) {
	pclntabSection := f.Section(".gopclntab")
	if pclntabSection == nil {
		return nil, errors.New("no .gopclntab section found - not a Go binary or section stripped")
	}
	pclntabData, err := pclntabSection.Data()
	if err != nil {
		return nil, errors.Wrap(err, "failed to read .gopclntab section")
	}

	textSection := f.Section(".text")
	if textSection == nil {
		return nil, errors.New("no .text section found")
	}
	textAddr := textSection.Addr
	textOffset := textSection.Offset

	lineTable := gosym.NewLineTable(pclntabData, textAddr)
	if lineTable == nil {
		return nil, errors.New("failed to parse .gopclntab line table")
	}
	table, err := gosym.NewTable(nil, lineTable)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build symbol table from .gopclntab")
	}
	if len(table.Funcs) == 0 {
		return nil, errors.New("no functions found in .gopclntab")
	}

	var syms []elf.Symbol
	for _, fn := range table.Funcs {
		sym := elf.Symbol{
			Name:  fn.Name,
			Value: fn.Entry,
			Size:  fn.End - fn.Entry,
			Info:  byte(elf.STT_FUNC),
		}
		if filter.shouldInclude(sym) {
			syms = append(syms, sym)
		}
	}
	if len(syms) == 0 {
		return nil, ErrNoFunctionSymbols
	}

	return funcEntriesFromSymbols(syms, func(va uint64) (uint64, error) {
		return (va - textAddr) + textOffset, nil
	}, logger)
}

// vaToFileOffset converts a virtual address to a file offset using the
// binary's PT_LOAD program headers.
func vaToFileOffset(f *elf.File, va uint64) (uint64, error) {
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_LOAD {
			continue
		}
		if va >= prog.Vaddr && va < prog.Vaddr+prog.Filesz {
			return va - prog.Vaddr + prog.Off, nil
		}
	}
	return 0, fmt.Errorf("VA 0x%x not covered by any loadable segment", va)
}

// symFilter holds the compiled --include/--exclude patterns and the symbol
// binding filters. Patterns are compiled once here rather than per symbol:
// binaries can carry tens of thousands of function symbols, and an invalid
// pattern must surface as an error, not a panic inside the resolver.
type symFilter struct {
	include, exclude         *regexp.Regexp
	bindInclude, bindExclude []elf.SymBind
}

// newSymFilter compiles include and exclude (empty means unset). An invalid
// pattern returns an error wrapping ErrInvalidPattern.
func newSymFilter(include, exclude string, bindInclude, bindExclude []elf.SymBind) (symFilter, error) {
	f := symFilter{bindInclude: bindInclude, bindExclude: bindExclude}
	var err error
	if include != "" {
		if f.include, err = regexp.Compile(include); err != nil {
			return symFilter{}, errors.Wrapf(ErrInvalidPattern, "include %q: %s", include, err)
		}
	}
	if exclude != "" {
		if f.exclude, err = regexp.Compile(exclude); err != nil {
			return symFilter{}, errors.Wrapf(ErrInvalidPattern, "exclude %q: %s", exclude, err)
		}
	}
	return f, nil
}

// ValidateSymPatterns returns an error wrapping ErrInvalidPattern when include
// or exclude is not a valid regular expression; empty means unset. It lets a
// command reject a bad pattern up front, before any daemonization.
func ValidateSymPatterns(include, exclude string) error {
	_, err := newSymFilter(include, exclude, nil, nil)
	return err
}

// shouldInclude reports whether sym passes the include/exclude filters.
// Binding filters take precedence over name patterns, and exclude is checked
// before include.
func (f symFilter) shouldInclude(sym elf.Symbol) bool {
	if f.bindExclude != nil {
		for _, b := range f.bindExclude {
			if elf.ST_BIND(sym.Info) == b {
				return false
			}
		}
	}
	if f.bindInclude != nil {
		for _, b := range f.bindInclude {
			if elf.ST_BIND(sym.Info) == b {
				return true
			}
		}
		return false
	}
	if f.exclude != nil && f.exclude.MatchString(sym.Name) {
		return false
	}
	if f.include != nil {
		return f.include.MatchString(sym.Name)
	}
	return true
}

// RecoveryResolver returns a FunctionResolver backed by binary analysis via resurgo.
// It is used as a last resort when the ELF symbol table and .gopclntab are both unavailable
// (e.g. a fully stripped non-Go binary).
//
// Only candidates at ConfidenceHigh are accepted - those confirmed by compiler-emitted
// .eh_frame FDE entries. Functions have no symbol names; they are assigned synthetic
// names of the form func_0x<hex_addr>.
func RecoveryResolver(path string, logger log.Logger) FunctionResolver {
	return func(ctx context.Context) ([]FunctionEntry, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		ef, err := elf.Open(path)
		if err != nil {
			return nil, errors.Wrap(err, "failed to open binary")
		}
		defer ef.Close()

		candidates, err := resurgo.DetectFunctionsFromELF(ef)
		if err != nil {
			return nil, errors.Wrap(err, "resurgo: failed to detect functions")
		}

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var entries []FunctionEntry
		for _, c := range candidates {
			if c.Confidence != resurgo.ConfidenceHigh {
				continue
			}
			offset, err := vaToFileOffset(ef, c.Address)
			if err != nil {
				logger.Debug().Uint64("addr", c.Address).Err(err).Msg("skipping candidate")
				continue
			}
			entries = append(entries, FunctionEntry{
				Name:   fmt.Sprintf("func_0x%x", c.Address),
				Offset: offset,
			})
		}
		if len(entries) == 0 {
			return nil, ErrNoFunctionSymbols
		}
		return entries, nil
	}
}
