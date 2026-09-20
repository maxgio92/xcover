//go:build e2e

package e2e

import (
	"os/exec"
	"testing"
	"time"
)

// TestDebugPathTracesStrippedBinary strips the Go fixture, keeps its debug
// info in a separate file and checks that --debug-path resolves the fixture
// functions for the stripped --path binary. The stripped binary loses its
// .note.gnu.build-id section, so the resolver refuses the pair unless
// --no-build-id-check is passed; the session exercises that flag for real.
// The session passes no --scope because run ignores it once --debug-path
// installs its own resolver.
func TestDebugPathTracesStrippedBinary(t *testing.T) {
	if _, err := exec.LookPath("objcopy"); err != nil {
		skipOrFail(t, "objcopy is not installed: %v", err)
	}

	xcover := xcoverBinary(t)
	workDir := t.TempDir()
	bin := buildGoFixture(t, workDir, projectScopeGoScenario)
	debug := bin + ".debug"
	stripped := bin + ".stripped"
	runCommand(t, workDir, 30*time.Second, "objcopy", "--only-keep-debug", bin, debug)
	runCommand(t, workDir, 30*time.Second, "objcopy", "--strip-all", bin, stripped)
	runCommand(t, workDir, 30*time.Second, "objcopy", "--remove-section", ".note.gnu.build-id", stripped)
	t.Logf("built stripped fixture %s with debug file %s", stripped, debug)

	args := []string{"--path", stripped, "--debug-path", debug, "--no-build-id-check"}
	report := runXcoverSession(t, xcover, workDir, args, func() {
		t.Log("running stripped fixture binary")
		runCommand(t, workDir, 10*time.Second, stripped)
	})

	assertFixtureFunctionsAcked(t, report)
}
