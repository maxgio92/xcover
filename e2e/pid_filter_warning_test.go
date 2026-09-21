//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	pidFilterByThreadWarning  = "filters uprobe_multi by thread instead of thread group"
	pidFilterInconclusiveText = "could not check whether the kernel filters uprobe_multi"
)

// TestPIDFilterWarnsOnThreadFilteringKernel runs a --pid session and asserts
// the daemon log carries the thread-filter warning when the running kernel
// lacks commit 46ba0e49b642. On a fixed kernel the warning cannot fire, so
// the test skips with a plain t.Skipf and names the kernel release. It does
// not use skipOrFail because a fixed kernel is a valid environment, not an
// unmet precondition, and XCOVER_E2E_REQUIRE=1 must not fail it. In both
// cases the inconclusive variant of the warning is a defect: the check must
// give an answer on every supported kernel.
func TestPIDFilterWarnsOnThreadFilteringKernel(t *testing.T) {
	xcover := xcoverBinary(t)
	bin := buildGoFixture(t, t.TempDir(), pidFilterGoScenario)
	t.Logf("built Go fixture binary: %s", bin)

	workDir := t.TempDir()
	stopFile := filepath.Join(workDir, "stop")
	instance := startFixtureInstance(t, workDir, bin, "a", stopFile)
	pid := instance.Process.Pid
	t.Logf("started fixture instance: %d", pid)

	runArgs := []string{"--path", bin, "--scope", projectScope, "--pid", strconv.Itoa(pid)}
	logOffset := startXcoverDaemonEnv(t, xcover, workDir, nil, runArgs)
	// The instance loops every 20ms; give it a few rounds past readiness.
	time.Sleep(time.Second)
	if err := os.WriteFile(stopFile, nil, 0o644); err != nil {
		t.Fatalf("failed to create stop file: %v", err)
	}
	if err := instance.Wait(); err != nil {
		t.Fatalf("fixture instance %d failed: %v", pid, err)
	}
	stopXcoverDaemon(t, xcover, workDir)

	tail := readSince(t, logFile, logOffset)
	if strings.Contains(tail, pidFilterInconclusiveText) {
		t.Fatalf("the PID filter check was inconclusive on kernel %s:\n%s", kernelRelease(t), tail)
	}
	if !strings.Contains(tail, pidFilterByThreadWarning) {
		t.Skipf("kernel %s filters uprobe_multi by thread group, so the --pid warning cannot fire", kernelRelease(t))
	}
}

func kernelRelease(t *testing.T) string {
	t.Helper()
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		t.Fatalf("uname failed: %v", err)
	}
	return unix.ByteSliceToString(uts.Release[:])
}
