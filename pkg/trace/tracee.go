package trace

import (
	"debug/elf"
	"fmt"
	"github.com/pkg/errors"
)

type UserTracee struct {
	file        *elf.File
	funcOffsets []uint64
	funcSyms    []elf.Symbol
	*UserTraceeOptions
}

func NewUserTracee(opts ...UserTraceeOption) *UserTracee {
	tracee := &UserTracee{
		UserTraceeOptions: &UserTraceeOptions{},
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
	t.file, err = elf.Open(t.exePath)
	if err != nil {
		return err
	}
	if t.file == nil {
		return errors.New("no elf file found")
	}
	t.funcSyms, err = t.getFuncSyms()
	if err != nil {
		return err
	}
	t.funcOffsets, err = t.getFuncOffsets()
	if err != nil {
		return err
	}

	return nil
}

func (t *UserTracee) validate() error {
	if t.exePath == "" {
		return fmt.Errorf("exe path is empty")
	}

	return nil
}

func (t *UserTracee) getFuncSyms() ([]elf.Symbol, error) {
	var funSyms []elf.Symbol
	if t.file == nil {
		return nil, fmt.Errorf("elf file is nil")
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
		// Exclude non-local symbols.
		if elf.ST_BIND(sym.Info) != elf.STB_LOCAL {
			continue
		}

		funSyms = append(funSyms, sym)
	}

	return funSyms, nil
}

func (t *UserTracee) getFuncOffsets() ([]uint64, error) {
	offsets := make([]uint64, len(t.funcOffsets))
	for _, sym := range t.funcSyms {
		// TODO(maxgio92): use libbpfgo/helpers.
		offset, err := symbolToOffset(t.exePath, sym.Name)
		if err != nil {
			return nil, errors.Wrapf(err, "symbol %s not found in %s", sym.Name, t.exePath)
		}
		offsets = append(offsets, offset)
	}

	return offsets, nil
}

// SymbolToOffset attempts to resolve a 'symbol' name in the binary found at
// 'path' to an offset. The offset can be used for attaching a u(ret)probe
func symbolToOffset(path, symbol string) (uint64, error) {
	f, err := elf.Open(path)
	if err != nil {
		return 0, fmt.Errorf("could not open elf file to resolve symbol offset: %w", err)
	}

	regularSymbols, regularSymbolsErr := f.Symbols()
	dynamicSymbols, dynamicSymbolsErr := f.DynamicSymbols()

	// Only if we failed getting both regular and dynamic symbols - then we abort.
	if regularSymbolsErr != nil && dynamicSymbolsErr != nil {
		return 0, fmt.Errorf("could not open regular or dynamic symbol sections to resolve symbol offset: %w %s", regularSymbolsErr, dynamicSymbolsErr)
	}

	// Concatenating into a single list.
	// The list can have duplications, but we will find the first occurrence which is sufficient.
	syms := append(regularSymbols, dynamicSymbols...)

	sectionsToSearchForSymbol := []*elf.Section{}

	for i := range f.Sections {
		if f.Sections[i].Flags == elf.SHF_ALLOC+elf.SHF_EXECINSTR {
			sectionsToSearchForSymbol = append(sectionsToSearchForSymbol, f.Sections[i])
		}
	}

	var executableSection *elf.Section

	for j := range syms {
		if syms[j].Name == symbol {
			// Find what section the symbol is in by checking the executable section's
			// addr space.
			for m := range sectionsToSearchForSymbol {
				if syms[j].Value > sectionsToSearchForSymbol[m].Addr &&
					syms[j].Value < sectionsToSearchForSymbol[m].Addr+sectionsToSearchForSymbol[m].Size {
					executableSection = sectionsToSearchForSymbol[m]
				}
			}

			if executableSection == nil {
				return 0, errors.New("could not find symbol in executable sections of binary")
			}

			return syms[j].Value - executableSection.Addr + executableSection.Offset, nil
		}
	}

	return 0, fmt.Errorf("symbol %s not found in %s", symbol, path)
}
