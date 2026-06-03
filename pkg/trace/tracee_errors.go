package trace

import (
	"github.com/pkg/errors"
)

var (
	ErrNoFunctionSymbols    = errors.New("no functions found")
	ErrNoSymbolTable        = errors.New("no symbol table available")
	ErrNoOffsets            = errors.New("no function offsets found")
	ErrExePathEmpty         = errors.New("exe path is empty")
	ErrElfFileNil           = errors.New("elf file is nil")
	ErrNoBuildID            = errors.New("executable or debug file has no GNU build-id")
	ErrDebugBuildIDMismatch = errors.New("debug file build-id does not match executable")
)
