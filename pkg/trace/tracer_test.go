package trace

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/coverage"
)

const (
	testExcludedSyms = "^runtime.text$|^internal/cpu.Initialize$"
)

func TestHandleEvent_Verbose(t *testing.T) {
	var buf bytes.Buffer

	tracee := NewUserTracee(
		WithTraceeExePath("testdata/gotest"),
		WithTraceeSymPatternExclude(testExcludedSyms),
	)
	tracee.funcs = map[cookie]funcInfo{1: {name: "main.fooFunction"}}
	err := tracee.Init(t.Context())
	require.NoError(t, err)

	tracer := NewUserTracer(
		WithTracerVerbose(true),
		WithTracerWriter(&buf),
		WithTracerTracee(tracee),
	)
	// TODO: need permission to set rlimit while creating the libbpf BPF module.
	//err = tracer.Init()
	//require.NoError(t, err)
	//err = tracer.Load()
	//require.NoError(t, err)

	// Encode the Event
	event := Event{Cookie: 1}
	data := new(bytes.Buffer)
	err = binary.Write(data, binary.LittleEndian, event)
	require.NoError(t, err)

	tracer.handleEvent(data.Bytes())

	require.Contains(t, buf.String(), "main.fooFunction")

	_, ok := tracer.ack.Load(cookie(1))
	require.True(t, ok)
}

// TestHandleEvent_UnknownCookie verifies that an event carrying a cookie not
// present in tracee.funcs is still acked; writeReport is responsible for
// leaving such cookies out of the report.
func TestHandleEvent_UnknownCookie(t *testing.T) {
	var buf bytes.Buffer

	tracee := NewUserTracee(
		WithTraceeExePath("testdata/gotest"),
		WithTraceeSymPatternExclude(testExcludedSyms),
	)
	tracee.funcs = map[cookie]funcInfo{1: {name: "main.fooFunction"}}
	err := tracee.Init(t.Context())
	require.NoError(t, err)

	tracer := NewUserTracer(
		WithTracerVerbose(true),
		WithTracerWriter(&buf),
		WithTracerTracee(tracee),
	)

	// Encode an Event with a cookie that has no matching entry in tracee.funcs.
	event := Event{Cookie: 2}
	data := new(bytes.Buffer)
	err = binary.Write(data, binary.LittleEndian, event)
	require.NoError(t, err)

	tracer.handleEvent(data.Bytes())

	_, ok := tracer.ack.Load(cookie(2))
	require.True(t, ok)
}

// TestWriteReport_UnknownCookie verifies that a cookie with no matching
// function neither truncates funcs_ack nor inflates cov_by_func: the ratio is
// derived from the names listed in funcs_ack. The old Range callback returned
// false on the first unknown cookie, dropping every function visited after it.
func TestWriteReport_UnknownCookie(t *testing.T) {
	tracee := NewUserTracee(WithTraceeExePath("dummy-path"))
	tracee.funcs = map[cookie]funcInfo{
		1: {name: "pkg.Alpha"},
		2: {name: "pkg.Beta"},
		3: {name: "pkg.Gamma"},
		4: {name: "pkg.Delta"},
	}

	tracer := NewUserTracer(WithTracerReport(true), WithTracerTracee(tracee))
	for _, ck := range []cookie{1, 2, 99, 3} {
		tracer.ack.Store(ck, struct{}{})
	}

	reportPath := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, tracer.writeReport(reportPath))

	data, err := os.ReadFile(reportPath)
	require.NoError(t, err)
	var report coverage.CoverageReport
	require.NoError(t, json.Unmarshal(data, &report))

	require.ElementsMatch(t, []string{"pkg.Alpha", "pkg.Beta", "pkg.Gamma"}, report.FuncsAck)
	require.Len(t, report.FuncsTraced, 4)
	require.InDelta(t, 75.0, report.CovByFunc, 1e-9)
	require.Equal(t, "dummy-path", report.ExePath)
}

// TestWriteReport_CreateError verifies that a report path that cannot be
// created is returned as an error so the run exits non-zero instead of
// logging and reporting success.
func TestWriteReport_CreateError(t *testing.T) {
	tracee := NewUserTracee(WithTraceeExePath("dummy-path"))
	tracee.funcs = map[cookie]funcInfo{1: {name: "pkg.Alpha"}}
	tracer := NewUserTracer(WithTracerReport(true), WithTracerTracee(tracee))

	err := tracer.writeReport(filepath.Join(t.TempDir(), "missing-dir", "report.json"))
	require.ErrorContains(t, err, "failed to create report file")
}
