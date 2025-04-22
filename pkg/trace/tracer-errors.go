package trace

import (
	"github.com/pkg/errors"
)

var (
	ErrFuncNotFoundForCookie = errors.New("function not found for cookie")
	ErrBpfModPathEmpty       = errors.New("no BPF module path specified")
	ErrTraceeNil             = errors.New("trace is nil")
	ErrTraceeExePathEmpty    = errors.New("tracee exe path is empty")
	ErrTraceeFuncListEmpty   = errors.New("tracee function list is empty")
)
