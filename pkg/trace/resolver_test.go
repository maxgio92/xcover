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

// TestWithTraceeResolver_CustomResolver verifies that a custom FunctionResolver
// injected via WithTraceeResolver is called by Init() instead of the default.
// The custom resolver returns hardcoded entries - no real binary is opened.
func TestWithTraceeResolver_CustomResolver(t *testing.T) {
	want := []trace.FunctionEntry{
		{Name: "custom.Alpha", Offset: 0x1000},
		{Name: "custom.Beta", Offset: 0x2000},
	}

	called := false
	custom := func() ([]trace.FunctionEntry, error) {
		called = true
		return want, nil
	}

	tracee := trace.NewUserTracee(
		trace.WithTraceeExePath("dummy-path"),
		trace.WithTraceeResolver(custom),
		trace.WithTraceeLogger(testLogger),
	)
	err := tracee.Init()
	require.NoError(t, err)
	assert.True(t, called, "custom resolver was not called")

	names := tracee.GetFuncNames()
	assert.Contains(t, names, "custom.Alpha")
	assert.Contains(t, names, "custom.Beta")

	offsets := tracee.GetFuncOffsets()
	assert.Contains(t, offsets, uint64(0x1000))
	assert.Contains(t, offsets, uint64(0x2000))
}
