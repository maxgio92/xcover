package coverage_test

import (
	"bytes"
	"encoding/json"
	"github.com/maxgio92/xcover/pkg/coverage"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewReportWithOptions(t *testing.T) {
	traced := []string{"foo", "bar"}
	ack := []string{"foo"}
	cov := 0.5

	report := coverage.NewCoverageReport(
		coverage.WithReportFuncsTraced(traced),
		coverage.WithReportFuncsAck(ack),
		coverage.WithReportFuncsCov(cov),
	)

	require.Equal(t, traced, report.FuncsTraced)
	require.Equal(t, ack, report.FuncsAck)
	require.Equal(t, cov, report.CovByFunc)
}

func TestWriteReportJSONOutput(t *testing.T) {
	report := coverage.NewCoverageReport(
		coverage.WithReportFuncsTraced([]string{"foo"}),
		coverage.WithReportFuncsAck([]string{"foo"}),
		coverage.WithReportFuncsCov(1.0),
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
	)

	var buf bytes.Buffer
	err := report.WriteReport(&buf)
	require.NoError(t, err)

	output := buf.String()
	require.True(t, strings.Contains(output, "traceFunc"))
	require.True(t, strings.Contains(output, "main.foo"))
	require.True(t, strings.Contains(output, "cov_by_func"))
	require.True(t, strings.Contains(output, "exe_path"))
	require.False(t, strings.Contains(output, "symbols"), "symbols must be omitted when no name was demangled")
}

// TestWriteReportSymbols checks that the symbols map round-trips and that an
// empty map is omitted from the JSON so Go and C reports keep their schema.
func TestWriteReportSymbols(t *testing.T) {
	symbols := map[string]string{"_ZN3app3net5parseEi": "app::net::parse(int)"}
	report := coverage.NewCoverageReport(
		coverage.WithReportFuncsTraced([]string{"_ZN3app3net5parseEi", "main"}),
		coverage.WithReportFuncsAck([]string{"_ZN3app3net5parseEi"}),
		coverage.WithReportSymbols(symbols),
	)

	var buf bytes.Buffer
	require.NoError(t, report.WriteReport(&buf))

	var parsed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	var got map[string]string
	require.NoError(t, json.Unmarshal(parsed["symbols"], &got))
	require.Equal(t, symbols, got)

	buf.Reset()
	empty := coverage.NewCoverageReport(coverage.WithReportSymbols(map[string]string{}))
	require.NoError(t, empty.WriteReport(&buf))
	require.NotContains(t, buf.String(), "symbols")
}
