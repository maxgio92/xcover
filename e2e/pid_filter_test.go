//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/maxgio92/xcover/pkg/coverage"
)

const pidFilterGoScenario = "pid-filter-go-module"

// TestPIDFilterRestrictsToProcess runs two instances of the same binary, each
// calling a distinct marker function, and checks that --pid records hits from
// the selected instance only while a run without --pid records both.
func TestPIDFilterRestrictsToProcess(t *testing.T) {
	xcover := xcoverBinary(t)
	buildDir := t.TempDir()
	bin := buildGoFixture(t, buildDir, pidFilterGoScenario)
	t.Logf("built Go fixture binary: %s", bin)

	filtered, filteredPID := traceFixtureInstances(t, xcover, bin, true)
	if filtered.PID != filteredPID {
		t.Fatalf("report pid = %d, want %d", filtered.PID, filteredPID)
	}
	acked := stringSet(filtered.FuncsAck)
	for _, name := range []string{"main.tick", "main.onlyA"} {
		if !acked[name] {
			t.Fatalf("--pid run did not ack %q; acked=%v", name, filtered.FuncsAck)
		}
	}
	if acked["main.onlyB"] {
		t.Fatalf("--pid run acked main.onlyB from the unselected instance; acked=%v", filtered.FuncsAck)
	}

	control, _ := traceFixtureInstances(t, xcover, bin, false)
	if control.PID != 0 {
		t.Fatalf("report without --pid has pid = %d, want it omitted", control.PID)
	}
	acked = stringSet(control.FuncsAck)
	for _, name := range []string{"main.tick", "main.onlyA", "main.onlyB"} {
		if !acked[name] {
			t.Fatalf("control run did not ack %q; acked=%v", name, control.FuncsAck)
		}
	}
}

// traceFixtureInstances starts instance A and B of bin, traces bin with
// --pid set to instance A when filter is true, lets both instances run past
// readiness, stops them and returns the report together with A's PID.
func traceFixtureInstances(t *testing.T, xcover, bin string, filter bool) (report coverage.CoverageReport, pidA int) {
	t.Helper()

	workDir := t.TempDir()
	stopFile := filepath.Join(workDir, "stop")
	instanceA := startFixtureInstance(t, workDir, bin, "a", stopFile)
	instanceB := startFixtureInstance(t, workDir, bin, "b", stopFile)
	pidA = instanceA.Process.Pid
	t.Logf("started fixture instances: a=%d b=%d", pidA, instanceB.Process.Pid)

	runArgs := []string{"--path", bin, "--scope", projectScope}
	if filter {
		runArgs = append(runArgs, "--pid", strconv.Itoa(pidA))
	}

	report = runXcoverSession(t, xcover, workDir, runArgs, func() {
		// The instances loop every 20ms; give both a few rounds after the
		// probes are attached before releasing them.
		time.Sleep(time.Second)
		if err := os.WriteFile(stopFile, nil, 0o644); err != nil {
			t.Fatalf("failed to create stop file: %v", err)
		}
		for _, instance := range []*exec.Cmd{instanceA, instanceB} {
			if err := instance.Wait(); err != nil {
				t.Fatalf("fixture instance %d failed: %v", instance.Process.Pid, err)
			}
		}
	})
	return report, pidA
}

func startFixtureInstance(t *testing.T, dir, bin, marker, stopFile string) *exec.Cmd {
	t.Helper()

	cmd := exec.Command(bin, marker, stopFile)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start fixture instance %s: %v", marker, err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return cmd
}
