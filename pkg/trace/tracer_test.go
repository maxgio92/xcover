package trace

import (
	"bytes"
	"encoding/hex"
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

func TestHandleEvent_Verbose(t *testing.T) {
	var buf bytes.Buffer

	tracee := NewUserTracee(
		WithTraceeExePath("testdata/gotest"),
		WithTraceeSymPatternExclude(testExcludedSyms),
	)
	tracee.funcs = map[cookie]funcInfo{1: {name: "main.fooFunction", demangled: "main.fooFunction"}}
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

func TestWithTracerPID(t *testing.T) {
	require.Equal(t, -1, NewUserTracer().pid, "default must trace every process")
	require.Equal(t, 1234, NewUserTracer(WithTracerPID(1234)).pid)
}

func newReportTracer(t *testing.T) *UserTracer {
	t.Helper()
	tracee := NewUserTracee(WithTraceeExePath("testdata/gotest"))
	id, err := hex.DecodeString(testGotestBuildID)
	require.NoError(t, err)
	tracee.buildID = id
	tracee.funcs = map[cookie]funcInfo{
		0x30: {name: "main.c", offset: 0x30},
		0x10: {name: "main.b", offset: 0x10},
		0x20: {name: "main.a", offset: 0x20},
		0x40: {name: "main.z", offset: 0x40},
	}
	return NewUserTracer(WithTracerReport(true), WithTracerTracee(tracee))
}

// readReport reads a report the tracer wrote through coverage.ReadReport, so
// every test here also checks that the producer output passes the consumer's
// validation.
func readReport(t *testing.T, path string) *coverage.CoverageReport {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	report, err := coverage.ReadReport(f)
	require.NoError(t, err)
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

// TestWriteReport_BuildIDCapturedAtInit verifies that the report carries the
// build-id of the binary whose functions were resolved, even when the file is
// no longer at exe_path when the report is written.
func TestWriteReport_BuildIDCapturedAtInit(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "gotest")
	fixture, err := os.ReadFile("testdata/gotest")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(exePath, fixture, 0o755))

	tracee := NewUserTracee(
		WithTraceeExePath(exePath),
		WithTraceeSymPatternExclude(testExcludedSyms),
	)
	require.NoError(t, tracee.Init(t.Context()))
	require.Equal(t, testGotestBuildID, tracee.exeBuildID())

	require.NoError(t, os.Rename(exePath, filepath.Join(dir, "gotest.rebuilt")))

	tracer := NewUserTracer(WithTracerReport(true), WithTracerTracee(tracee))
	path := filepath.Join(dir, "report.json")
	require.NoError(t, tracer.writeReport(path))
	report := readReport(t, path)

	require.Equal(t, exePath, report.ExePath)
	require.Equal(t, testGotestBuildID, report.BuildID)
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

// TestWriteReport_MergeRoundTrip writes a report through the tracer, reads it
// back with coverage.ReadReport and merges it with itself. The merge must
// give the input back apart from generated_at, which shows that the producer
// and coverage.Merge agree on ordering, cov_by_func and the metadata fields.
func TestWriteReport_MergeRoundTrip(t *testing.T) {
	tracer := newReportTracer(t)
	tracer.ack.Store(cookie(0x30), struct{}{})
	tracer.ack.Store(cookie(0x10), struct{}{})

	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, tracer.writeReport(path))
	report := readReport(t, path)

	merged, err := coverage.Merge([]*coverage.CoverageReport{report, report})
	require.NoError(t, err)

	require.NotEmpty(t, merged.GeneratedAt)
	merged.GeneratedAt = report.GeneratedAt
	require.Equal(t, report, merged)
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

// cppFuncs is a function map as Init would build it from a C++ binary: one
// mangled symbol with a distinct demangled form and one plain C symbol.
var cppFuncs = map[cookie]funcInfo{
	1: {name: "_ZN3app3net5parseEi", demangled: "app::net::parse(int)", offset: 1},
	2: {name: "c_entry", demangled: "c_entry", offset: 2},
}

// TestHandleEvent_VerbosePrintsDemangled checks that verbose output shows the
// demangled name while the raw name stays out of it.
func TestHandleEvent_VerbosePrintsDemangled(t *testing.T) {
	var buf bytes.Buffer
	tracee := NewUserTracee(WithTraceeExePath("dummy-path"))
	tracee.funcs = cppFuncs
	tracer := NewUserTracer(
		WithTracerVerbose(true),
		WithTracerWriter(&buf),
		WithTracerTracee(tracee),
	)

	tracer.handleEvent(encodeEvent(t, 1))
	tracer.handleEvent(encodeEvent(t, 2))

	require.Equal(t, "app::net::parse(int)\nc_entry\n", buf.String())
}

// TestWriteReport_Demangled checks that funcs_traced, funcs_ack and name keep
// the raw symbol names and that a function entry carries demangled only when
// it differs from name.
func TestWriteReport_Demangled(t *testing.T) {
	tracee := NewUserTracee(WithTraceeExePath("dummy-path"))
	tracee.funcs = cppFuncs
	tracer := NewUserTracer(WithTracerReport(true), WithTracerTracee(tracee))
	tracer.handleEvent(encodeEvent(t, 1))

	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, tracer.writeReport(path))
	report := readReport(t, path)

	require.Equal(t, []string{"_ZN3app3net5parseEi", "c_entry"}, report.FuncsTraced)
	require.Equal(t, []string{"_ZN3app3net5parseEi"}, report.FuncsAck)
	require.Equal(t, []coverage.FunctionCoverage{
		{Name: "_ZN3app3net5parseEi", Demangled: "app::net::parse(int)", Offset: 1, Hit: true},
		{Name: "c_entry", Offset: 2, Hit: false},
	}, report.Functions)
}

// TestWriteReport_NoDemangledForGo checks that a Go-only function set writes
// no demangled key at all, keeping the report shape unchanged for Go users.
func TestWriteReport_NoDemangledForGo(t *testing.T) {
	tracee := NewUserTracee(WithTraceeExePath("dummy-path"))
	tracee.funcs = map[cookie]funcInfo{1: {name: "main.foo", demangled: "main.foo", offset: 1}}
	tracer := NewUserTracer(WithTracerReport(true), WithTracerTracee(tracee))

	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, tracer.writeReport(path))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"functions"`)
	require.NotContains(t, string(raw), `"demangled"`)
}
