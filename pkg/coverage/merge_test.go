package coverage_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/coverage"
)

var fixedClock = func() time.Time { return time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC) }

func report(buildID string, functions []coverage.FunctionCoverage, opts ...coverage.CoverageReportOption) *coverage.CoverageReport {
	base := []coverage.CoverageReportOption{
		coverage.WithReportBuildID(buildID),
		coverage.WithReportFunctions(functions),
		coverage.WithReportExePath("/bin/app"),
		coverage.WithReportKernel("6.1.0"),
		coverage.WithReportXcoverVersion("v0.0.1"),
		coverage.WithReportGeneratedAt("2026-01-01T00:00:00Z"),
	}

	return coverage.NewCoverageReport(append(base, opts...)...)
}

func TestMerge(t *testing.T) {
	tests := []struct {
		name    string
		reports []*coverage.CoverageReport
		opts    []coverage.MergeOption
		want    *coverage.CoverageReport
		wantErr error
		errMsg  string
	}{
		{
			name:    "zero inputs",
			reports: nil,
			wantErr: coverage.ErrNoReports,
		},
		{
			name:    "nil input",
			reports: []*coverage.CoverageReport{report("abc", nil), nil},
			wantErr: coverage.ErrNilReport,
		},
		{
			name: "hit is OR and duplicate names at different offsets are preserved",
			reports: []*coverage.CoverageReport{
				report("abc", []coverage.FunctionCoverage{
					{Name: "foo", Offset: 1, Hit: false},
					{Name: "foo", Offset: 2, Hit: true},
					{Name: "bar", Offset: 3, Hit: false},
				}),
				report("abc", []coverage.FunctionCoverage{
					{Name: "foo", Offset: 1, Hit: true},
					{Name: "foo", Offset: 2, Hit: false},
					{Name: "baz", Offset: 4, Hit: false},
				}),
			},
			want: coverage.NewCoverageReport(
				coverage.WithReportBuildID("abc"),
				coverage.WithReportExePath("/bin/app"),
				coverage.WithReportKernel("6.1.0"),
				coverage.WithReportXcoverVersion(settings.Version),
				coverage.WithReportGeneratedAt("2026-03-04T05:06:07Z"),
				coverage.WithReportFuncsTraced([]string{"bar", "baz", "foo", "foo"}),
				coverage.WithReportFuncsAck([]string{"foo", "foo"}),
				coverage.WithReportFuncsCov(50),
				coverage.WithReportFunctions([]coverage.FunctionCoverage{
					{Name: "foo", Offset: 1, Hit: true},
					{Name: "foo", Offset: 2, Hit: true},
					{Name: "bar", Offset: 3, Hit: false},
					{Name: "baz", Offset: 4, Hit: false},
				}),
			),
		},
		{
			name: "stale funcs lists and cov_by_func are recomputed from functions",
			reports: []*coverage.CoverageReport{
				report("abc", []coverage.FunctionCoverage{
					{Name: "foo", Offset: 1, Hit: true},
					{Name: "bar", Offset: 2, Hit: false},
				},
					coverage.WithReportFuncsTraced([]string{"stale"}),
					coverage.WithReportFuncsAck([]string{"stale"}),
					coverage.WithReportFuncsCov(100),
				),
			},
			want: coverage.NewCoverageReport(
				coverage.WithReportBuildID("abc"),
				coverage.WithReportExePath("/bin/app"),
				coverage.WithReportKernel("6.1.0"),
				coverage.WithReportXcoverVersion(settings.Version),
				coverage.WithReportGeneratedAt("2026-03-04T05:06:07Z"),
				coverage.WithReportFuncsTraced([]string{"bar", "foo"}),
				coverage.WithReportFuncsAck([]string{"foo"}),
				coverage.WithReportFuncsCov(50),
				coverage.WithReportFunctions([]coverage.FunctionCoverage{
					{Name: "foo", Offset: 1, Hit: true},
					{Name: "bar", Offset: 2, Hit: false},
				}),
			),
		},
		{
			name: "zero traced functions",
			reports: []*coverage.CoverageReport{
				report("abc", []coverage.FunctionCoverage{}),
				report("abc", []coverage.FunctionCoverage{}),
			},
			want: coverage.NewCoverageReport(
				coverage.WithReportBuildID("abc"),
				coverage.WithReportExePath("/bin/app"),
				coverage.WithReportKernel("6.1.0"),
				coverage.WithReportXcoverVersion(settings.Version),
				coverage.WithReportGeneratedAt("2026-03-04T05:06:07Z"),
				coverage.WithReportFuncsTraced([]string{}),
				coverage.WithReportFuncsAck([]string{}),
				coverage.WithReportFuncsCov(0),
				coverage.WithReportFunctions([]coverage.FunctionCoverage{}),
			),
		},
		{
			name: "same build_id different exe_path and kernel keeps them empty",
			reports: []*coverage.CoverageReport{
				report("abc", []coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: true}}),
				report("abc", []coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: false}},
					coverage.WithReportExePath("/other/app"),
					coverage.WithReportKernel("6.2.0"),
				),
			},
			want: coverage.NewCoverageReport(
				coverage.WithReportBuildID("abc"),
				coverage.WithReportXcoverVersion(settings.Version),
				coverage.WithReportGeneratedAt("2026-03-04T05:06:07Z"),
				coverage.WithReportFuncsTraced([]string{"foo"}),
				coverage.WithReportFuncsAck([]string{"foo"}),
				coverage.WithReportFuncsCov(100),
				coverage.WithReportFunctions([]coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: true}}),
			),
		},
		{
			name: "different build_id same exe_path is an error",
			reports: []*coverage.CoverageReport{
				report("abc", []coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: true}}),
				report("def", []coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: true}}),
			},
			wantErr: coverage.ErrBuildIDMismatch,
		},
		{
			name: "empty build_id is an error",
			reports: []*coverage.CoverageReport{
				report("abc", []coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: true}}),
				report("", []coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: true}}),
			},
			wantErr: coverage.ErrBuildIDMissing,
		},
		{
			name: "empty build_id allowed with override, output build_id empty",
			reports: []*coverage.CoverageReport{
				report("abc", []coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: false}}),
				report("", []coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: true}}),
			},
			opts: []coverage.MergeOption{coverage.WithAllowMismatchedBuildID()},
			want: coverage.NewCoverageReport(
				coverage.WithReportExePath("/bin/app"),
				coverage.WithReportKernel("6.1.0"),
				coverage.WithReportXcoverVersion(settings.Version),
				coverage.WithReportGeneratedAt("2026-03-04T05:06:07Z"),
				coverage.WithReportFuncsTraced([]string{"foo"}),
				coverage.WithReportFuncsAck([]string{"foo"}),
				coverage.WithReportFuncsCov(100),
				coverage.WithReportFunctions([]coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: true}}),
			),
		},
		{
			name: "different build_id allowed with override, output build_id empty",
			reports: []*coverage.CoverageReport{
				report("abc", []coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: false}}),
				report("def", []coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: true}}),
			},
			opts: []coverage.MergeOption{coverage.WithAllowMismatchedBuildID()},
			want: coverage.NewCoverageReport(
				coverage.WithReportExePath("/bin/app"),
				coverage.WithReportKernel("6.1.0"),
				coverage.WithReportXcoverVersion(settings.Version),
				coverage.WithReportGeneratedAt("2026-03-04T05:06:07Z"),
				coverage.WithReportFuncsTraced([]string{"foo"}),
				coverage.WithReportFuncsAck([]string{"foo"}),
				coverage.WithReportFuncsCov(100),
				coverage.WithReportFunctions([]coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: true}}),
			),
		},
		{
			name: "matching build_id with override keeps it",
			reports: []*coverage.CoverageReport{
				report("abc", []coverage.FunctionCoverage{}),
				report("abc", []coverage.FunctionCoverage{}),
			},
			opts: []coverage.MergeOption{coverage.WithAllowMismatchedBuildID()},
			want: coverage.NewCoverageReport(
				coverage.WithReportBuildID("abc"),
				coverage.WithReportExePath("/bin/app"),
				coverage.WithReportKernel("6.1.0"),
				coverage.WithReportXcoverVersion(settings.Version),
				coverage.WithReportGeneratedAt("2026-03-04T05:06:07Z"),
				coverage.WithReportFuncsTraced([]string{}),
				coverage.WithReportFuncsAck([]string{}),
				coverage.WithReportFunctions([]coverage.FunctionCoverage{}),
			),
		},
		{
			name: "name conflict at one offset across inputs",
			reports: []*coverage.CoverageReport{
				report("abc", []coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: true}}),
				report("abc", []coverage.FunctionCoverage{{Name: "bar", Offset: 1, Hit: true}}),
			},
			errMsg: "offset 1 is \"foo\" in one report and \"bar\" in another",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := coverage.Merge(tt.reports, append(tt.opts, coverage.WithClock(fixedClock))...)
			if tt.wantErr != nil || tt.errMsg != "" {
				require.Error(t, err)
				require.Nil(t, got)
				if tt.wantErr != nil {
					require.ErrorIs(t, err, tt.wantErr)
				}
				if tt.errMsg != "" {
					require.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMergeStagedEqualsFlat(t *testing.T) {
	a := report("abc", []coverage.FunctionCoverage{
		{Name: "foo", Offset: 1, Hit: true},
		{Name: "bar", Offset: 2, Hit: false},
	})
	b := report("abc", []coverage.FunctionCoverage{
		{Name: "bar", Offset: 2, Hit: true},
		{Name: "baz", Offset: 3, Hit: false},
	}, coverage.WithReportKernel("6.2.0"))
	c := report("abc", []coverage.FunctionCoverage{
		{Name: "qux", Offset: 4, Hit: false},
	}, coverage.WithReportExePath("/other/app"))

	tests := []struct {
		name    string
		reports []*coverage.CoverageReport
		opts    []coverage.MergeOption
	}{
		{name: "matching build_id", reports: []*coverage.CoverageReport{a, b, c}},
		{
			name:    "empty build_id with override stays empty across stages",
			reports: []*coverage.CoverageReport{a, report("", b.Functions, coverage.WithReportKernel("6.2.0")), c},
			opts:    []coverage.MergeOption{coverage.WithAllowMismatchedBuildID()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := append(tt.opts, coverage.WithClock(fixedClock))

			flat, err := coverage.Merge(tt.reports, opts...)
			require.NoError(t, err)

			ab, err := coverage.Merge(tt.reports[:2], opts...)
			require.NoError(t, err)
			staged, err := coverage.Merge([]*coverage.CoverageReport{ab, tt.reports[2]}, opts...)
			require.NoError(t, err)

			require.Equal(t, flat, staged)
			require.Equal(t, settings.Version, flat.XcoverVersion)
			require.Equal(t, []string{"bar", "baz", "foo", "qux"}, flat.FuncsTraced)
			require.Equal(t, []string{"bar", "foo"}, flat.FuncsAck)
			require.Equal(t, "", flat.Kernel)
			require.Equal(t, "", flat.ExePath)
		})
	}
}

func TestMergeDefaultClock(t *testing.T) {
	before := time.Now().UTC().Truncate(time.Second)
	got, err := coverage.Merge([]*coverage.CoverageReport{report("abc", []coverage.FunctionCoverage{})})
	require.NoError(t, err)

	generated, err := time.Parse(time.RFC3339, got.GeneratedAt)
	require.NoError(t, err)
	require.False(t, generated.Before(before))
}
