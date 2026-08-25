package coverage_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/coverage"
)

func TestMergeUnionsTracedAndAck(t *testing.T) {
	r1 := coverage.NewCoverageReport(
		coverage.WithReportFuncsTraced([]string{"main.a", "main.b", "main.c"}),
		coverage.WithReportFuncsAck([]string{"main.a"}),
		coverage.WithReportExePath("mybin"),
	)
	r2 := coverage.NewCoverageReport(
		coverage.WithReportFuncsTraced([]string{"main.a", "main.b", "main.c"}),
		coverage.WithReportFuncsAck([]string{"main.b"}),
		coverage.WithReportExePath("mybin"),
	)

	merged, err := coverage.Merge(r1, r2)
	require.NoError(t, err)

	require.Equal(t, []string{"main.a", "main.b", "main.c"}, merged.FuncsTraced)
	require.Equal(t, []string{"main.a", "main.b"}, merged.FuncsAck)
	// 2 of 3 functions covered across the shards.
	require.InDelta(t, 66.6667, merged.CovByFunc, 0.001)
	require.Equal(t, "mybin", merged.ExePath)
}

func TestMergeDeduplicatesAndSorts(t *testing.T) {
	r1 := coverage.NewCoverageReport(
		coverage.WithReportFuncsTraced([]string{"main.z", "main.a"}),
		coverage.WithReportFuncsAck([]string{"main.z", "main.z"}),
	)
	r2 := coverage.NewCoverageReport(
		coverage.WithReportFuncsTraced([]string{"main.a", "main.m"}),
		coverage.WithReportFuncsAck([]string{"main.a"}),
	)

	merged, err := coverage.Merge(r1, r2)
	require.NoError(t, err)

	require.Equal(t, []string{"main.a", "main.m", "main.z"}, merged.FuncsTraced)
	require.Equal(t, []string{"main.a", "main.z"}, merged.FuncsAck)
}

func TestMergeDivergentExePathsLeftEmpty(t *testing.T) {
	r1 := coverage.NewCoverageReport(
		coverage.WithReportFuncsTraced([]string{"main.a"}),
		coverage.WithReportExePath("binA"),
	)
	r2 := coverage.NewCoverageReport(
		coverage.WithReportFuncsTraced([]string{"main.a"}),
		coverage.WithReportExePath("binB"),
	)

	merged, err := coverage.Merge(r1, r2)
	require.NoError(t, err)

	require.Empty(t, merged.ExePath)
	require.Equal(t, []string{"binA", "binB"}, coverage.ExePaths(r1, r2))
}

func TestMergeNoReportsErrors(t *testing.T) {
	_, err := coverage.Merge()
	require.ErrorIs(t, err, coverage.ErrNoReports)
}

func TestMergeSingleReportRecomputesCoverage(t *testing.T) {
	r := coverage.NewCoverageReport(
		coverage.WithReportFuncsTraced([]string{"main.a", "main.b"}),
		coverage.WithReportFuncsAck([]string{"main.a"}),
		coverage.WithReportFuncsCov(99), // stale value must be recomputed
	)

	merged, err := coverage.Merge(r)
	require.NoError(t, err)
	require.Equal(t, 50.0, merged.CovByFunc)
}
