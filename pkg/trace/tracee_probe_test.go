package trace_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/internal/utils"
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

	wantOffsetByCookie := make(map[uint64]uint64, len(probeTestEntries))
	for _, e := range probeTestEntries {
		wantOffsetByCookie[utils.Hash(e.Name)] = e.Offset
	}

	for i := range cookies {
		wantOffset, ok := wantOffsetByCookie[cookies[i]]
		require.Truef(t, ok, "unexpected cookie %d", cookies[i])
		require.Equalf(t, wantOffset, offsets[i],
			"cookie %d paired with offset %#x, want %#x", cookies[i], offsets[i], wantOffset)
	}
}
