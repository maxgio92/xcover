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
	// Name is the raw symbol name. It is the attach key and the name written
	// to the report.
	Name string
	// Demangled is the human-readable form of Name for C++ and Rust symbols
	// and equals Name for everything else. Resolvers may leave it empty to
	// mean "same as Name".
	Demangled string
	Offset    uint64
}

// FunctionResolver resolves the set of functions to trace from a binary.
// It is self-contained: it owns its own I/O and closes any resources it opens.
// ctx is checked between steps; cancellation aborts the resolution early.
type FunctionResolver func(ctx context.Context) ([]FunctionEntry, error)

// funcSym is an ELF function symbol paired with its demangled name. The name
// is demangled once here and shared by the filters and the FunctionEntry
// built from it.
type funcSym struct {
	elf.Symbol
	demangled string
}

func newFuncSym(sym elf.Symbol) funcSym {
	return funcSym{Symbol: sym, demangled: demangleName(sym.Name)}
}

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
func funcSymsFromELF(f *elf.File, filter symFilter) ([]funcSym, error) {
	syms, err := f.Symbols()
	if err != nil {
		return nil, err
	}

	var out []funcSym
	for _, sym := range syms {
		if elf.ST_TYPE(sym.Info) != elf.STT_FUNC {
			continue
		}
		fs := newFuncSym(sym)
		if !filter.shouldInclude(fs) {
			continue
		}
		out = append(out, fs)
	}
	return out, nil
}

// funcEntriesFromSymbols converts a list of ELF function symbols to FunctionEntry values.
// toOffset converts sym.Value (a virtual address) to the file offset required for uprobe attachment.
func funcEntriesFromSymbols(syms []funcSym, toOffset func(uint64) (uint64, error), logger log.Logger) ([]FunctionEntry, error) {
	var entries []FunctionEntry
	for _, sym := range syms {
		offset, err := toOffset(sym.Value)
		if err != nil {
			logger.Debug().Str("symbol", sym.Name).Err(err).Msg("failed to resolve file offset, skipping")
			continue
		}
		entries = append(entries, FunctionEntry{Name: sym.Name, Demangled: sym.demangled, Offset: offset})
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

	var syms []funcSym
	for _, fn := range table.Funcs {
		// Go symbol names are plain text and never mangled, so they bypass
		// the demangler: a name starting with _R or _Z would otherwise be
		// misread as a Rust or C++ symbol.
		sym := funcSym{
			Symbol: elf.Symbol{
				Name:  fn.Name,
				Value: fn.Entry,
				Size:  fn.End - fn.Entry,
				Info:  byte(elf.STT_FUNC),
			},
			demangled: fn.Name,
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

// symFilter holds the symbol filters of one resolver run. The name patterns
// are compiled once here instead of once per symbol.
type symFilter struct {
	include, exclude         *regexp.Regexp
	bindInclude, bindExclude []elf.SymBind
}

// newSymFilter compiles the include and exclude patterns; an empty pattern
// disables that filter. An invalid pattern is reported as an error so the
// resolver fails before any binary is opened.
func newSymFilter(include, exclude string, bindInclude, bindExclude []elf.SymBind) (symFilter, error) {
	f := symFilter{bindInclude: bindInclude, bindExclude: bindExclude}
	var err error
	if include != "" {
		if f.include, err = regexp.Compile(include); err != nil {
			return symFilter{}, errors.Wrap(err, "invalid include pattern")
		}
	}
	if exclude != "" {
		if f.exclude, err = regexp.Compile(exclude); err != nil {
			return symFilter{}, errors.Wrap(err, "invalid exclude pattern")
		}
	}
	return f, nil
}

// shouldInclude reports whether sym passes the filters. Binding filters run
// first. A name pattern matches when it matches the raw or the demangled
// name, so "^app::net::" selects a C++ namespace while "^_ZN3app" keeps
// working. Exclude wins over include.
func (f symFilter) shouldInclude(sym funcSym) bool {
	for _, b := range f.bindExclude {
		if elf.ST_BIND(sym.Info) == b {
			return false
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
	if f.exclude != nil && matchesName(f.exclude, sym) {
		return false
	}
	if f.include != nil {
		return matchesName(f.include, sym)
	}
	return true
}

// matchesName reports whether re matches the raw or the demangled name of sym.
func matchesName(re *regexp.Regexp, sym funcSym) bool {
	return re.MatchString(sym.Name) || (sym.demangled != sym.Name && re.MatchString(sym.demangled))
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
			name := fmt.Sprintf("func_0x%x", c.Address)
			entries = append(entries, FunctionEntry{
				Name:      name,
				Demangled: name,
				Offset:    offset,
			})
		}
		if len(entries) == 0 {
			return nil, ErrNoFunctionSymbols
		}
		return entries, nil
	}
}
