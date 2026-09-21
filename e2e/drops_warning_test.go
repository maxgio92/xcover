//go:build e2e

package e2e

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	seenFuncsCapEnv  = "XCOVER_E2E_SEEN_FUNCS_MAX"
	dropsWarningText = "calls not recorded because the seen_funcs map rejected the insert"
)

var (
	// ansiEscape matches the colour codes the console log writer wraps
	// around field names, so `dropped=N` can be matched as written.
	ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")
	// droppedField matches the dropped counter the console writer prints as
	// `dropped=N`.
	droppedField = regexp.MustCompile(`\bdropped=(\d+)`)
)

// TestDropsWarningReportsRejectedInserts caps the seen_funcs map to one entry
// through the e2etest sizing seam, runs a fixture that hits several distinct
// functions and asserts the daemon logs the drops warning with a positive
// count. The scenario relies on the xcover binary being built with
// -tags e2etest, as CI does; a release build ignores the variable and the
// warning never fires.
func TestDropsWarningReportsRejectedInserts(t *testing.T) {
	xcover := xcoverBinary(t)
	workDir := t.TempDir()
	bin := buildGoFixture(t, workDir, projectScopeGoScenario)
	t.Logf("built Go fixture binary: %s", bin)

	runArgs := []string{"--path", bin, "--scope=" + projectScope}
	env := []string{seenFuncsCapEnv + "=1"}
	logOffset := startXcoverDaemonEnv(t, xcover, workDir, env, runArgs)

	// The fixture calls at least two distinct project functions, so the map
	// of one entry rejects every call past the first function.
	t.Log("running fixture binary")
	runCommand(t, workDir, 10*time.Second, bin)
	stopXcoverDaemon(t, xcover, workDir)

	tail := readSince(t, logFile, logOffset)
	plain := ansiEscape.ReplaceAllString(tail, "")
	if !strings.Contains(plain, dropsWarningText) {
		t.Fatalf("daemon log does not contain %q; the xcover binary must be built with -tags e2etest for %s to cap the map:\n%s", dropsWarningText, seenFuncsCapEnv, plain)
	}
	match := droppedField.FindStringSubmatch(plain)
	if match == nil {
		t.Fatalf("daemon log has the drops warning but no dropped=N field:\n%s", plain)
	}
	dropped, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil {
		t.Fatalf("failed to parse dropped count %q: %v", match[1], err)
	}
	if dropped == 0 {
		t.Fatalf("expected a positive dropped count, got %d:\n%s", dropped, plain)
	}
	t.Logf("daemon reported %d dropped calls", dropped)
}
