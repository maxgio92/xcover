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
// out events so tests can feed the pipeline directly. onDetach, when set,
// runs inside DetachLinks so a test can emulate records that land after the
// uprobes are detached.
type fakeProbe struct {
	initErr   error
	attachErr error
	events    chan []byte
	onDetach  func()
	drops     uint64

	attachCalls int
	// detachCalls counts DetachLinks calls made directly, not via CloseBPFMod.
	detachCalls int
	// reportAtDetach records whether the report file already existed when
	// DetachLinks was first called.
	reportAtDetach bool
	modClosed      bool
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
func (p *fakeProbe) Drops() (uint64, error)                            { return p.drops, nil }

func (p *fakeProbe) DetachLinks() {
	if p.detachCalls == 0 {
		_, err := os.Stat(ReportFileName)
		p.reportAtDetach = err == nil
	}
	p.detachCalls++
	if p.onDetach != nil {
		p.onDetach()
	}
}

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

// TestRun_DrainsBufferedEventsOnCancel asserts that on cancellation the
// uprobes are detached before the report is written, and that both the
// events already buffered and those that land shortly after the detach are
// acked before Run returns.
func TestRun_DrainsBufferedEventsOnCancel(t *testing.T) {
	const (
		buffered = 300
		late     = 3
		// Late events are delivered lateDelay after the detach; the quiet
		// period is an order of magnitude longer so a slow scheduler cannot
		// end the drain first.
		lateDelay = 50 * time.Millisecond
	)

	origQuiet := drainQuietPeriod
	drainQuietPeriod = 10 * lateDelay
	t.Cleanup(func() { drainQuietPeriod = origQuiet })

	origReport := ReportFileName
	ReportFileName = filepath.Join(t.TempDir(), "report.json")
	t.Cleanup(func() { ReportFileName = origReport })

	// Encoded up front: the goroutine below may outlive the test if the drain
	// ended early, and require must not be called from it after that.
	lateEvents := make([][]byte, 0, late)
	for i := 0; i < late; i++ {
		lateEvents = append(lateEvents, encodeEvent(t, cookie(lifecycleEntries[i%len(lifecycleEntries)].Offset)))
	}

	p := &fakeProbe{events: make(chan []byte, buffered+late)}
	// Emulate records still in the kernel ring at detach time: they surface
	// through the poll callback only after the links are gone.
	p.onDetach = func() {
		go func() {
			time.Sleep(lateDelay)
			for _, ev := range lateEvents {
				p.events <- ev
			}
		}()
	}
	tracer, _ := newLifecycleTracer(t, p)
	tracer.report = true

	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, tracer.Init(ctx))

	// Only the last entry is hit before cancellation so the late events,
	// which cycle through every entry, are what completes the ack set.
	for i := 0; i < buffered; i++ {
		p.events <- encodeEvent(t, cookie(lifecycleEntries[len(lifecycleEntries)-1].Offset))
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

	require.Equal(t, uint64(buffered+late), tracer.consumed)
	require.Equal(t, len(lifecycleEntries), utils.LenSyncMap(&tracer.ack))
	require.Empty(t, p.events)
	require.Equal(t, 1, p.detachCalls, "links must be detached once cancellation is observed")
	require.False(t, p.reportAtDetach, "links must be detached before the report is written")
	require.FileExists(t, ReportFileName)
	require.True(t, p.modClosed)
}

// TestInit_ProbeFailureShutsDownListener asserts that a probe Init failure
// removes the health check socket instead of leaving it stale.
func TestInit_ProbeFailureShutsDownListener(t *testing.T) {
	initErr := errors.New("bpf load failed")
	tracer, sockPath := newLifecycleTracer(t, &fakeProbe{initErr: initErr})

	err := tracer.Init(t.Context())
	require.ErrorIs(t, err, initErr)

	_, err = os.Stat(sockPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestDrainEvents_ObservesChannelEmptyBeforeReturn asserts that drainEvents
// never returns while events are still buffered, even when the quiet timer
// has already fired: with a zero quiet period both select cases are ready
// on the first iteration, so any early exit on the timer branch shows up as
// a missed event.
func TestDrainEvents_ObservesChannelEmptyBeforeReturn(t *testing.T) {
	const buffered = 300

	origQuiet := drainQuietPeriod
	drainQuietPeriod = 0
	t.Cleanup(func() { drainQuietPeriod = origQuiet })

	events := make(chan []byte, buffered)
	for i := 0; i < buffered; i++ {
		events <- encodeEvent(t, cookie(lifecycleEntries[i%len(lifecycleEntries)].Offset))
	}

	tracer, _ := newLifecycleTracer(t, &fakeProbe{events: events})
	tracer.drainEvents(events)

	require.Equal(t, uint64(buffered), tracer.consumed)
	require.Empty(t, events)
}
