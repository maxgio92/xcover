//go:build integration

package trace_test

import (
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
	entries, err := resolver()
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
	entries, err := resolver()
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
	_, err := resolver()
	assert.ErrorIs(t, err, trace.ErrNoFunctionSymbols)
}
