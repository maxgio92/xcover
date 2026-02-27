package trace

import (
	"debug/elf"
	"debug/gosym"
	"regexp"

	"github.com/aquasecurity/libbpfgo/helpers"
	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
)

// FunctionEntry represents a function resolved from a binary, ready for uprobe attachment.
type FunctionEntry struct {
	Name   string
	Offset uint64
}

// FunctionResolver resolves the set of functions to trace from a binary path.
// It returns a list of FunctionEntry values ready for uprobe attachment.
type FunctionResolver func(path string) ([]FunctionEntry, error)

// SymbolTableResolver returns a FunctionResolver backed by the ELF symbol table,
// with a .gopclntab fallback for stripped Go binaries.
func SymbolTableResolver(logger log.Logger, include, exclude string, bindInclude, bindExclude []elf.SymBind) FunctionResolver {
	return func(path string) ([]FunctionEntry, error) {
		f, err := elf.Open(path)
		if err != nil {
			return nil, errors.Wrap(err, "failed to open ELF file")
		}
		defer f.Close()

		syms, err := funcSymsFromELF(f, include, exclude, bindInclude, bindExclude)
		if err != nil {
			if errors.Is(err, elf.ErrNoSymbols) {
				logger.Info().Msg("binary is stripped, attempting .gopclntab fallback")
				return funcEntriesFromGoPclntab(f, path, include, exclude, bindInclude, bindExclude, logger)
			}
			return nil, err
		}
		if len(syms) == 0 {
			logger.Info().Msg("no function symbols found, attempting .gopclntab fallback")
			entries, goPclnErr := funcEntriesFromGoPclntab(f, path, include, exclude, bindInclude, bindExclude, logger)
			if goPclnErr != nil {
				return nil, ErrNoFunctionSymbols
			}
			return entries, nil
		}

		return funcEntriesFromSymbols(path, syms, logger)
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
func funcEntriesFromSymbols(path string, syms []elf.Symbol, logger log.Logger) ([]FunctionEntry, error) {
	var entries []FunctionEntry
	for _, sym := range syms {
		offset := int64(sym.Value)
		if helperOffset, err := helpers.SymbolToOffset(path, sym.Name); err == nil {
			offset = helperOffset
			logger.Debug().Str("symbol", sym.Name).Int64("offset", offset).Msg("resolved offset via helper")
		} else {
			logger.Debug().Str("symbol", sym.Name).Int64("offset", offset).Msg("helper failed, using sym.Value as offset")
		}
		entries = append(entries, FunctionEntry{Name: sym.Name, Offset: uint64(offset)})
	}
	if len(entries) == 0 {
		return nil, ErrNoOffsets
	}
	return entries, nil
}

// funcEntriesFromGoPclntab extracts function entries from the .gopclntab section.
// This works even for stripped Go binaries where the ELF symbol table is absent.
func funcEntriesFromGoPclntab(f *elf.File, path, include, exclude string, bindInclude, bindExclude []elf.SymBind, logger log.Logger) ([]FunctionEntry, error) {
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

	var elfSyms []elf.Symbol
	for _, fn := range table.Funcs {
		fileOffset := (fn.Entry - textAddr) + textOffset
		elfSyms = append(elfSyms, elf.Symbol{
			Name:  fn.Name,
			Value: fileOffset,
			Size:  fn.End - fn.Entry,
			Info:  byte(elf.STT_FUNC),
		})
	}

	var filtered []elf.Symbol
	for _, sym := range elfSyms {
		if shouldInclude(sym, include, exclude, bindInclude, bindExclude) {
			filtered = append(filtered, sym)
		}
	}
	if len(filtered) == 0 {
		return nil, ErrNoFunctionSymbols
	}

	return funcEntriesFromSymbols(path, filtered, logger)
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
