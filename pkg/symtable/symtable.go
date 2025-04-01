package symtable

import (
	"debug/elf"
	"github.com/maxgio92/utrace/pkg/symcache"
	"github.com/pkg/errors"
)

var (
	ErrSymNotFound   = errors.New("symbol not found")
	ErrSymTableEmpty = errors.New("symtable is empty")
)

// ELFSymTab is one of the possible abstractions around executable
// file symbol tables, for ELF files.
type ELFSymTab struct {
	Symtab []elf.Symbol
	cache  *symcache.SymCache
}

func NewELFSymTab() *ELFSymTab {
	tab := new(ELFSymTab)
	tab.Symtab = make([]elf.Symbol, 0)
	tab.cache = symcache.NewSymCache()

	return tab
}

// Load loads from the underlying filesystem the ELF file
// with debug/elf.Open and stores it in the ELFSymTab struct.
func (e *ELFSymTab) Load(pathname string) error {
	// Skip load if file elf.File has already been loaded.
	if e.Symtab != nil && len(e.Symtab) > 0 {
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

	e.Symtab = syms

	return nil
}

// GetName returns symbol name from an instruction pointer address.
func (e *ELFSymTab) GetName(ip uint64, cache bool) (string, error) {
	if !cache {
		for _, s := range e.Symtab {
			if ip >= s.Value && ip < (s.Value+s.Size) {
				return s.Name, nil
			}
		}
		return "", ErrSymNotFound
	}
	// Try from cache.
	sym, err := e.cache.Get(ip)
	if err != nil {
		// Cache miss.
		if e.Symtab == nil || len(e.Symtab) == 0 {
			return "", ErrSymTableEmpty
		}
		for _, s := range e.Symtab {
			if ip >= s.Value && ip < (s.Value+s.Size) {
				sym = s.Name
			}
		}
		if sym == "" {
			return "", ErrSymNotFound
		}
		if e.cache != nil {
			e.cache.Set(sym, ip)
		}
	}

	return sym, nil
}
