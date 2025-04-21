package trace

import (
	"bytes"
	"encoding/binary"
	"github.com/stretchr/testify/require"
	"testing"
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
	err := tracee.Init()
	require.NoError(t, err)

	tracer := NewUserTracer(
		WithTracerBpfModPath("testdata/test.bpf.o"),
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
