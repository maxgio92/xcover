package static

import "debug/elf"

func GetFuncs(name string) ([]elf.Symbol, error) {
	var symbols []elf.Symbol

	b, err := elf.Open(name)
	if err != nil {
		return nil, err
	}
	syms, err := b.Symbols()
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

		symbols = append(symbols, sym)
	}

	return symbols, nil
}
