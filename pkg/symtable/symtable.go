package symtable

import (
	"debug/elf"
	"github.com/maxgio92/utrace/pkg/symcache"
	"github.com/pkg/errors"
)

var (
	ErrSymTableEmpty = errors.New("symtable is empty")
)

// ELFSymTab is one of the possible abstractions around executable
// file symbol tables, for ELF files.
type ELFSymTab struct {
	symtab []elf.Symbol
	cache  *symcache.SymCache
}

func NewELFSymTab() *ELFSymTab {
	tab := new(ELFSymTab)
	tab.symtab = make([]elf.Symbol, 0)
	tab.cache = symcache.NewSymCache()

	return tab
}

// Load loads from the underlying filesystem the ELF file
// with debug/elf.Open and stores it in the ELFSymTab struct.
func (e *ELFSymTab) Load(pathname string) error {
	// Skip load if file elf.File has already been loaded.
	if e.symtab != nil && len(e.symtab) > 0 {
		return nil
	}

	file, err := elf.Open(pathname)
	if err != nil {
		return errors.Wrap(err, "error opening ELF file")
	}

	syms, err := file.Symbols()
	if err != nil {
		return errors.Wrap(err, "error reading ELF symtable section")
	}

	e.symtab = syms

	return nil
}

// GetName returns symbol name from an instruction pointer address.
func (e *ELFSymTab) GetName(ip uint64) (string, error) {
	// Try from cache.
	sym, err := e.cache.Get(ip)
	if err != nil {
		// Cache miss.
		if e.symtab == nil || len(e.symtab) == 0 {
			return "", ErrSymTableEmpty
		}
		for _, s := range e.symtab {
			if ip >= s.Value && ip < (s.Value+s.Size) {
				sym = s.Name
			}
		}
		if e.cache != nil {
			e.cache.Set(sym, ip)
		}
	}

	return sym, nil
}
