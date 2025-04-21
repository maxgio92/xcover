package trace_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/maxgio92/utrace/pkg/trace"
)

func TestNewReportWithOptions(t *testing.T) {
	traced := []string{"foo", "bar"}
	ack := []string{"foo"}
	cov := 0.5

	report := trace.NewReport(
		trace.WithReportFuncsTraced(traced),
		trace.WithReportFuncsAck(ack),
		trace.WithReportFuncsCov(cov),
	)

	require.Equal(t, traced, report.FuncsTraced)
	require.Equal(t, ack, report.FuncsAck)
	require.Equal(t, cov, report.CovByFunc)
}

func TestWriteReportJSONOutput(t *testing.T) {
	report := trace.NewReport(
		trace.WithReportFuncsTraced([]string{"foo"}),
		trace.WithReportFuncsAck([]string{"foo"}),
		trace.WithReportFuncsCov(1.0),
	)

	var buf bytes.Buffer
	err := report.WriteReport(&buf)
	require.NoError(t, err)

	var parsed trace.UserTraceReport
	err = json.Unmarshal(buf.Bytes(), &parsed)
	require.NoError(t, err)

	require.Equal(t, report, &parsed)
}

func TestWriteReportToBufferContainsExpectedFields(t *testing.T) {
	report := trace.NewReport(
		trace.WithReportFuncsTraced([]string{"traceFunc"}),
		trace.WithReportFuncsAck([]string{"main.foo"}),
		trace.WithReportFuncsCov(0.25),
	)

	var buf bytes.Buffer
	err := report.WriteReport(&buf)
	require.NoError(t, err)

	output := buf.String()
	require.True(t, strings.Contains(output, "traceFunc"))
	require.True(t, strings.Contains(output, "main.foo"))
	require.True(t, strings.Contains(output, "cov_by_func"))
}
