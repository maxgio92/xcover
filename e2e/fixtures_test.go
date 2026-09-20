//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuildGoFixturePrebuilt checks that buildGoFixture returns the prebuilt
// binary and does not build when XCOVER_E2E_FIXTURES is set. It needs no
// root and no xcover binary.
func TestBuildGoFixturePrebuilt(t *testing.T) {
	fixtures := t.TempDir()
	want := filepath.Join(fixtures, projectScopeGoScenario)
	if err := os.WriteFile(want, []byte("not a real binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(fixturesEnv, fixtures)
	// An empty PATH makes any go build attempt fail loudly.
	t.Setenv("PATH", "")

	workDir := t.TempDir()
	got := buildGoFixture(t, workDir, projectScopeGoScenario)
	if got != want {
		t.Fatalf("buildGoFixture returned %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(workDir, "fixture")); !os.IsNotExist(err) {
		t.Fatalf("fixture copy in %s should not exist, stat err: %v", workDir, err)
	}
}
