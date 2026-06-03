package trace

import (
	"github.com/pkg/errors"
)

var (
	ErrFuncNotFoundForCookie = errors.New("function not found for cookie")
	ErrBpfObjBufEmpty        = errors.New("BPF object buffer is empty")
	ErrBpfObjNameEmpty       = errors.New("BPF object name is empty")
	ErrTraceeNil             = errors.New("trace is nil")
	ErrTraceeExePathEmpty    = errors.New("tracee exe path is empty")
	ErrTraceeFuncListEmpty   = errors.New("tracee function list is empty")
)
