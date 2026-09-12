package trace

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/internal/utils"
)

// fakeProbe satisfies Probe without a BPF-capable kernel. InitEventBuf hands
// out events so tests can feed the pipeline directly.
type fakeProbe struct {
	initErr   error
	attachErr error
	events    chan []byte

	attachCalls int
	modClosed   bool
}

func (p *fakeProbe) Init(context.Context) error { return p.initErr }

func (p *fakeProbe) Attach(context.Context, string, []uint64, []uint64) error {
	p.attachCalls++
	return p.attachErr
}

func (p *fakeProbe) InitEventBuf(context.Context) (chan []byte, error) { return p.events, nil }
func (p *fakeProbe) PollEventBuf()                                     {}
func (p *fakeProbe) CloseEventBuf()                                    {}
func (p *fakeProbe) CloseBPFMod()                                      { p.modClosed = true }

var lifecycleEntries = []FunctionEntry{
	{Name: "pkg.Alpha", Offset: 0x1000},
	{Name: "pkg.Beta", Offset: 0x2000},
	{Name: "pkg.Gamma", Offset: 0x3000},
}

// newLifecycleTracer builds a tracer over p with a static tracee and points
// the health check socket at a per-test path so tests never touch the real
// /tmp/xcover.sock. It returns the socket path.
func newLifecycleTracer(t *testing.T, p Probe) (*UserTracer, string) {
	t.Helper()

	orig := HealthCheckSockPath
	HealthCheckSockPath = filepath.Join(t.TempDir(), "hc.sock")
	t.Cleanup(func() { HealthCheckSockPath = orig })

	tracee := NewUserTracee(
		WithTraceeExePath("dummy-path"),
		WithTraceeResolver(func(context.Context) ([]FunctionEntry, error) {
			return lifecycleEntries, nil
		}),
	)
	tracer := NewUserTracer(
		WithTracerLogger(zerolog.Nop()),
		WithTracerTracee(tracee),
		WithTracerProbe(p),
		WithTracerWriter(new(bytes.Buffer)),
	)

	return tracer, HealthCheckSockPath
}

func encodeEvent(t *testing.T, ck cookie) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	require.NoError(t, binary.Write(buf, binary.LittleEndian, Event{Cookie: ck}))
	return buf.Bytes()
}

// TestRun_AttachFailure asserts that a failed attach fails Run before
// readiness is signalled, so a `wait` client never reads ReadyMsg, and that
// the BPF module is still torn down and the socket removed.
func TestRun_AttachFailure(t *testing.T) {
	attachErr := errors.New("uprobe_multi rejected")
	p := &fakeProbe{attachErr: attachErr}
	tracer, sockPath := newLifecycleTracer(t, p)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, tracer.Init(ctx))

	// Connect like `xcover wait` does, before Run, so the pending connection
	// observes whether readiness is ever signalled.
	conn, err := net.Dial("unix", sockPath)
	require.NoError(t, err)
	defer conn.Close()

	err = tracer.Run(ctx)
	require.ErrorIs(t, err, attachErr)
	require.Equal(t, 1, p.attachCalls)
	require.True(t, p.modClosed, "CloseBPFMod must run after a failed attach")

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	buf := make([]byte, 1)
	n, err := conn.Read(buf)
	require.Error(t, err, "readiness must not be signalled after a failed attach")
	require.Zero(t, n)

	_, err = os.Stat(sockPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestRun_DrainsBufferedEventsOnCancel asserts that every event already
// buffered when the context is cancelled is acked before Run returns.
func TestRun_DrainsBufferedEventsOnCancel(t *testing.T) {
	const n = 300

	p := &fakeProbe{events: make(chan []byte, n)}
	tracer, _ := newLifecycleTracer(t, p)

	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, tracer.Init(ctx))

	for i := 0; i < n; i++ {
		p.events <- encodeEvent(t, cookie(lifecycleEntries[i%len(lifecycleEntries)].Offset))
	}
	// Cancel before Run consumes anything so the whole batch goes through the
	// drain path rather than the steady-state loop.
	cancel()

	done := make(chan error, 1)
	go func() { done <- tracer.Run(ctx) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}

	require.Equal(t, uint64(n), tracer.consumed)
	require.Equal(t, len(lifecycleEntries), utils.LenSyncMap(&tracer.ack))
	require.Empty(t, p.events)
	require.True(t, p.modClosed)
}
