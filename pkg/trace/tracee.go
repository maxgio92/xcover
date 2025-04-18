package trace

import (
	"debug/elf"
	"fmt"

	"github.com/aquasecurity/libbpfgo/helpers"
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
	t.file, err = elf.Open(t.exePath)
	if err != nil {
		return err
	}
	if t.file == nil {
		return errors.New("no elf file found")
	}
	if err = t.loadFunctions(); err != nil {
		return errors.Wrap(err, "failed loading functions data")
	}

	return nil
}

func (t *UserTracee) validate() error {
	if t.exePath == "" {
		return fmt.Errorf("exe path is empty")
	}

	return nil
}

func (t *UserTracee) loadFunctions() error {
	funcSyms, err := t.getFuncSyms()
	if err != nil {
		return err
	}

	for _, sym := range funcSyms {
		offset, err := helpers.SymbolToOffset(t.exePath, sym.Name)
		if err != nil {
			return errors.Wrapf(err, "symbol %s not found in %s", sym.Name, t.exePath)
		}
		t.funcs[cookie(hash(sym.Name))] = funcInfo{
			name:   sym.Name,
			offset: uint64(offset),
		}
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

func (t *UserTracee) getFuncOffsets() []uint64 {
	offsets := make([]uint64, len(t.funcs))
	for i := range t.funcs {
		offsets = append(offsets, t.funcs[i].offset)
	}

	return offsets
}

func (t *UserTracee) getFuncCookies() []uint64 {
	cookies := make([]uint64, len(t.funcs))
	for cookie := range t.funcs {
		cookies = append(cookies, uint64(cookie))
	}

	return cookies
}
