//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestVerboseLogsFunctionNames checks that --verbose prints the name of each
// function on its first hit. The detached daemon writes stdout to the log
// file, and the tracer prints one demangled name per line, so the log must
// gain a line equal to a fixture function name.
func TestVerboseLogsFunctionNames(t *testing.T) {
	xcover := xcoverBinary(t)
	workDir := t.TempDir()
	bin := buildGoFixture(t, workDir, projectScopeGoScenario)

	logOffset := fileSize(t, logFile)
	args := []string{"--path", bin, "--scope=" + projectScope, "--verbose"}
	report := runXcoverSession(t, xcover, workDir, args, func() {
		t.Log("running fixture binary")
		runCommand(t, workDir, 10*time.Second, bin)
	})
	assertFixtureFunctionsAcked(t, report)

	lines := stringSet(strings.Split(readSince(t, logFile, logOffset), "\n"))
	for _, name := range fixtureExecutedFuncs {
		if lines[name] {
			return
		}
	}
	t.Fatalf("--verbose did not print any of %v to %s", fixtureExecutedFuncs, logFile)
}
