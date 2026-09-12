//go:build integration

package trace_test

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/trace"
)

// TestSymbolTableResolver_Direct calls SymbolTableResolver directly with a real
// binary path, bypassing UserTracee. This verifies the resolver contract
// independently of the tracee wiring.
func TestSymbolTableResolver_Direct(t *testing.T) {
	resolver := trace.SymbolTableResolver(testBinary, testLogger, "", testExcludedSyms, nil, nil)
	entries, err := resolver(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
	for _, e := range entries {
		assert.NotEmpty(t, e.Name)
		assert.NotZero(t, e.Offset)
		// The Go linker's zero-size end-of-code marker is not a function.
		assert.NotEqual(t, "runtime.etext", e.Name)
	}
}

// TestSymbolTableResolver_IncludePattern verifies that the include filter is
// applied when calling the resolver directly.
func TestSymbolTableResolver_IncludePattern(t *testing.T) {
	resolver := trace.SymbolTableResolver(testBinary, testLogger, `^main\.`, "", nil, nil)
	entries, err := resolver(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
	for _, e := range entries {
		assert.Regexp(t, `^main\.`, e.Name)
	}
}

// TestSymbolTableResolver_NoMatch verifies that ErrNoFunctionSymbols is
// returned when the include pattern matches nothing.
func TestSymbolTableResolver_NoMatch(t *testing.T) {
	resolver := trace.SymbolTableResolver(testBinary, testLogger, `^nonexistentsymbol\.$`, "", nil, nil)
	_, err := resolver(t.Context())
	assert.ErrorIs(t, err, trace.ErrNoFunctionSymbols)
}

// TestRecoveryResolver verifies that RecoveryResolver returns
// entries with synthesized func_0x<addr> names and valid file offsets
// from a stripped C binary.
func TestRecoveryResolver(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xcover-recovery-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	src := filepath.Join(tmpDir, "main.c")
	err = os.WriteFile(src, []byte(`
#include <stdio.h>

void greet(const char *name) {
	printf("hello %s\n", name);
}

void farewell(const char *name) {
	printf("bye %s\n", name);
}

int main() {
	greet("world");
	farewell("world");
	return 0;
}
`), 0644)
	require.NoError(t, err)

	bin := filepath.Join(tmpDir, "bin")
	out, err := exec.Command("gcc", "-o", bin, src).CombinedOutput()
	if err != nil {
		t.Errorf("gcc not available: %v: %s", err, out)
	}
	require.NoError(t, exec.Command("strip", bin).Run())

	resolver := trace.RecoveryResolver(bin, testLogger)
	entries, err := resolver(t.Context())
	require.NoError(t, err)
	// The C source defines greet, farewell, and main; _start is the ELF entry point.
	assert.Len(t, entries, 4)

	for _, e := range entries {
		assert.Regexp(t, `^func_0x[0-9a-f]+$`, e.Name)
		assert.NotZero(t, e.Offset)
	}
}

// TestSymbolTableResolver_PIESkipsUndefinedImports compiles a dynamically
// linked PIE, whose first PT_LOAD has Vaddr 0, and checks that undefined
// imports such as puts@GLIBC_2.2.5 (STT_FUNC, SHN_UNDEF, Value 0) are not
// resolved to file offset 0 and probed as functions.
func TestSymbolTableResolver_PIESkipsUndefinedImports(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not available")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.c")
	require.NoError(t, os.WriteFile(src, []byte(`
#include <stdio.h>
__attribute__((noinline)) void greet(void) { puts("hello"); }
int main(void) { greet(); return 0; }
`), 0o644))

	bin := filepath.Join(dir, "pie")
	if out, err := exec.Command("gcc", "-O0", "-fPIE", "-pie", "-o", bin, src).CombinedOutput(); err != nil {
		t.Fatalf("gcc failed: %v: %s", err, out)
	}

	// Collect the fixture's undefined symbols (puts@GLIBC_2.2.5 and friends)
	// so the check does not depend on the versioned spelling.
	f, err := elf.Open(bin)
	require.NoError(t, err)
	defer f.Close()
	syms, err := f.Symbols()
	require.NoError(t, err)
	undefined := make(map[string]bool)
	for _, s := range syms {
		if s.Section == elf.SHN_UNDEF && elf.ST_TYPE(s.Info) == elf.STT_FUNC && s.Name != "" {
			undefined[s.Name] = true
		}
	}
	require.NotEmpty(t, undefined, "fixture has no undefined function imports; puts should be imported")

	entries, err := trace.SymbolTableResolver(bin, testLogger, "", "", nil, nil)(t.Context())
	require.NoError(t, err)

	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name] = true
		assert.NotZerof(t, e.Offset, "function %q resolved to file offset 0", e.Name)
		assert.Falsef(t, undefined[e.Name], "undefined import %q leaked into the function list", e.Name)
	}
	assert.True(t, names["greet"], "expected defined function greet, got %v", entries)
	assert.True(t, names["main"], "expected defined function main, got %v", entries)
}
