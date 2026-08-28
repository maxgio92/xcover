package common

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"

	"github.com/maxgio92/xcover/internal/settings"
)

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
