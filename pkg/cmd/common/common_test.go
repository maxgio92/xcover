package common

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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

func TestCheckRunning(t *testing.T) {
	// 1<<22 is the largest pid_max Linux accepts, so one past it never
	// names a process.
	const stalePID = 1<<22 + 1

	writeFile := func(content string) func(t *testing.T, path string) {
		return func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
		}
	}

	tests := []struct {
		name string
		// setup prepares the PID file at path; nil leaves it missing.
		setup func(t *testing.T, path string)
		// wantIs is the sentinel the error must wrap; nil with an empty
		// want means CheckRunning must return nil.
		wantIs error
		// wantNotIs lists sentinels the error must not wrap.
		wantNotIs []error
		// want is the exact error string with the PID file path substituted
		// for %s; empty means no error.
		want string
		// wantPID is the PID a nil error must come with.
		wantPID int
	}{
		{
			name:   "missing",
			wantIs: ErrNotRunning,
			want:   "xcover is not running: PID file %s not found",
		},
		{
			name:   "malformed",
			setup:  writeFile("abc"),
			wantIs: ErrInvalidPIDFile,
			want:   "invalid PID file %s",
		},
		{
			name:   "zero",
			setup:  writeFile("0"),
			wantIs: ErrInvalidPIDFile,
			want:   "invalid PID file %s",
		},
		{
			name:   "negative",
			setup:  writeFile("-1"),
			wantIs: ErrInvalidPIDFile,
			want:   "invalid PID file %s",
		},
		{
			name: "unreadable",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatalf("Mkdir() error = %v", err)
				}
			},
			wantNotIs: []error{ErrNotRunning, ErrInvalidPIDFile},
			want:      "read PID file %s: is a directory",
		},
		{
			name:   "stale",
			setup:  writeFile(strconv.Itoa(stalePID)),
			wantIs: ErrNotRunning,
			want:   "xcover is not running: stale PID file %s (PID 4194305)",
		},
		{
			name:    "live self",
			setup:   writeFile(strconv.Itoa(os.Getpid())),
			wantPID: os.Getpid(),
		},
		{
			// PID 1 belongs to another user unless the test runs as root,
			// so this pins EPERM counting as live.
			name: "live pid 1",
			setup: func(t *testing.T, path string) {
				if os.Geteuid() == 0 {
					t.Skip("root can signal pid 1; EPERM cannot be produced")
				}
				writeFile("1")(t, path)
			},
			wantPID: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := withTempPidFile(t)
			if tt.setup != nil {
				tt.setup(t, path)
			}

			pid, err := CheckRunning()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("CheckRunning() error = %v, want nil", err)
				}
				if pid != tt.wantPID {
					t.Fatalf("CheckRunning() = %d, want %d", pid, tt.wantPID)
				}
				return
			}
			if err == nil {
				t.Fatal("CheckRunning() error = nil, want error")
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("CheckRunning() error = %v, want wrapping %v", err, tt.wantIs)
			}
			for _, notIs := range tt.wantNotIs {
				if errors.Is(err, notIs) {
					t.Errorf("CheckRunning() error = %v, must not wrap %v", err, notIs)
				}
			}
			if got, want := err.Error(), fmt.Sprintf(tt.want, path); got != want {
				t.Errorf("CheckRunning() error = %q, want %q", got, want)
			}
		})
	}
}

func TestIsDaemonRunning_PID1(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can signal pid 1; EPERM cannot be produced")
	}

	path := withTempPidFile(t)

	if err := os.WriteFile(path, []byte("1"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if !IsDaemonRunning() {
		t.Fatal("IsDaemonRunning() = false for PID 1, want true")
	}

	if err := os.WriteFile(path, []byte(strconv.Itoa(1<<22+1)), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if IsDaemonRunning() {
		t.Fatal("IsDaemonRunning() = true for an impossible PID, want false")
	}
}
