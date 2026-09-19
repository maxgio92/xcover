package common

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
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

func TestWritePID_Atomic(t *testing.T) {
	path := withTempPidFile(t)

	if err := WritePID(111); err != nil {
		t.Fatalf("WritePID(111) error = %v", err)
	}
	if err := WritePID(222); err != nil {
		t.Fatalf("WritePID(222) error = %v", err)
	}

	pid, err := ReadPID()
	if err != nil {
		t.Fatalf("ReadPID() error = %v", err)
	}
	if pid != 222 {
		t.Fatalf("ReadPID() = %d, want 222", pid)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Fatalf("PID file mode = %o, want 0644", got)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("leftover files in PID dir: %v", names)
	}

	// A reader racing the writer must see the old or the new PID, never an
	// empty or partial file.
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("ReadFile() error = %v", err)
				return
			}
			if s := string(data); s != "111" && s != "222" {
				t.Errorf("read %q, want 111 or 222", s)
				return
			}
		}
	}()

	for i := 0; i < 200; i++ {
		if err := WritePID(111 + 111*(i%2)); err != nil {
			t.Errorf("WritePID() error = %v", err)
			break
		}
	}
	close(done)
	wg.Wait()
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

// TestWritePID_ReplacesInode pins the rename: a descriptor opened before a
// WritePID keeps reading the old content, which a truncating write in place
// could never satisfy.
func TestWritePID_ReplacesInode(t *testing.T) {
	path := withTempPidFile(t)
	if err := WritePID(111); err != nil {
		t.Fatalf("WritePID(111) error = %v", err)
	}
	held, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer held.Close()

	if err := WritePID(222); err != nil {
		t.Fatalf("WritePID(222) error = %v", err)
	}
	old, err := io.ReadAll(held)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(old) != "111" {
		t.Fatalf("held descriptor reads %q, want 111", old)
	}
	pid, err := ReadPID()
	if err != nil || pid != 222 {
		t.Fatalf("ReadPID() = %d, %v; want 222", pid, err)
	}
}
