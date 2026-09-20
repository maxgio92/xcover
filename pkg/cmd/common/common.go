package common

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/maxgio92/xcover/internal/settings"
)

var (
	// ErrInvalidPID is returned by ReadPID when the PID file exists but its
	// content cannot be parsed as a PID. Callers should use errors.Is to detect
	// this case rather than inspecting the underlying parse error.
	ErrInvalidPID = errors.New("invalid PID")

	// ErrNotRunning is the one message every command prints when no daemon
	// runs. CheckRunning wraps it with the PID file detail.
	ErrNotRunning = fmt.Errorf("%s is not running", settings.CmdName)

	// ErrInvalidPIDFile is returned by CheckRunning when the PID file exists
	// but does not hold a positive PID.
	ErrInvalidPIDFile = errors.New("invalid PID file")
)

const (
	// logTailLines is how many log lines PrintLogTail shows.
	logTailLines = 20
	// logTailMaxBytes bounds the read from the end of the log. The status
	// bar redraws in place with carriage returns and no newline, so a
	// line count alone would not bound the bytes.
	logTailMaxBytes = 64 << 10
)

// ansiEscape matches one CSI sequence: ESC, "[", parameter bytes,
// intermediate bytes, one final byte. zerolog emits only SGR ("ESC [ n m"),
// which is a subset.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

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

// CheckRunning reports whether the daemon named by the PID file is alive.
// It returns the validated PID and nil for a live process, an error wrapping
// ErrNotRunning when the file is missing or names a dead process, an error
// wrapping ErrInvalidPIDFile when the content is not a positive PID, and a
// plain error for any other read failure. Callers that need the PID use the
// returned value instead of reading the file again, because the daemon can
// exit between the two reads.
func CheckRunning() (int, error) {
	pid, err := ReadPID()
	switch {
	case errors.Is(err, os.ErrNotExist):
		return 0, fmt.Errorf("%w: PID file %s not found", ErrNotRunning, settings.PidFile)
	case errors.Is(err, ErrInvalidPID):
		return 0, fmt.Errorf("%w %s", ErrInvalidPIDFile, settings.PidFile)
	case err != nil:
		return 0, fmt.Errorf("read PID file %s: %v", settings.PidFile, unwrapPathError(err))
	case pid <= 0:
		return 0, fmt.Errorf("%w %s", ErrInvalidPIDFile, settings.PidFile)
	case !processAlive(pid):
		return 0, fmt.Errorf("%w: stale PID file %s (PID %d)", ErrNotRunning, settings.PidFile, pid)
	}

	return pid, nil
}

// IsDaemonRunning reports whether CheckRunning finds a live daemon.
func IsDaemonRunning() bool {
	_, err := CheckRunning()

	return err == nil
}

// unwrapPathError returns the errno behind a *fs.PathError, or err itself.
// The os package names the path in the PathError, so a caller that prints
// the path in its own message uses this to show it once.
func unwrapPathError(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}

	return err
}

// LogTail returns the last lines of the file at path, reading at most
// maxBytes from its end plus one byte before them. Segments end at a
// newline or a carriage return, so a status bar redrawn in place shows as
// separate lines. When the file is larger than maxBytes the first segment
// is dropped: the extra leading byte makes it either the partial tail of
// the line before the window or the empty segment a separator there
// produces, never a complete line inside the window. ANSI escape sequences
// are removed and empty segments skipped before the line cap applies. A
// missing file gives nil lines and a nil error. Any other failure is
// returned without the path, which the caller already knows.
func LogTail(path string, lines int, maxBytes int64) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, unwrapPathError(err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, unwrapPathError(err)
	}
	limit := maxBytes
	truncated := info.Size() > maxBytes
	if truncated {
		limit = maxBytes + 1
		if _, err := f.Seek(info.Size()-limit, io.SeekStart); err != nil {
			return nil, unwrapPathError(err)
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, unwrapPathError(err)
	}

	segments := strings.Split(strings.ReplaceAll(string(data), "\r", "\n"), "\n")
	if truncated {
		segments = segments[1:]
	}
	out := make([]string, 0, len(segments))
	for _, seg := range segments {
		seg = ansiEscape.ReplaceAllString(seg, "")
		if seg != "" {
			out = append(out, seg)
		}
	}
	if len(out) > lines {
		out = out[len(out)-lines:]
	}

	return out, nil
}

// PrintLogTail writes the tail of the daemon log to w under a header that
// names the file and the line count. A missing or empty log, or one with
// nothing left after stripping, writes nothing. A read failure is reported
// on w in one line so the caller can still return its own error.
func PrintLogTail(w io.Writer) {
	lines, err := LogTail(settings.LogFile, logTailLines, logTailMaxBytes)
	if err != nil {
		fmt.Fprintf(w, "cannot read %s: %v\n", settings.LogFile, err)
		return
	}
	if len(lines) == 0 {
		return
	}

	fmt.Fprintf(w, "tail of %s (%d lines):\n", settings.LogFile, len(lines))
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
}

// processAlive reports whether pid names a live process. Signal 0 performs
// the existence check without delivering anything: nil means the process
// exists and this user may signal it, EPERM means it exists under another
// user (a daemon started with sudo), ESRCH means it is gone. pid <= 0 is
// rejected first because kill(0, 0) and kill(-1, 0) address a process group
// or every process and succeed.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	err := syscall.Kill(pid, 0)

	return err == nil || errors.Is(err, syscall.EPERM)
}
