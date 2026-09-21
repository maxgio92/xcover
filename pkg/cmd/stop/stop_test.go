package stop

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/common"
	"github.com/maxgio92/xcover/pkg/cmd/options"
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

// seedLog writes two lines to a temp daemon log, the second wrapped in an
// SGR sequence, and returns exactly what PrintLogTail must write for it.
func seedLog(t *testing.T) string {
	t.Helper()

	path := withTempLogFile(t)
	if err := os.WriteFile(path, []byte("failed to load bpf module trace\n\x1b[31mboom\x1b[0m\n"), 0644); err != nil {
		t.Fatalf("failed to seed log file: %v", err)
	}

	return "tail of " + path + " (2 lines):\nfailed to load bpf module trace\nboom\n"
}

func TestRun_MissingPIDFile(t *testing.T) {
	path := withTempPidFile(t)
	wantTail := seedLog(t)

	var buf bytes.Buffer
	o := &Options{Options: options.NewOptions(), timeout: defaultTimeout, errOut: &buf}

	err := o.Run(nil, nil)
	if !errors.Is(err, common.ErrNotRunning) {
		t.Fatalf("Run() error = %v, want wrapping common.ErrNotRunning", err)
	}
	if got, want := err.Error(), "xcover is not running: PID file "+path+" not found"; got != want {
		t.Fatalf("Run() error = %q, want %q", got, want)
	}
	if got := buf.String(); got != wantTail {
		t.Fatalf("Run() errOut = %q, want %q", got, wantTail)
	}
}

func TestRun_MalformedPIDFile(t *testing.T) {
	path := withTempPidFile(t)

	if err := os.WriteFile(path, []byte("not-a-pid"), 0644); err != nil {
		t.Fatalf("failed to write malformed PID file: %v", err)
	}
	wantTail := seedLog(t)

	var buf bytes.Buffer
	o := &Options{Options: options.NewOptions(), timeout: defaultTimeout, errOut: &buf}

	err := o.Run(nil, nil)
	if !errors.Is(err, ErrInvalidPIDFile) {
		t.Fatalf("Run() error = %v, want ErrInvalidPIDFile", err)
	}
	if got, want := err.Error(), "invalid PID file "+path; got != want {
		t.Fatalf("Run() error = %q, want %q", got, want)
	}
	if got := buf.String(); got != wantTail {
		t.Fatalf("Run() errOut = %q, want %q", got, wantTail)
	}
}

func TestRun_StalePIDFile(t *testing.T) {
	path := withTempPidFile(t)

	// 1<<22 is the largest pid_max Linux accepts, so one past it never
	// names a process.
	if err := os.WriteFile(path, []byte(strconv.Itoa(1<<22+1)), 0644); err != nil {
		t.Fatalf("failed to write stale PID file: %v", err)
	}
	wantTail := seedLog(t)

	var buf bytes.Buffer
	o := &Options{Options: options.NewOptions(), timeout: defaultTimeout, errOut: &buf}

	err := o.Run(nil, nil)
	if !errors.Is(err, common.ErrNotRunning) {
		t.Fatalf("Run() error = %v, want wrapping common.ErrNotRunning", err)
	}
	if got, want := err.Error(), "xcover is not running: stale PID file "+path+" (PID 4194305)"; got != want {
		t.Fatalf("Run() error = %q, want %q", got, want)
	}
	if got := buf.String(); got != wantTail {
		t.Fatalf("Run() errOut = %q, want %q", got, wantTail)
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

// TestRun_ContextCanceled points the PID file at a child that ignores SIGTERM
// and asserts a cancelled o.Ctx ends the grace loop at once, leaving the
// child, its PID file and the log tail alone.
func TestRun_ContextCanceled(t *testing.T) {
	path := withTempPidFile(t)
	seedLog(t)

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

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	o := &Options{Options: options.NewOptions(options.WithContext(ctx)), timeout: 5 * time.Second, errOut: &buf}

	start := time.Now()
	err = o.Run(nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed >= o.timeout {
		t.Fatalf("Run() returned after %v, reached the %v grace period", elapsed, o.timeout)
	}

	// Cancellation must not force kill the child or remove its PID file.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("PID file missing after cancellation: %v", err)
	}
	if err := syscall.Kill(child.Process.Pid, 0); err != nil {
		t.Fatalf("child not alive after cancellation: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("Run() errOut = %q, want no log tail", buf.String())
	}
}
