package coverage_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/coverage"
)

func TestNewReportWithOptions(t *testing.T) {
	traced := []string{"foo", "bar"}
	ack := []string{"foo"}
	cov := 0.5
	functions := []coverage.FunctionCoverage{
		{Name: "bar", Offset: 0x10, Hit: false},
		{Name: "foo", Offset: 0x20, Hit: true},
	}

	report := coverage.NewCoverageReport(
		coverage.WithReportFuncsTraced(traced),
		coverage.WithReportFuncsAck(ack),
		coverage.WithReportFuncsCov(cov),
		coverage.WithReportBuildID("abc123"),
		coverage.WithReportKernel("6.1.0"),
		coverage.WithReportXcoverVersion("v1.2.3"),
		coverage.WithReportGeneratedAt("2026-01-02T03:04:05Z"),
		coverage.WithReportFunctions(functions),
	)

	require.Equal(t, coverage.SchemaVersion, report.SchemaVersion)
	require.Equal(t, traced, report.FuncsTraced)
	require.Equal(t, ack, report.FuncsAck)
	require.Equal(t, cov, report.CovByFunc)
	require.Equal(t, "abc123", report.BuildID)
	require.Equal(t, "6.1.0", report.Kernel)
	require.Equal(t, "v1.2.3", report.XcoverVersion)
	require.Equal(t, "2026-01-02T03:04:05Z", report.GeneratedAt)
	require.Equal(t, functions, report.Functions)
}

func TestNewReportDefaultsSchemaVersion(t *testing.T) {
	require.Equal(t, 1, coverage.NewCoverageReport().SchemaVersion)
}

func TestWriteReportJSONOutput(t *testing.T) {
	report := coverage.NewCoverageReport(
		coverage.WithReportFuncsTraced([]string{"foo"}),
		coverage.WithReportFuncsAck([]string{"foo"}),
		coverage.WithReportFuncsCov(1.0),
		coverage.WithReportFunctions([]coverage.FunctionCoverage{{Name: "foo", Offset: 1, Hit: true}}),
	)

	var buf bytes.Buffer
	err := report.WriteReport(&buf)
	require.NoError(t, err)

	var parsed coverage.CoverageReport
	err = json.Unmarshal(buf.Bytes(), &parsed)
	require.NoError(t, err)

	require.Equal(t, report, &parsed)
}

func TestWriteReportToBufferContainsExpectedFields(t *testing.T) {
	report := coverage.NewCoverageReport(
		coverage.WithReportFuncsTraced([]string{"traceFunc"}),
		coverage.WithReportFuncsAck([]string{"main.foo"}),
		coverage.WithReportFuncsCov(0.25),
		coverage.WithReportExePath("mybin"),
		coverage.WithReportFunctions([]coverage.FunctionCoverage{{Name: "main.foo", Offset: 4096, Hit: true}}),
	)

	var buf bytes.Buffer
	err := report.WriteReport(&buf)
	require.NoError(t, err)

	output := buf.String()
	require.True(t, strings.Contains(output, "traceFunc"))
	require.True(t, strings.Contains(output, "main.foo"))
	for _, key := range []string{
		"schema_version", "xcover_version", "generated_at", "kernel", "exe_path",
		"build_id", "funcs_traced", "funcs_ack", "cov_by_func", "functions",
	} {
		require.Contains(t, output, `"`+key+`"`)
	}
	// Offsets are emitted as decimal integers, not hex strings.
	require.Contains(t, output, `"offset":4096`)
}
