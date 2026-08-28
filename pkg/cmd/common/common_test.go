package common

import (
	"errors"
	"os"
	"path/filepath"
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
