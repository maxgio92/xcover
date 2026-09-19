package stop

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/options"
)

func withTempPidFile(t *testing.T) string {
	t.Helper()

	orig := settings.PidFile
	settings.PidFile = filepath.Join(t.TempDir(), "test.pid")
	t.Cleanup(func() { settings.PidFile = orig })

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
