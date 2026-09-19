package common

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/maxgio92/xcover/internal/settings"
)

// ErrInvalidPID is returned by ReadPID when the PID file exists but its
// content cannot be parsed as a PID. Callers should use errors.Is to detect
// this case rather than inspecting the underlying parse error.
var ErrInvalidPID = errors.New("invalid PID")

// WritePID writes the given PID to the PID file. The write is atomic: the
// PID goes to a temp file in the same directory which is then renamed over
// the PID file. A concurrent reader sees the old content or the new PID,
// never an empty or partial file.
func WritePID(pid int) error {
	return writeFileAtomic(settings.PidFile, []byte(strconv.Itoa(pid)), 0644)
}

// writeFileAtomic writes data to a temp file next to path, sets perm, and
// renames it onto path. On any error the process lives to see it tries to
// remove the temp file; a kill between create and rename leaves it behind.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()

	_, err = f.Write(data)
	if err == nil {
		// CreateTemp uses 0600 and rename keeps the inode, so set perm here.
		err = f.Chmod(perm)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}

	return nil
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
