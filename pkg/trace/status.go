package trace

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/maxgio92/xcover/internal/output"
	"github.com/maxgio92/xcover/internal/utils"
)

func (t *UserTracer) printStatusBar(ctx context.Context, eventsCh chan []byte) {
	if !t.status {
		return
	}
	output.StatusBar(ctx,
		1*time.Second, // bar refresh interval.
		func() {
			output.PrintRight(output.PrettyTraceStatus(
				float64(utils.LenSyncMap(&t.ack))/float64(len(t.tracee.funcs))*100,
				atomic.SwapUint64(&t.consumed, 0), // events rate reset at each bar refresh.
				bufferUtilisation(len(eventsCh), cap(eventsCh)),
			))
		},
	)
}

// bufferUtilisation returns how full a buffer of the given capacity is, as a
// whole percentage from 0 to 100. It multiplies before dividing so a partial
// fill does not truncate to 0. A zero capacity, such as an unbuffered channel,
// reports 0.
func bufferUtilisation(n, capacity int) int {
	if capacity == 0 {
		return 0
	}
	return n * 100 / capacity
}
