package common

import (
	"os"
	"strconv"
	"syscall"

	"github.com/maxgio92/xcover/internal/settings"
)

func IsDaemonRunning() bool {
	pidData, err := os.ReadFile(settings.PidFile)
	if err != nil {
		return false
	}

	pid, err := strconv.Atoi(string(pidData))
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
