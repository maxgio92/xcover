//go:build integration

package trace_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/trace"
)

// goProjectFixtureSrc is a multi-file Go module with functions from the
// project, stdlib usage, and a vendored dependency simulation. The module
// path is "example.com/testmod".
const goProjectFixtureMain = `package main

import (
	"fmt"
	"example.com/testmod/pkg"
)

//go:noinline
func appLogic() int { return 42 }

func main() {
	fmt.Println(appLogic())
	fmt.Println(pkg.Helper())
}
`

const goProjectFixturePkg = `package pkg

//go:noinline
func Helper() int { return internal() + 7 }

//go:noinline
func internal() int { return 3 }
`

const goProjectFixtureMod = `module example.com/testmod

go 1.26
`

// buildGoProjectFixture creates a Go module in dir, builds a binary, and
// returns the binary path.
func buildGoProjectFixture(t *testing.T, dir string) string {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goProjectFixtureMod), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(goProjectFixtureMain), 0o644))

	pkgDir := filepath.Join(dir, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte(goProjectFixturePkg), 0o644))

	bin := filepath.Join(dir, "testmod")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}

// TestGoProjectResolver_FiltersToModule verifies that GoProjectResolver returns
// only functions belonging to the project module, excluding stdlib.
func TestGoProjectResolver_FiltersToModule(t *testing.T) {
	dir := t.TempDir()
	bin := buildGoProjectFixture(t, dir)

	logger := zerolog.New(os.Stderr).Level(zerolog.DebugLevel)

	// Resolve with project scope.
	resolver := trace.GoProjectResolver(bin, logger, "", "", nil, nil)
	entries, err := resolver(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, e := range entries {
		assert.Truef(t,
			strings.HasPrefix(e.Name, "main.") ||
				strings.HasPrefix(e.Name, "example.com/testmod/") ||
				strings.HasPrefix(e.Name, "example.com/testmod."),
			"function %q should belong to the project module", e.Name)
		// Must not include stdlib.
		assert.Falsef(t, strings.HasPrefix(e.Name, "runtime."), "stdlib function %q leaked", e.Name)
		assert.Falsef(t, strings.HasPrefix(e.Name, "fmt."), "stdlib function %q leaked", e.Name)
	}

	// Our test functions must be present.
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name] = true
	}
	assert.True(t, names["main.appLogic"], "should include main package function")
	assert.True(t, names["main.main"], "should include main function")
	assert.True(t, names["example.com/testmod/pkg.Helper"], "should include sub-package function")
	assert.True(t, names["example.com/testmod/pkg.internal"], "should include unexported sub-package function")
}

// TestGoProjectResolver_VsBinaryScope verifies that project scope returns a
// strict subset of binary scope.
func TestGoProjectResolver_VsBinaryScope(t *testing.T) {
	dir := t.TempDir()
	bin := buildGoProjectFixture(t, dir)

	logger := zerolog.New(os.Stderr).Level(zerolog.DebugLevel)

	allResolver := trace.SymbolTableResolver(bin, logger, "", "", nil, nil)
	allEntries, err := allResolver(t.Context())
	require.NoError(t, err)

	projectResolver := trace.GoProjectResolver(bin, logger, "", "", nil, nil)
	projectEntries, err := projectResolver(t.Context())
	require.NoError(t, err)

	assert.Less(t, len(projectEntries), len(allEntries),
		"project scope should return fewer functions than binary scope")

	t.Logf("binary scope: %d functions, project scope: %d functions",
		len(allEntries), len(projectEntries))

	// Every project entry should exist in the full set.
	allOffsets := make(map[uint64]bool, len(allEntries))
	for _, e := range allEntries {
		allOffsets[e.Offset] = true
	}
	for _, e := range projectEntries {
		assert.Truef(t, allOffsets[e.Offset],
			"project entry %q (offset %#x) not found in binary scope", e.Name, e.Offset)
	}
}

// TestGoProjectResolver_IncludeExcludeFilters verifies that include/exclude
// patterns are applied on top of the module filter.
func TestGoProjectResolver_IncludeExcludeFilters(t *testing.T) {
	dir := t.TempDir()
	bin := buildGoProjectFixture(t, dir)

	logger := zerolog.New(os.Stderr).Level(zerolog.DebugLevel)

	// Include only Helper.
	resolver := trace.GoProjectResolver(bin, logger, "Helper", "", nil, nil)
	entries, err := resolver(t.Context())
	require.NoError(t, err)

	for _, e := range entries {
		assert.Containsf(t, e.Name, "Helper", "include filter should limit to Helper, got %q", e.Name)
	}
}

