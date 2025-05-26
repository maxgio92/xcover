package trace

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/maxgio92/xcover/internal/output"
	"github.com/maxgio92/xcover/internal/utils"
	"github.com/maxgio92/xcover/pkg/probe"
)

func (t *UserTracer) printStatusBar(ctx context.Context, eventsCh, feedCh chan []byte) {
	if !t.status {
		return
	}
	output.StatusBar(ctx,
		1*time.Second, // bar refresh interval.
		func() {
			output.PrintRight(output.PrettyTraceStatus(
				float64(utils.LenSyncMap(&t.ack))/float64(len(t.tracee.funcs))*100,
				atomic.SwapUint64(&t.consumed, 0), // events rate reset at each bar refresh.
				len(eventsCh)/probe.EventsChBufSize*100,
				len(feedCh)/feedChBufSize*100,
			))
		},
	)
}
