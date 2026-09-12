package trace

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/coverage"
)

const (
	testExcludedSyms = "^runtime.text$|^internal/cpu.Initialize$"
	// testGotestBuildID is the GNU build-id of testdata/gotest (readelf -n).
	testGotestBuildID = "8ba13a038a6fdfddce17d7fb532a38340b1aedbf"
)

func encodeEvent(t *testing.T, ck cookie) []byte {
	t.Helper()
	data := new(bytes.Buffer)
	require.NoError(t, binary.Write(data, binary.LittleEndian, Event{Cookie: ck}))
	return data.Bytes()
}

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

	tracer.handleEvent(encodeEvent(t, 1))

	require.Contains(t, buf.String(), "main.fooFunction")

	_, ok := tracer.ack.Load(cookie(1))
	require.True(t, ok)
}

// TestHandleEvent_UnknownCookie verifies that an event carrying a cookie not
// present in tracee.funcs is not acked, so it cannot count toward coverage.
func TestHandleEvent_UnknownCookie(t *testing.T) {
	var buf bytes.Buffer

	tracee := NewUserTracee(WithTraceeExePath("testdata/gotest"))
	tracee.funcs = map[cookie]funcInfo{1: {name: "main.fooFunction"}}

	tracer := NewUserTracer(
		WithTracerVerbose(true),
		WithTracerWriter(&buf),
		WithTracerTracee(tracee),
	)

	tracer.handleEvent(encodeEvent(t, 2))

	_, ok := tracer.ack.Load(cookie(2))
	require.False(t, ok)
	require.Empty(t, buf.String())
}

// TestHandleEvent_DecodeError verifies that a truncated event is dropped
// instead of being acked as the zero cookie.
func TestHandleEvent_DecodeError(t *testing.T) {
	tracee := NewUserTracee(WithTraceeExePath("testdata/gotest"))
	tracee.funcs = map[cookie]funcInfo{0: {name: "main.zero"}}

	tracer := NewUserTracer(WithTracerTracee(tracee))

	tracer.handleEvent([]byte{0x01, 0x02})

	_, ok := tracer.ack.Load(cookie(0))
	require.False(t, ok)
}

func newReportTracer(t *testing.T) *UserTracer {
	t.Helper()
	tracee := NewUserTracee(WithTraceeExePath("testdata/gotest"))
	tracee.funcs = map[cookie]funcInfo{
		0x30: {name: "main.c", offset: 0x30},
		0x10: {name: "main.b", offset: 0x10},
		0x20: {name: "main.a", offset: 0x20},
		// Two functions folded onto the same offset sort by name.
		0x40: {name: "main.z", offset: 0x40},
	}
	return NewUserTracer(WithTracerReport(true), WithTracerTracee(tracee))
}

func readReport(t *testing.T, path string) coverage.CoverageReport {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var report coverage.CoverageReport
	require.NoError(t, json.Unmarshal(data, &report))
	return report
}

func TestWriteReport(t *testing.T) {
	tracer := newReportTracer(t)
	tracer.ack.Store(cookie(0x30), struct{}{})
	tracer.ack.Store(cookie(0x10), struct{}{})
	// A stale cookie with no function must be skipped, not counted, and must
	// not truncate the ack list.
	tracer.ack.Store(cookie(0x99), struct{}{})

	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, tracer.writeReport(path))
	report := readReport(t, path)

	require.Equal(t, coverage.SchemaVersion, report.SchemaVersion)
	require.Equal(t, settings.Version, report.XcoverVersion)
	require.Equal(t, "testdata/gotest", report.ExePath)
	require.Equal(t, testGotestBuildID, report.BuildID)
	require.NotEmpty(t, report.Kernel)
	generatedAt, err := time.Parse(time.RFC3339, report.GeneratedAt)
	require.NoError(t, err)
	require.Equal(t, time.UTC, generatedAt.Location())

	require.Equal(t, []string{"main.a", "main.b", "main.c", "main.z"}, report.FuncsTraced)
	require.Equal(t, []string{"main.b", "main.c"}, report.FuncsAck)
	require.InDelta(t, 50.0, report.CovByFunc, 1e-9)
	require.Equal(t, []coverage.FunctionCoverage{
		{Name: "main.b", Offset: 0x10, Hit: true},
		{Name: "main.a", Offset: 0x20, Hit: false},
		{Name: "main.c", Offset: 0x30, Hit: true},
		{Name: "main.z", Offset: 0x40, Hit: false},
	}, report.Functions)
}

func TestWriteReport_Deterministic(t *testing.T) {
	tracer := newReportTracer(t)
	tracer.ack.Store(cookie(0x20), struct{}{})

	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	require.NoError(t, tracer.writeReport(first))
	require.NoError(t, tracer.writeReport(second))

	// Compare raw JSON with generated_at removed so encoder-level changes
	// (field names, number formatting) are caught, not only slice order.
	require.Equal(t, reportBytesWithoutTimestamp(t, first), reportBytesWithoutTimestamp(t, second))
}

func reportBytesWithoutTimestamp(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &fields))
	delete(fields, "generated_at")
	out, err := json.Marshal(fields)
	require.NoError(t, err)
	return out
}

func TestWriteReport_EmptyAck(t *testing.T) {
	tracer := newReportTracer(t)

	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, tracer.writeReport(path))
	report := readReport(t, path)

	require.NotNil(t, report.FuncsAck)
	require.Empty(t, report.FuncsAck)
	require.Zero(t, report.CovByFunc)
}

func TestWriteReport_CreateError(t *testing.T) {
	tracer := newReportTracer(t)

	err := tracer.writeReport(filepath.Join(t.TempDir(), "missing", "report.json"))
	require.Error(t, err)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestWriteReport_Disabled(t *testing.T) {
	tracer := NewUserTracer(WithTracerTracee(NewUserTracee(WithTraceeExePath("testdata/gotest"))))

	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, tracer.writeReport(path))
	require.NoFileExists(t, path)
}
