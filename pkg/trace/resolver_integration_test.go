//go:build integration

package trace_test

import (
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
	require.NotEmpty(t, entries)

	for _, e := range entries {
		assert.Regexp(t, `^func_0x[0-9a-f]+$`, e.Name)
		assert.NotZero(t, e.Offset)
	}
}
