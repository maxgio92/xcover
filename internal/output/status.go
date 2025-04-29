package output

import (
	"context"
	"fmt"
	"time"
)

func StatusBar(ctx context.Context, refreshRate time.Duration, printF func()) {
	ticker := time.NewTicker(refreshRate)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			printF()
		case <-ctx.Done():
			return
		}
	}
}

func PrettyTraceStatus(cov float64, rate uint64, evtUtil, feedUtil int) string {
	return fmt.Sprintf("\r%-50s %-20s %-20s",
		fmt.Sprintf("Coverage by functions: [%s] %6.2f%%", ProgressBar(int(cov), 40), cov),
		fmt.Sprintf("Events/s: %4d", rate),
		fmt.Sprintf("Events Buffer: [%s] %3d%%", ProgressBar(evtUtil, 10), evtUtil),
	)
}
