package trace_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/trace"
)

// staticResolver returns a fixed set of entries without opening any binary.
func staticResolver(entries []trace.FunctionEntry) trace.FunctionResolver {
	return func(_ context.Context) ([]trace.FunctionEntry, error) {
		return entries, nil
	}
}

var probeTestEntries = []trace.FunctionEntry{
	{Name: "pkg.Alpha", Offset: 0x1000},
	{Name: "pkg.Beta", Offset: 0x2000},
	{Name: "pkg.Gamma", Offset: 0x3000},
}

func newProbeTestTracee(t *testing.T) *trace.UserTracee {
	t.Helper()
	tracee := trace.NewUserTracee(
		trace.WithTraceeExePath("dummy-path"),
		trace.WithTraceeResolver(staticResolver(probeTestEntries)),
		trace.WithTraceeLogger(testLogger),
	)
	require.NoError(t, tracee.Init(t.Context()))
	return tracee
}

// TestUserTracee_GetterLengths asserts the getters return exactly one element
// per resolved function. The previous implementation pre-sized the slices with
// make([]T, len(funcs)) and then appended, yielding 2N elements whose first N
// were zero/empty values.
func TestUserTracee_GetterLengths(t *testing.T) {
	tracee := newProbeTestTracee(t)
	n := len(probeTestEntries)

	require.Len(t, tracee.GetFuncOffsets(), n, "offsets slice has phantom entries")
	require.Len(t, tracee.GetFuncCookies(), n, "cookies slice has phantom entries")
	require.Len(t, tracee.GetFuncNames(), n, "names slice has phantom entries")
}

// TestUserTracee_GetFuncProbes_Alignment asserts that offsets and cookies are
// index-aligned: offsets[i] is the offset of the function identified by
// cookies[i]. The previous code derived offsets and cookies from two
// independent range loops over a map; Go randomizes map iteration order, so the
// two slices could describe functions in different orders, attaching a uprobe
// at one function's offset under another function's cookie.
func TestUserTracee_GetFuncProbes_Alignment(t *testing.T) {
	tracee := newProbeTestTracee(t)

	offsets, cookies := tracee.GetFuncProbes()
	require.Len(t, offsets, len(probeTestEntries))
	require.Len(t, cookies, len(probeTestEntries))

	// The cookie reported when a probe fires must identify the function whose
	// probe is attached at offsets[i]. Cookies are keyed by offset, so the
	// alignment invariant is offsets[i] == cookies[i]; building the two slices
	// from independent map ranges would break this.
	for i := range cookies {
		require.Equalf(t, offsets[i], cookies[i],
			"offsets[%d]=%#x not aligned with cookies[%d]=%#x", i, offsets[i], i, cookies[i])
	}

	// Every injected offset must be present exactly once.
	want := make(map[uint64]int, len(probeTestEntries))
	for _, e := range probeTestEntries {
		want[e.Offset]++
	}
	got := make(map[uint64]int, len(offsets))
	for _, o := range offsets {
		got[o]++
	}
	require.Equal(t, want, got)
}

// TestUserTracee_DuplicateNames_NotDropped verifies that two distinct functions
// sharing a name (e.g. C statics in different translation units, or C++
// overloads sharing a DWARF DW_AT_name) are both traced. Keying probes by
// name-hash collapses them into one map entry, silently dropping a function and
// undercounting coverage.
func TestUserTracee_DuplicateNames_NotDropped(t *testing.T) {
	entries := []trace.FunctionEntry{
		{Name: "helper", Offset: 0x1160},
		{Name: "helper", Offset: 0x11a0}, // same name, different function
		{Name: "unique", Offset: 0x1200},
	}
	tracee := trace.NewUserTracee(
		trace.WithTraceeExePath("dummy-path"),
		trace.WithTraceeResolver(staticResolver(entries)),
		trace.WithTraceeLogger(testLogger),
	)
	require.NoError(t, tracee.Init(t.Context()))

	offsets, cookies := tracee.GetFuncProbes()
	require.Len(t, offsets, len(entries), "a duplicate-named function was dropped")
	require.Len(t, cookies, len(entries))

	gotOffset := make(map[uint64]bool, len(offsets))
	for _, o := range offsets {
		gotOffset[o] = true
	}
	for _, e := range entries {
		require.Truef(t, gotOffset[e.Offset], "offset %#x for %q missing", e.Offset, e.Name)
	}
}
