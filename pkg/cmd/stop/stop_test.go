package stop

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/options"
)

func withTempPidFile(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	origPid := settings.PidFile
	origLog := settings.LogFile
	settings.PidFile = filepath.Join(dir, "test.pid")
	settings.LogFile = filepath.Join(dir, "xcover.log")
	t.Cleanup(func() {
		settings.PidFile = origPid
		settings.LogFile = origLog
	})

	return settings.PidFile
}

func TestRun_MissingPIDFile(t *testing.T) {
	withTempPidFile(t)

	o := &Options{Options: options.NewOptions(), timeout: defaultTimeout}

	err := o.Run(nil, nil)
	if !errors.Is(err, ErrNotRunningOrNotFound) {
		t.Fatalf("Run() error = %v, want ErrNotRunningOrNotFound", err)
	}
}

func TestRun_MissingPIDFilePrintsLogTail(t *testing.T) {
	withTempPidFile(t)
	if err := os.WriteFile(settings.LogFile, []byte("failed to load BPF object\n"), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	runErr := (&Options{Options: options.NewOptions(), timeout: defaultTimeout}).Run(nil, nil)
	_ = w.Close()
	os.Stderr = orig
	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("read stderr: %v", readErr)
	}
	if !errors.Is(runErr, ErrNotRunningOrNotFound) {
		t.Fatalf("Run() error = %v, want ErrNotRunningOrNotFound", runErr)
	}
	got := string(out)
	if !strings.Contains(got, "failed to load BPF object") {
		t.Fatalf("stderr = %q, want daemon log tail", got)
	}
}

func TestRun_MalformedPIDFile(t *testing.T) {
	path := withTempPidFile(t)

	if err := os.WriteFile(path, []byte("not-a-pid"), 0644); err != nil {
		t.Fatalf("failed to write malformed PID file: %v", err)
	}

	o := &Options{Options: options.NewOptions(), timeout: defaultTimeout}

	err := o.Run(nil, nil)
	if !errors.Is(err, ErrInvalidPIDFile) {
		t.Fatalf("Run() error = %v, want ErrInvalidPIDFile", err)
	}
}

func TestNewCommand_TimeoutFlag(t *testing.T) {
	cmd := NewCommand(options.NewOptions())

	flag := cmd.Flags().Lookup("timeout")
	if flag == nil {
		t.Fatal("timeout flag not registered")
	}
	if flag.DefValue != defaultTimeout.String() {
		t.Fatalf("timeout default = %q, want %q", flag.DefValue, defaultTimeout)
	}

	if err := cmd.Flags().Parse([]string{"--timeout=1500ms"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	got, err := cmd.Flags().GetDuration("timeout")
	if err != nil {
		t.Fatalf("get timeout: %v", err)
	}
	if got != 1500*time.Millisecond {
		t.Fatalf("timeout = %v, want 1.5s", got)
	}
}

// TestRun_ForceKill points the PID file at a child that ignores SIGTERM and
// asserts Run force kills it after the grace period and reports the failure.
func TestRun_ForceKill(t *testing.T) {
	path := withTempPidFile(t)

	child := exec.Command("sh", "-c", `trap "" TERM; echo ready; while :; do sleep 1; done`)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	// Block until sh has installed the trap, so SIGTERM cannot arrive first.
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatalf("wait for child readiness: %v", err)
	}

	if err := os.WriteFile(path, []byte(strconv.Itoa(child.Process.Pid)), 0644); err != nil {
		t.Fatalf("write PID file: %v", err)
	}

	o := &Options{Options: options.NewOptions(), timeout: 300 * time.Millisecond}

	start := time.Now()
	err = o.Run(nil, nil)
	if !errors.Is(err, ErrForceKilled) {
		t.Fatalf("Run() error = %v, want ErrForceKilled", err)
	}
	if elapsed := time.Since(start); elapsed < o.timeout {
		t.Fatalf("Run() returned after %v, before the %v grace period", elapsed, o.timeout)
	}

	// The child must have been killed: Wait returns once it is reaped.
	if err := child.Wait(); err == nil {
		t.Fatal("child exited cleanly, want killed by signal")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PID file still present after force kill: %v", err)
	}
}
