//go:build e2e

package e2e

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestStatusReportsRunningThenStopped checks that status names the daemon PID
// while a run is active and reports it as not running after stop.
func TestStatusReportsRunningThenStopped(t *testing.T) {
	xcover := xcoverBinary(t)
	workDir := t.TempDir()
	bin := buildGoFixture(t, workDir, projectScopeGoScenario)

	startXcoverDaemon(t, xcover, workDir, []string{"--path", bin, "--scope=" + projectScope})

	out := xcoverStatus(t, xcover, workDir)
	if !strings.Contains(out, "is running (PID") {
		t.Fatalf("status while running = %q, want it to contain %q", out, "is running (PID")
	}

	stopXcoverDaemon(t, xcover, workDir)

	out = xcoverStatus(t, xcover, workDir)
	if !strings.Contains(out, "is not running") {
		t.Fatalf("status after stop = %q, want it to contain %q", out, "is not running")
	}
}

// stalePID is a PID no process can hold: Linux caps pid_max at 2^22, well
// below MaxInt32, so kill(stalePID, 0) fails with ESRCH and IsDaemonRunning
// reports the daemon as not running.
const stalePID = math.MaxInt32

// TestStaleProcessFileIsOverwritten seeds the PID file with a PID that names
// no process and checks that run --detach starts anyway and replaces the file
// with the PID of the live daemon.
func TestStaleProcessFileIsOverwritten(t *testing.T) {
	xcover := xcoverBinaryPath(t)
	requireNoStateFile(t, socketFile)
	requireNoStateFile(t, pidFile)

	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(stalePID)), 0o644); err != nil {
		t.Fatalf("failed to seed stale PID file: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(pidFile) })
	t.Logf("seeded %s with stale PID %d", pidFile, stalePID)

	workDir := t.TempDir()
	bin := buildGoFixture(t, workDir, projectScopeGoScenario)
	startXcoverDaemon(t, xcover, workDir, []string{"--path", bin, "--scope=" + projectScope})

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("failed to read PID file after start: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("PID file holds %q, want an integer: %v", data, err)
	}
	if pid == stalePID {
		t.Fatalf("PID file still names the stale PID %d", stalePID)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("PID file names %d, which is not alive: %v", pid, err)
	}

	want := fmt.Sprintf("is running (PID %d)", pid)
	if out := xcoverStatus(t, xcover, workDir); !strings.Contains(out, want) {
		t.Fatalf("status = %q, want it to contain %q", out, want)
	}

	stopXcoverDaemon(t, xcover, workDir)
}

func xcoverStatus(t *testing.T, xcover, workDir string) string {
	t.Helper()
	out, err := commandOutput(workDir, 10*time.Second, xcover, "status")
	if err != nil {
		t.Fatalf("xcover status failed: %v\n%s", err, out)
	}
	t.Logf("xcover status: %s", strings.TrimSpace(out))
	return out
}
