package trace

import (
	"debug/elf"
	"debug/gosym"

	"github.com/aquasecurity/libbpfgo/helpers"
	"github.com/maxgio92/xcover/internal/utils"
	"github.com/pkg/errors"
)

// loadFunctionsFromGoPclntab attempts to load function symbols from the
// .gopclntab section of a Go binary. This works even when the binary
// is stripped and doesn't have a .symtab section.
//
// The .gopclntab section contains the program counter line table which
// includes function names and addresses, and is retained even in stripped
// Go binaries.
func (t *UserTracee) loadFunctionsFromGoPclntab() error {
	if t.file == nil {
		return ErrElfFileNil
	}

	// Find the .gopclntab section
	pclntabSection := t.file.Section(".gopclntab")
	if pclntabSection == nil {
		return errors.New("no .gopclntab section found - not a Go binary or too old")
	}

	// Read the pclntab data
	pclntabData, err := pclntabSection.Data()
	if err != nil {
		return errors.Wrap(err, "failed to read .gopclntab section")
	}

	t.logger.Debug().
		Uint64("offset", pclntabSection.Offset).
		Int("size", len(pclntabData)).
		Msg("found .gopclntab section")

	// Find the .text section for the base address
	textSection := t.file.Section(".text")
	if textSection == nil {
		return errors.New("no .text section found")
	}

	textAddr := textSection.Addr
	t.logger.Debug().
		Uint64("addr", textAddr).
		Msg("found .text section")

	// Parse the pclntab to create a line table
	lineTable := gosym.NewLineTable(pclntabData, textAddr)
	if lineTable == nil {
		return errors.New("failed to parse line table from .gopclntab")
	}

	// Create a symbol table from the line table
	// For stripped binaries, we pass nil for the first parameter (symtab)
	// and rely solely on the pclntab data
	table, err := gosym.NewTable(nil, lineTable)
	if err != nil {
		return errors.Wrap(err, "failed to create symbol table from pclntab")
	}

	if table == nil {
		return errors.New("symbol table is nil after parsing pclntab")
	}

	// Extract all functions from the table
	funcs := table.Funcs
	if len(funcs) == 0 {
		return errors.New("no functions found in pclntab")
	}

	t.logger.Debug().
		Int("count", len(funcs)).
		Msg("extracted functions from .gopclntab")

	// Convert gosym.Func to elf.Symbol format for compatibility with existing code
	// CRITICAL: fn.Entry is a virtual address, but uprobes need file offsets.
	// We must convert: fileOffset = (virtualAddr - textVirtualAddr) + textFileOffset
	textOffset := textSection.Offset
	var elfSyms []elf.Symbol
	for _, fn := range funcs {
		// Convert virtual address to file offset
		fileOffset := (fn.Entry - textAddr) + textOffset

		sym := elf.Symbol{
			Name:  fn.Name,
			Value: fileOffset, // Use file offset, not virtual address
			Size:  fn.End - fn.Entry,
			Info:  byte(elf.STT_FUNC), // Mark as function type
		}
		elfSyms = append(elfSyms, sym)
	}

	// Filter symbols based on include/exclude patterns
	var filteredSyms []elf.Symbol
	for _, sym := range elfSyms {
		if t.ShouldIncludeSymbol(sym) {
			filteredSyms = append(filteredSyms, sym)
		}
	}

	if len(filteredSyms) == 0 {
		return ErrNoFunctionSymbols
	}

	t.logger.Debug().
		Int("total", len(elfSyms)).
		Int("filtered", len(filteredSyms)).
		Str("include", t.symPatternInclude).
		Str("exclude", t.symPatternExclude).
		Msg("filtered functions from gopclntab")

	// Convert symbols to funcInfo and store in t.funcs
	return t.loadFunctionsFromSymbols(filteredSyms)
}

// loadFunctionsFromSymbols loads function information from a list of symbols
// This is a helper function used by both loadFunctions and loadFunctionsFromGoPclntab
func (t *UserTracee) loadFunctionsFromSymbols(funcSyms []elf.Symbol) error {
	t.logger.Debug().
		Int("functions", len(funcSyms)).
		Str("exe_path", t.exePath).
		Msg("loading function offsets from symbols")

	for _, sym := range funcSyms {
		// Try to get the offset using the helper first (works for unstripped binaries)
		offset := int64(sym.Value)
		if helperOffset, err := t.getSymbolOffset(sym.Name); err == nil {
			offset = helperOffset
		}

		t.funcs[cookie(utils.Hash(sym.Name))] = funcInfo{
			name:   sym.Name,
			offset: uint64(offset),
		}
	}

	if len(t.funcs) == 0 {
		return ErrNoOffsets
	}

	return nil
}

// getSymbolOffset attempts to get the symbol offset, returning error if it fails
// This is a wrapper to avoid failing the entire load if one symbol can't be resolved
func (t *UserTracee) getSymbolOffset(symName string) (int64, error) {
	// Note: This import is already available via helpers package
	// from github.com/aquasecurity/libbpfgo/helpers
	offset, err := helpers.SymbolToOffset(t.exePath, symName)
	if err != nil {
		t.logger.Debug().
			Err(err).
			Str("symbol", symName).
			Str("exe_path", t.exePath).
			Msg("failed to get function offset via helper")
		return 0, err
	}
	return int64(offset), nil
}
