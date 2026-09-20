//go:build e2e

package e2e

import (
	"regexp"
	"testing"
	"time"

	"github.com/maxgio92/xcover/pkg/coverage"
)

// filterPattern names one fixture function so the include and exclude
// scenarios can tell matching names from the rest of the project scope.
const filterPattern = `^main\.appLogic$`

// TestIncludeFilterTracesOnlyMatchingFunctions checks that --include keeps
// only the functions whose name matches the pattern.
func TestIncludeFilterTracesOnlyMatchingFunctions(t *testing.T) {
	report := runFilteredFixture(t, "--include="+filterPattern)
	pattern := regexp.MustCompile(filterPattern)

	if len(report.FuncsTraced) == 0 {
		t.Fatalf("--include=%s traced no function", filterPattern)
	}
	for _, name := range report.FuncsTraced {
		if !pattern.MatchString(name) {
			t.Fatalf("--include=%s traced non-matching function %q; traced=%v", filterPattern, name, report.FuncsTraced)
		}
	}
	if !stringSet(report.FuncsAck)["main.appLogic"] {
		t.Fatalf("--include=%s did not ack main.appLogic; ack=%v", filterPattern, report.FuncsAck)
	}
}

// TestExcludeFilterDropsMatchingFunctions checks that --exclude drops every
// function whose name matches the pattern and keeps the others.
func TestExcludeFilterDropsMatchingFunctions(t *testing.T) {
	report := runFilteredFixture(t, "--exclude="+filterPattern)
	pattern := regexp.MustCompile(filterPattern)

	for _, name := range report.FuncsTraced {
		if pattern.MatchString(name) {
			t.Fatalf("--exclude=%s traced matching function %q; traced=%v", filterPattern, name, report.FuncsTraced)
		}
	}
	traced := stringSet(report.FuncsTraced)
	for _, name := range fixtureExecutedFuncs {
		if name == "main.appLogic" {
			continue
		}
		if !traced[name] {
			t.Fatalf("--exclude=%s dropped unrelated function %q; traced=%v", filterPattern, name, report.FuncsTraced)
		}
	}
}

func runFilteredFixture(t *testing.T, filterArg string) coverage.CoverageReport {
	t.Helper()

	xcover := xcoverBinary(t)
	workDir := t.TempDir()
	bin := buildGoFixture(t, workDir, projectScopeGoScenario)
	t.Logf("built Go fixture binary: %s", bin)

	return runXcoverSession(t, xcover, workDir, []string{"--path", bin, "--scope=" + projectScope, filterArg}, func() {
		t.Log("running fixture binary")
		runCommand(t, workDir, 10*time.Second, bin)
	})
}
