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
}
