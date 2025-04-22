package trace

import (
	"github.com/pkg/errors"
)

var (
	ErrNoFunctionSymbols = errors.New("no functions found")
	ErrNoOffsets         = errors.New("no function offsets found")
	ErrExePathEmpty      = errors.New("exe path is empty")
	ErrElfFileNil        = errors.New("elf file is nil")
)
