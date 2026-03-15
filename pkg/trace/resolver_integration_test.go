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

// TestSymbolTableResolver_Symtab calls SymbolTableResolver against an unstripped
// binary, exercising the ELF symbol table path.
func TestSymbolTableResolver_Symtab(t *testing.T) {
	resolver := trace.SymbolTableResolver(testBinary, testLogger, "", testExcludedSyms, nil, nil)
	entries, err := resolver(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
	for _, e := range entries {
		assert.NotEmpty(t, e.Name)
		assert.NotZero(t, e.Offset)
	}
}

// TestSymbolTableResolver_GoPclntab verifies that SymbolTableResolver falls back
// to .gopclntab for a stripped Go binary, and that the returned offsets are
// valid file offsets (not virtual addresses) within the .text section.
//
// This also covers Go 1.26+ compat: pcHeader.textStart is always zero in 1.26,
// so the resolver must derive the text base from the ELF .text section header
// rather than from pcHeader.
func TestSymbolTableResolver_GoPclntab(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xcover-gopclntab-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	src := filepath.Join(tmpDir, "main.go")
	err = os.WriteFile(src, []byte(`package main

import "fmt"

//go:noinline
func add(a, b int) int { return a + b }

//go:noinline
func greet(name string) { fmt.Println("hello", name) }

func main() { fmt.Println(add(1, 2)); greet("world") }
`), 0644)
	require.NoError(t, err)

	bin := filepath.Join(tmpDir, "bin")
	require.NoError(t, exec.Command("go", "build", "-o", bin, src).Run())
	require.NoError(t, exec.Command("strip", bin).Run())

	ef, err := elf.Open(bin)
	require.NoError(t, err)
	defer ef.Close()

	textSec := ef.Section(".text")
	require.NotNil(t, textSec)
	require.NotNil(t, ef.Section(".gopclntab"), ".gopclntab must survive strip")

	resolver := trace.SymbolTableResolver(bin, testLogger, `^main\.`, "", nil, nil)
	entries, err := resolver(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, e := range entries {
		assert.GreaterOrEqual(t, e.Offset, textSec.Offset,
			"%s: offset should be >= .text file offset", e.Name)
		assert.Less(t, e.Offset, textSec.Offset+textSec.Size,
			"%s: offset should be within .text section", e.Name)
	}
}

// TestRecoveryResolver_StrippedBinary verifies that RecoveryResolver returns
// entries with synthesized func_0x<addr> names and valid file offsets
// from a stripped C binary.
func TestRecoveryResolver_StrippedBinary(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xcover-recovery-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	src := filepath.Join(tmpDir, "main.c")
	err = os.WriteFile(src, []byte(`
#include <stdio.h>
void greet(const char *name) { printf("hello %s\n", name); }
void farewell(const char *name) { printf("bye %s\n", name); }
int main() { greet("world"); farewell("world"); return 0; }
`), 0644)
	require.NoError(t, err)

	bin := filepath.Join(tmpDir, "bin")
	out, err := exec.Command("gcc", "-o", bin, src).CombinedOutput()
	if err != nil {
		t.Skipf("gcc not available: %v: %s", err, out)
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

// TestSymbolTableResolver_Filters verifies include and exclude pattern filtering.
func TestSymbolTableResolver_Filters(t *testing.T) {
	t.Run("Include", func(t *testing.T) {
		resolver := trace.SymbolTableResolver(testBinary, testLogger, `^main\.`, "", nil, nil)
		entries, err := resolver(t.Context())
		require.NoError(t, err)
		assert.NotEmpty(t, entries)
		for _, e := range entries {
			assert.Regexp(t, `^main\.`, e.Name)
		}
	})

	t.Run("Exclude", func(t *testing.T) {
		resolver := trace.SymbolTableResolver(testBinary, testLogger, "", `^runtime\.`, nil, nil)
		entries, err := resolver(t.Context())
		require.NoError(t, err)
		assert.NotEmpty(t, entries)
		for _, e := range entries {
			assert.NotRegexp(t, `^runtime\.`, e.Name)
		}
	})
}

// TestSymbolTableResolver_NoMatch verifies that ErrNoFunctionSymbols is
// returned when the include pattern matches nothing.
func TestSymbolTableResolver_NoMatch(t *testing.T) {
	resolver := trace.SymbolTableResolver(testBinary, testLogger, `^nonexistentsymbol\.$`, "", nil, nil)
	_, err := resolver(t.Context())
	assert.ErrorIs(t, err, trace.ErrNoFunctionSymbols)
}
