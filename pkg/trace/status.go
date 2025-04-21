package trace

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/maxgio92/utrace/internal/output"
	"github.com/maxgio92/utrace/internal/utils"
)

func (t *UserTracer) printStatusBar(ctx context.Context, eventsCh, feedCh chan []byte) {
	if !t.status {
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			count := atomic.SwapUint64(&consumed, 0)
			output.PrintRight(
				prettyStatus(
					float64(utils.LenSyncMap(&t.ack))/float64(len(t.tracee.funcs))*100,
					count,
					len(eventsCh)/eventsChBufSize*100,
					len(feedCh)/feedChBufSize*100),
			)
		case <-ctx.Done():
			return
		}
	}
}

func prettyStatus(cov float64, rate uint64, evtUtil, feedUtil int) string {
	return fmt.Sprintf("\r%-30s %-20s %-30s %-30s",
		fmt.Sprintf("Functions aknowledged: [%s] %6.2f%%", output.ProgressBar(int(cov), 20), cov),
		fmt.Sprintf("Events/s: %4d", rate),
		fmt.Sprintf("Events Buffer: [%s] %3d%%", output.ProgressBar(evtUtil, 20), evtUtil),
		fmt.Sprintf("Feed Buffer: [%s] %3d%%", output.ProgressBar(feedUtil, 20), feedUtil),
	)
}
