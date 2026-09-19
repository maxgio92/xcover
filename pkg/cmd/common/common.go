package common

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/maxgio92/xcover/internal/settings"
)

// DefaultLogTailLines is how many trailing lines DumpLogTail prints.
const DefaultLogTailLines = 20

// ErrInvalidPID is returned by ReadPID when the PID file exists but its
// content cannot be parsed as a PID. Callers should use errors.Is to detect
// this case rather than inspecting the underlying parse error.
var ErrInvalidPID = errors.New("invalid PID")

// WritePID writes the given PID to the PID file.
func WritePID(pid int) error {
	return os.WriteFile(settings.PidFile, []byte(strconv.Itoa(pid)), 0644)
}

// ReadPID reads and parses the PID from the PID file.
func ReadPID() (int, error) {
	pidData, err := os.ReadFile(settings.PidFile)
	if err != nil {
		return 0, err
	}

	pid, err := strconv.Atoi(string(pidData))
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidPID, err)
	}

	return pid, nil
}

// RemovePID removes the PID file.
func RemovePID() error {
	return os.Remove(settings.PidFile)
}

func IsDaemonRunning() bool {
	pid, err := ReadPID()
	if err != nil {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Check if process exists
	return process.Signal(syscall.Signal(0)) == nil
}

// DumpLogTail writes the last n lines of the daemon log to w. A missing or
// empty log is a no-op so wait/stop can call this on every "not running"
// path without inventing a second error.
func DumpLogTail(w io.Writer, n int) {
	if w == nil {
		return
	}
	if n <= 0 {
		n = DefaultLogTailLines
	}
	data, err := os.ReadFile(settings.LogFile)
	if err != nil || len(data) == 0 {
		return
	}
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	fmt.Fprintf(w, "last %d lines of %s:\n", len(lines), settings.LogFile)
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
}
