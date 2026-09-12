package trace_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/trace"
)

// TestWithTraceeResolver_CustomResolver verifies that a custom FunctionResolver
// injected via WithTraceeResolver is called by Init() instead of the default.
// The custom resolver returns hardcoded entries - no real binary is opened.
func TestWithTraceeResolver_CustomResolver(t *testing.T) {
	want := []trace.FunctionEntry{
		{Name: "custom.Alpha", Offset: 0x1000},
		{Name: "custom.Beta", Offset: 0x2000},
	}

	called := false
	custom := func(_ context.Context) ([]trace.FunctionEntry, error) {
		called = true
		return want, nil
	}

	tracee := trace.NewUserTracee(
		trace.WithTraceeExePath("dummy-path"),
		trace.WithTraceeResolver(custom),
		trace.WithTraceeLogger(testLogger),
	)
	err := tracee.Init(t.Context())
	require.NoError(t, err)
	assert.True(t, called, "custom resolver was not called")

	names := tracee.GetFuncNames()
	assert.Contains(t, names, "custom.Alpha")
	assert.Contains(t, names, "custom.Beta")

	offsets, _ := tracee.GetFuncProbes()
	assert.Contains(t, offsets, uint64(0x1000))
	assert.Contains(t, offsets, uint64(0x2000))
}

// TestSymbolTableResolver_InvalidPattern verifies that a malformed --include
// or --exclude regex is reported as an error from the resolver instead of
// panicking while symbols are filtered. The pattern is checked before the
// binary is opened, so no real file is needed.
func TestSymbolTableResolver_InvalidPattern(t *testing.T) {
	_, err := trace.SymbolTableResolver("dummy-path", testLogger, "(", "", nil, nil)(t.Context())
	require.ErrorContains(t, err, "invalid include pattern")

	_, err = trace.SeparateDebugResolver("dummy-path", "dummy-debug", testLogger, "", "[", nil, nil, true)(t.Context())
	require.ErrorContains(t, err, "invalid exclude pattern")
}
