package trace

import (
	"github.com/maxgio92/utrace/pkg/symtable"
	log "github.com/rs/zerolog"
)

const (
	TaskCommLen = 16
)

type HistogramKey struct {
	// PID.
	Pid int32

	// UserStackId, an index into the stack-traces map.
	UserStackId uint32

	// KernelStackId, an index into the stack-traces map.
	KernelStackId uint32

	// Comm.
	Comm [TaskCommLen]byte
}

// StackTrace is an array of instruction pointers (IP).
// 127 is the size of the trace, as for the default PERF_MAX_STACK_DEPTH.
type StackTrace [127]uint64

type Profiler struct {
	pid                  int
	comm                 string
	samplingPeriodMillis uint64
	probe                []byte
	probeName            string
	mapStackTraces       string
	mapHistogram         string
	logger               log.Logger
	symTabELF            *symtable.ELFSymTab
}
