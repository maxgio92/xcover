package trace

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
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
