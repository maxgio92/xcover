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

	offsets := tracee.GetFuncOffsets()
	assert.Contains(t, offsets, uint64(0x1000))
	assert.Contains(t, offsets, uint64(0x2000))
}
