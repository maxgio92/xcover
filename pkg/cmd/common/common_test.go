package common

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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

func withTempLogFile(t *testing.T) string {
	t.Helper()

	orig := settings.LogFile
	settings.LogFile = filepath.Join(t.TempDir(), "xcover.log")
	t.Cleanup(func() { settings.LogFile = orig })

	return settings.LogFile
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

// skipIfRoot skips a test that needs a permission error, which root never
// gets from the kernel.
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode 0000 file; EACCES cannot be produced")
	}
}

func TestLogTail(t *testing.T) {
	// numbered returns the lines "line from" to "line to", each newline
	// terminated, and the same lines as LogTail returns them.
	numbered := func(from, to int) (string, []string) {
		var sb strings.Builder
		var want []string
		for i := from; i <= to; i++ {
			line := fmt.Sprintf("line %d", i)
			sb.WriteString(line + "\n")
			want = append(want, line)
		}
		return sb.String(), want
	}

	// Twenty lines of 4000 bytes make 80000 bytes. The last 64 KiB start
	// at offset 14464, which is 2464 bytes into line 3, so the read begins
	// with a partial line followed by the 16 complete lines 4 to 19.
	const wideCount, wideWidth = 20, 4000
	var wide strings.Builder
	var wideWant []string
	for i := 0; i < wideCount; i++ {
		line := fmt.Sprintf("%0*d", wideWidth-1, i)
		wide.WriteString(line + "\n")
		if i >= 4 {
			wideWant = append(wideWant, line)
		}
	}
	if wide.Len() <= logTailMaxBytes || (wide.Len()-logTailMaxBytes)%wideWidth == 0 {
		t.Fatalf("wide fixture is %d bytes; the byte cap must fall mid-line", wide.Len())
	}

	// Seventeen lines of 4096 bytes make 69632 bytes. The last 64 KiB start
	// at offset 4096, right after the newline that ends line 0, so the byte
	// before the window is a separator and the window holds the 16 complete
	// lines 1 to 16. Line 1 must survive.
	const alignedWidth = 4096
	const alignedCount = logTailMaxBytes/alignedWidth + 1
	var aligned strings.Builder
	var alignedWant []string
	for i := 0; i < alignedCount; i++ {
		line := fmt.Sprintf("%0*d", alignedWidth-1, i)
		aligned.WriteString(line + "\n")
		if i >= 1 {
			alignedWant = append(alignedWant, line)
		}
	}
	if aligned.Len() <= logTailMaxBytes || (aligned.Len()-logTailMaxBytes)%alignedWidth != 0 {
		t.Fatalf("aligned fixture is %d bytes; the byte cap must fall on a line start", aligned.Len())
	}

	moreContent, moreWant := numbered(1, 25)

	tests := []struct {
		name string
		// missing leaves the log file absent; content is ignored.
		missing bool
		content string
		// setup runs after content is written.
		setup func(t *testing.T, path string)
		want  []string
		// wantErr is the sentinel the error must wrap and wantErrText its
		// exact text; nil means LogTail must succeed.
		wantErr     error
		wantErrText string
	}{
		{
			name:    "missing",
			missing: true,
		},
		{
			name: "empty",
		},
		{
			name:    "only escape sequences",
			content: "\x1b[31m\x1b[0m\n\x1b[2K\r",
		},
		{
			name:    "fewer lines than cap",
			content: "a\nb\nc\n",
			want:    []string{"a", "b", "c"},
		},
		{
			name:    "more lines than cap",
			content: moreContent,
			want:    moreWant[len(moreWant)-logTailLines:],
		},
		{
			name:    "byte cap drops the partial first segment",
			content: wide.String(),
			want:    wideWant,
		},
		{
			name:    "byte cap window after a newline keeps its first line",
			content: aligned.String(),
			want:    alignedWant,
		},
		{
			name:    "no trailing newline",
			content: "a\nb",
			want:    []string{"a", "b"},
		},
		{
			name:    "ansi stripped",
			content: "\x1b[31mboom\x1b[0m\n",
			want:    []string{"boom"},
		},
		{
			name:    "carriage return split",
			content: "\rA\rB\n",
			want:    []string{"A", "B"},
		},
		{
			name:    "permission denied",
			content: "hidden\n",
			setup: func(t *testing.T, path string) {
				t.Helper()
				skipIfRoot(t)
				if err := os.Chmod(path, 0000); err != nil {
					t.Fatalf("Chmod() error = %v", err)
				}
			},
			wantErr:     fs.ErrPermission,
			wantErrText: "permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := withTempLogFile(t)
			if !tt.missing {
				if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			}
			if tt.setup != nil {
				tt.setup(t, path)
			}

			got, err := LogTail(path, logTailLines, logTailMaxBytes)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("LogTail() error = %v, want wrapping %v", err, tt.wantErr)
				}
				if err.Error() != tt.wantErrText {
					t.Errorf("LogTail() error = %q, want %q", err, tt.wantErrText)
				}
				if got != nil {
					t.Errorf("LogTail() = %v, want nil with an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("LogTail() error = %v", err)
			}
			if len(got) > logTailLines {
				t.Fatalf("LogTail() returned %d lines, cap is %d", len(got), logTailLines)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("LogTail() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintLogTail(t *testing.T) {
	t.Run("lines", func(t *testing.T) {
		path := withTempLogFile(t)
		if err := os.WriteFile(path, []byte("first\n\x1b[31mboom\x1b[0m\n"), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		var buf bytes.Buffer
		PrintLogTail(&buf)

		if got, want := buf.String(), "tail of "+path+" (2 lines):\nfirst\nboom\n"; got != want {
			t.Errorf("PrintLogTail() wrote %q, want %q", got, want)
		}
	})

	t.Run("missing", func(t *testing.T) {
		withTempLogFile(t)

		var buf bytes.Buffer
		PrintLogTail(&buf)

		if buf.Len() != 0 {
			t.Errorf("PrintLogTail() wrote %q, want nothing", buf.String())
		}
	})

	t.Run("unreadable", func(t *testing.T) {
		skipIfRoot(t)
		path := withTempLogFile(t)
		if err := os.WriteFile(path, []byte("hidden\n"), 0000); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		var buf bytes.Buffer
		PrintLogTail(&buf)

		if got, want := buf.String(), "cannot read "+path+": permission denied\n"; got != want {
			t.Errorf("PrintLogTail() wrote %q, want %q", got, want)
		}
	})
}
