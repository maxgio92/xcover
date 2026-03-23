// Package procwatch watches /proc for a process whose executable matches a
// given path and emits the PID when found. It is used by xcover to detect
// the tracee automatically without requiring the user to provide a PID or
// wrap their command.
package procwatch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultInterval = 250 * time.Millisecond

// Watcher polls /proc for a process running the target executable.
type Watcher struct {
	targetPath string
	interval   time.Duration
}

// New returns a Watcher that looks for processes running targetPath.
func New(targetPath string) (*Watcher, error) {
	abs, err := filepath.Abs(targetPath)
	if err != nil {
		return nil, fmt.Errorf("procwatch: resolve target path: %w", err)
	}
	return &Watcher{targetPath: abs, interval: defaultInterval}, nil
}

// Watch blocks until a process running the target executable is found or ctx
// is cancelled. On success it returns the PID.
//
// Note: there is an inherent race between process start and injection. For
// typical test coverage workflows (binary starts, runs a suite) this window
// is acceptable. Short-lived processes that complete before the first poll
// interval may be missed.
func (w *Watcher) Watch(ctx context.Context) (int, error) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-ticker.C:
			pid, err := w.findPID()
			if err != nil {
				continue
			}
			return pid, nil
		}
	}
}

// findPID scans /proc entries and resolves /proc/<pid>/exe against the target.
func (w *Watcher) findPID() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("procwatch: read /proc: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a PID directory
		}

		exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
		if err != nil {
			continue // process may have exited or we lack permission
		}

		// Strip any " (deleted)" suffix that Linux appends for replaced
		// binaries.
		exe = strings.TrimSuffix(exe, " (deleted)")

		if exe == w.targetPath {
			return pid, nil
		}
	}

	return 0, fmt.Errorf("procwatch: no process found for %s", w.targetPath)
}
