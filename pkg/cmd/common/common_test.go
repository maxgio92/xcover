package common

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxgio92/xcover/internal/settings"
)

func withTempPidFile(t *testing.T) string {
	t.Helper()

	orig := settings.PidFile
	settings.PidFile = filepath.Join(t.TempDir(), "test.pid")
	t.Cleanup(func() { settings.PidFile = orig })

	return settings.PidFile
}

func TestWriteReadRemovePID(t *testing.T) {
	withTempPidFile(t)

	if err := WritePID(1234); err != nil {
		t.Fatalf("WritePID() error = %v", err)
	}

	pid, err := ReadPID()
	if err != nil {
		t.Fatalf("ReadPID() error = %v", err)
	}
	if pid != 1234 {
		t.Fatalf("ReadPID() = %d, want 1234", pid)
	}

	if err := RemovePID(); err != nil {
		t.Fatalf("RemovePID() error = %v", err)
	}

	if _, err := os.Stat(settings.PidFile); !os.IsNotExist(err) {
		t.Fatalf("expected PID file to be removed, stat err = %v", err)
	}
}

func TestReadPID_Missing(t *testing.T) {
	withTempPidFile(t)

	_, err := ReadPID()
	if err == nil {
		t.Fatal("expected error reading missing PID file, got nil")
	}
	if errors.Is(err, ErrInvalidPID) {
		t.Fatalf("expected a non-ErrInvalidPID error for a missing file, got %v", err)
	}
}

func TestReadPID_Malformed(t *testing.T) {
	path := withTempPidFile(t)

	if err := os.WriteFile(path, []byte("not-a-pid"), 0644); err != nil {
		t.Fatalf("failed to write malformed PID file: %v", err)
	}

	_, err := ReadPID()
	if !errors.Is(err, ErrInvalidPID) {
		t.Fatalf("ReadPID() error = %v, want wrapping ErrInvalidPID", err)
	}
}

func withTempLogFile(t *testing.T) string {
	t.Helper()

	orig := settings.LogFile
	settings.LogFile = filepath.Join(t.TempDir(), "xcover.log")
	t.Cleanup(func() { settings.LogFile = orig })

	return settings.LogFile
}

func TestDumpLogTail_MissingIsNoop(t *testing.T) {
	withTempLogFile(t)

	var buf bytes.Buffer
	DumpLogTail(&buf, 20)
	if buf.Len() != 0 {
		t.Fatalf("DumpLogTail on missing log = %q, want empty", buf.String())
	}
}

func TestDumpLogTail_LastNLines(t *testing.T) {
	path := withTempLogFile(t)

	var body strings.Builder
	for i := 1; i <= 25; i++ {
		fmt.Fprintf(&body, "line-%d\n", i)
	}
	if err := os.WriteFile(path, []byte(body.String()), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	var buf bytes.Buffer
	DumpLogTail(&buf, 3)
	got := buf.String()
	wantPrefix := "last 3 lines of " + path + ":\n"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("DumpLogTail prefix = %q, want starting %q", got, wantPrefix)
	}
	if !strings.Contains(got, "line-23\nline-24\nline-25\n") {
		t.Fatalf("DumpLogTail body = %q, want last three lines", got)
	}
	if strings.Contains(got, "line-22") {
		t.Fatalf("DumpLogTail included lines before the tail: %q", got)
	}
}
