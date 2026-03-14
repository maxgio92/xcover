package trace

import (
	"context"
	"debug/elf"
	"debug/gosym"
	"fmt"
	"regexp"

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

		f, err := elf.Open(path)
		if err != nil {
			return nil, errors.Wrap(err, "failed to open binary")
		}
		defer f.Close()

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		syms, err := funcSymsFromELF(f, include, exclude, bindInclude, bindExclude)
		if err != nil {
			if errors.Is(err, elf.ErrNoSymbols) {
				logger.Info().Msg("binary is stripped, attempting .gopclntab fallback")
				return funcEntriesFromGoPclntab(f, include, exclude, bindInclude, bindExclude, logger)
			}
			return nil, err
		}
		if len(syms) == 0 {
			logger.Info().Msg("no function symbols found, attempting .gopclntab fallback")
			entries, goPclnErr := funcEntriesFromGoPclntab(f, include, exclude, bindInclude, bindExclude, logger)
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
func funcSymsFromELF(f *elf.File, include, exclude string, bindInclude, bindExclude []elf.SymBind) ([]elf.Symbol, error) {
	syms, err := f.Symbols()
	if err != nil {
		return nil, err
	}

	var out []elf.Symbol
	for _, sym := range syms {
		if elf.ST_TYPE(sym.Info) != elf.STT_FUNC {
			continue
		}
		if !shouldInclude(sym, include, exclude, bindInclude, bindExclude) {
			continue
		}
		out = append(out, sym)
	}
	return out, nil
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
func funcEntriesFromGoPclntab(f *elf.File, include, exclude string, bindInclude, bindExclude []elf.SymBind, logger log.Logger) ([]FunctionEntry, error) {
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
		if shouldInclude(sym, include, exclude, bindInclude, bindExclude) {
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

// shouldInclude reports whether sym passes the include/exclude filters.
func shouldInclude(sym elf.Symbol, include, exclude string, bindInclude, bindExclude []elf.SymBind) bool {
	if bindExclude != nil {
		for _, b := range bindExclude {
			if elf.ST_BIND(sym.Info) == b {
				return false
			}
		}
	}
	if bindInclude != nil {
		for _, b := range bindInclude {
			if elf.ST_BIND(sym.Info) == b {
				return true
			}
		}
		return false
	}
	if exclude != "" && regexp.MustCompile(exclude).MatchString(sym.Name) {
		return false
	}
	if include != "" {
		return regexp.MustCompile(include).MatchString(sym.Name)
	}
	return true
}
