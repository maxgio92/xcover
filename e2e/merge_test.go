//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxgio92/xcover/pkg/coverage"
)

// TestMergeCombinesTwoSessionReports runs two sessions of the same fixture
// with disjoint --include filters and checks that xcover merge -o keeps their
// build_id and traces the union of their functions, which is larger than
// either input.
func TestMergeCombinesTwoSessionReports(t *testing.T) {
	xcover := xcoverBinary(t)
	bin := buildGoFixture(t, t.TempDir(), projectScopeGoScenario)
	first, firstReport := runMergeInputSession(t, xcover, bin, `--include=^main\.`)
	second, secondReport := runMergeInputSession(t, xcover, bin, `--include=^example\.com/testmod/pkg\.`)

	if firstReport.BuildID == "" || firstReport.BuildID != secondReport.BuildID {
		t.Fatalf("expected both inputs to share a non-empty build_id; got %q and %q", firstReport.BuildID, secondReport.BuildID)
	}

	outDir := t.TempDir()
	out := filepath.Join(outDir, "merged.json")
	runCommand(t, outDir, 10*time.Second, xcover, "merge", first, second, "-o", out)

	merged := readReport(t, out)
	if merged.BuildID != firstReport.BuildID {
		t.Fatalf("expected merged build_id %q; got %q", firstReport.BuildID, merged.BuildID)
	}

	traced := stringSet(merged.FuncsTraced)
	if len(traced) <= len(firstReport.FuncsTraced) || len(traced) <= len(secondReport.FuncsTraced) {
		t.Fatalf("expected merged funcs_traced (%d) to exceed both inputs (%d and %d); merged=%v",
			len(traced), len(firstReport.FuncsTraced), len(secondReport.FuncsTraced), merged.FuncsTraced)
	}
	expected := stringSet(append(append([]string{}, firstReport.FuncsTraced...), secondReport.FuncsTraced...))
	for name := range expected {
		if !traced[name] {
			t.Fatalf("expected merged report to trace %q; traced=%v", name, merged.FuncsTraced)
		}
	}
	if len(merged.FuncsTraced) != len(expected) {
		t.Fatalf("expected merged funcs_traced to hold exactly the %d names from both inputs; got %d: %v",
			len(expected), len(merged.FuncsTraced), merged.FuncsTraced)
	}
}

// TestMergeAllowsMissingBuildID checks that a report with an empty build_id is
// refused by default and accepted with --allow-missing-build-id, in which case
// the merged build_id is empty.
func TestMergeAllowsMissingBuildID(t *testing.T) {
	xcover := xcoverBinary(t)
	bin := buildGoFixture(t, t.TempDir(), projectScopeGoScenario)
	path, report := runMergeInputSession(t, xcover, bin, "--include=^main\\.")

	report.BuildID = ""
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("failed to marshal report without build_id: %v", err)
	}
	dir := t.TempDir()
	blank := filepath.Join(dir, "no-build-id.json")
	if err := os.WriteFile(blank, data, 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", blank, err)
	}

	out := filepath.Join(dir, "merged.json")
	output, err := commandOutput(dir, 10*time.Second, xcover, "merge", path, blank, "-o", out)
	if err == nil {
		t.Fatalf("expected merge without --allow-missing-build-id to fail\n%s", output)
	}
	if !strings.Contains(output, coverage.ErrBuildIDMissing.Error()) {
		t.Fatalf("expected merge failure to mention %q; got %v\n%s", coverage.ErrBuildIDMissing, err, output)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatalf("expected no output file after a refused merge; found %s", out)
	}

	runCommand(t, dir, 10*time.Second, xcover, "merge", path, blank, "--allow-missing-build-id", "-o", out)
	merged := readReport(t, out)
	if merged.BuildID != "" {
		t.Fatalf("expected empty merged build_id; got %q", merged.BuildID)
	}
}

// runMergeInputSession traces one run of the fixture binary bin in a fresh
// work dir with the given filter flag and returns the report path and its
// parsed content.
func runMergeInputSession(t *testing.T, xcover, bin, filter string) (string, coverage.CoverageReport) {
	t.Helper()

	workDir := t.TempDir()
	report := runXcoverSession(t, xcover, workDir, []string{"--path", bin, "--scope=" + projectScope, filter}, func() {
		runCommand(t, workDir, 10*time.Second, bin)
	})
	return filepath.Join(workDir, reportFile), report
}
