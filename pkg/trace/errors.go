package trace

import (
	"github.com/pkg/errors"
)

// Resolver errors: returned by the symbol/offset resolvers in resolver.go,
// resolver_go.go and resolver_debug.go, and checked by their callers
// (UserTracee.Init in tracee.go).
var (
	ErrNoFunctionSymbols    = errors.New("no functions found")
	ErrNoSymbolTable        = errors.New("no symbol table available")
	ErrNoOffsets            = errors.New("no function offsets found")
	ErrElfFileNil           = errors.New("elf file is nil")
	ErrNoBuildID            = errors.New("executable or debug file has no GNU build-id")
	ErrDebugBuildIDMismatch = errors.New("debug file build-id does not match executable")

	// ErrProjectScopeUnsupported is returned by GoProjectResolver when the
	// binary does not carry the metadata required for project-scoped function
	// resolution (e.g. missing or incomplete Go build info). Callers may treat
	// this as a signal to fall back to binary-scope resolution.
	ErrProjectScopeUnsupported = errors.New("project scope not supported for this binary")
)

// Tracee errors: returned by UserTracee (tracee.go).
var (
	ErrExePathEmpty = errors.New("exe path is empty")
)

// Tracer errors: used by UserTracer (tracer.go).
var (
	ErrFuncNotFoundForCookie = errors.New("function not found for cookie")
	ErrBpfObjBufEmpty        = errors.New("BPF object buffer is empty")
	ErrBpfObjNameEmpty       = errors.New("BPF object name is empty")
	ErrTraceeNil             = errors.New("trace is nil")
	ErrTraceeExePathEmpty    = errors.New("tracee exe path is empty")
	ErrTraceeFuncListEmpty   = errors.New("tracee function list is empty")
)