// TestGoProjectResolver_NonGoBinary verifies that GoProjectResolver returns a
// clear error when pointed at a non-Go binary.
func TestGoProjectResolver_NonGoBinary(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "hello.c")
	require.NoError(t, os.WriteFile(src, []byte(`
#include <stdio.h>
int main(void) { printf("hello\n"); return 0; }
`), 0o644))

	bin := filepath.Join(dir, "hello")
	out, err := exec.Command("gcc", "-o", bin, src).CombinedOutput()
	if err != nil {
		t.Skipf("gcc unavailable: %v: %s", err, out)
	}

	logger := zerolog.New(os.Stderr).Level(zerolog.DebugLevel)
	resolver := trace.GoProjectResolver(bin, logger, "", "", nil, nil)
	_, err = resolver(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Go module path")
}

// TestGoProjectResolver_CommandLineArgumentsBinary verifies that project scope
// rejects Go binaries built from explicit .go files, which do not carry module
// metadata identifying the project.
func TestGoProjectResolver_CommandLineArgumentsBinary(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(src, []byte(`
package main

func main() {}
`), 0o644))

	bin := filepath.Join(dir, "bin")
	cmd := exec.Command("go", "build", "-o", bin, src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	logger := zerolog.New(os.Stderr).Level(zerolog.DebugLevel)
	resolver := trace.GoProjectResolver(bin, logger, "", "", nil, nil)
	_, err = resolver(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command-line-arguments")
}

// TestGoProjectResolver_StrippedGoBinary verifies project scope works on a
// stripped Go binary (falls back to .gopclntab for function resolution,
// debug/buildinfo for module detection).
func TestGoProjectResolver_StrippedGoBinary(t *testing.T) {
	dir := t.TempDir()
	bin := buildGoProjectFixture(t, dir)

	stripped := bin + ".stripped"
	require.NoError(t, exec.Command("cp", bin, stripped).Run())
	out, err := exec.Command("strip", stripped).CombinedOutput()
	if err != nil {
		t.Skipf("strip failed: %v: %s", err, out)
	}

	logger := zerolog.New(os.Stderr).Level(zerolog.DebugLevel)
	resolver := trace.GoProjectResolver(stripped, logger, "", "", nil, nil)
	entries, err := resolver(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, e := range entries {
		assert.Truef(t,
			strings.HasPrefix(e.Name, "main.") ||
				strings.HasPrefix(e.Name, "example.com/testmod/") ||
				strings.HasPrefix(e.Name, "example.com/testmod."),
			"stripped binary: function %q should belong to the project module", e.Name)
	}
}

// TestScopeIntegration_TraceeInit verifies the full tracee init path with
// scope=project, which is how users actually use it via --scope=project.
func TestScopeIntegration_TraceeInit(t *testing.T) {
	dir := t.TempDir()
	bin := buildGoProjectFixture(t, dir)

	logger := zerolog.New(os.Stderr).Level(zerolog.DebugLevel)

	// Binary scope (default) - should have many functions including stdlib.
	binaryTracee := trace.NewUserTracee(
		trace.WithTraceeExePath(bin),
		trace.WithTraceeScope(trace.ScopeBinary),
		trace.WithTraceeLogger(logger),
	)
	require.NoError(t, binaryTracee.Init(t.Context()))
	binaryNames := binaryTracee.GetFuncNames()

	// Project scope - should have only module functions.
	projectTracee := trace.NewUserTracee(
		trace.WithTraceeExePath(bin),
		trace.WithTraceeScope(trace.ScopeProject),
		trace.WithTraceeLogger(logger),
	)
	require.NoError(t, projectTracee.Init(t.Context()))
	projectNames := projectTracee.GetFuncNames()

	assert.Less(t, len(projectNames), len(binaryNames),
		"project scope tracee should resolve fewer functions")

	t.Logf("tracee binary scope: %d functions, project scope: %d functions",
		len(binaryNames), len(projectNames))

	// Project names should all belong to the module.
	for _, name := range projectNames {
		assert.Truef(t,
			strings.HasPrefix(name, "main.") ||
				strings.HasPrefix(name, "example.com/testmod/") ||
				strings.HasPrefix(name, "example.com/testmod."),
			"project scope tracee: function %q should belong to the project module", name)
	}
}
