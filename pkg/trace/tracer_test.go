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
// present in tracee.funcs is still acked, matching the pre-refactor behavior
// of counting unresolved cookies toward the reported coverage.
func TestHandleEvent_UnknownCookie(t *testing.T) {
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

	// Encode an Event with a cookie that has no matching entry in tracee.funcs.
	event := Event{Cookie: 2}
	data := new(bytes.Buffer)
	err = binary.Write(data, binary.LittleEndian, event)
	require.NoError(t, err)

	tracer.handleEvent(data.Bytes())

	_, ok := tracer.ack.Load(cookie(2))
	require.True(t, ok)
}

// cppFuncs is a function map as Init would build it from a C++ binary: one
// mangled symbol with a distinct demangled form and one plain C symbol.
var cppFuncs = map[cookie]funcInfo{
	1: {name: "_ZN3app3net5parseEi", demangled: "app::net::parse(int)", offset: 1},
	2: {name: "c_entry", demangled: "c_entry", offset: 2},
}

func encodeEvent(t *testing.T, ck cookie) []byte {
	t.Helper()
	data := new(bytes.Buffer)
	require.NoError(t, binary.Write(data, binary.LittleEndian, Event{Cookie: ck}))
	return data.Bytes()
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

// TestWriteReport_Symbols checks that the report keeps raw names in
// funcs_traced and funcs_ack and lists only the names that demangle
// differently under symbols.
func TestWriteReport_Symbols(t *testing.T) {
	tracee := NewUserTracee(WithTraceeExePath("dummy-path"))
	tracee.funcs = cppFuncs
	tracer := NewUserTracer(WithTracerReport(true), WithTracerTracee(tracee))
	tracer.handleEvent(encodeEvent(t, 1))

	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, tracer.writeReport(path))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var report coverage.CoverageReport
	require.NoError(t, json.Unmarshal(raw, &report))

	require.ElementsMatch(t, []string{"_ZN3app3net5parseEi", "c_entry"}, report.FuncsTraced)
	require.Equal(t, []string{"_ZN3app3net5parseEi"}, report.FuncsAck)
	require.Equal(t, map[string]string{"_ZN3app3net5parseEi": "app::net::parse(int)"}, report.Symbols)
}

// TestWriteReport_NoSymbolsForGo checks that a Go-only function set produces
// no symbols key at all, keeping the report schema unchanged for Go users.
func TestWriteReport_NoSymbolsForGo(t *testing.T) {
	tracee := NewUserTracee(WithTraceeExePath("dummy-path"))
	tracee.funcs = map[cookie]funcInfo{1: {name: "main.foo", demangled: "main.foo", offset: 1}}
	tracer := NewUserTracer(WithTracerReport(true), WithTracerTracee(tracee))

	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, tracer.writeReport(path))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	require.NotContains(t, fields, "symbols")
	require.Contains(t, fields, "funcs_traced")
}
