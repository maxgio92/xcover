package trace

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/internal/utils"
	"github.com/maxgio92/xcover/pkg/coverage"
	"github.com/maxgio92/xcover/pkg/healthcheck"
	"github.com/maxgio92/xcover/pkg/probe"
)

const (
	bpfMaxBufferSize               = 1024                 // Maximum size of bpf_attr needed to batch offsets for uprobe_multi attachments.
	bpfUprobeMultiAttachMaxOffsets = bpfMaxBufferSize / 8 // 8 is the byte size of uint64 used to represent offsets.
)

var (
	ReportFileName = fmt.Sprintf("%s-report.json", settings.CmdName)
	// drainQuietPeriod is how long the event consumer keeps receiving after
	// shutdown starts without any event arriving before it gives up. It
	// covers records still in the kernel ring or an in-flight libbpf poll
	// callback (poll timeout is probe.evtRingBufPollTimeout, 60 ms), since
	// libbpfgo exposes no way to consume the ring synchronously. Tests
	// shorten it.
	drainQuietPeriod = 150 * time.Millisecond
	// HealthCheckSockPath is kept as an alias of settings.HealthCheckSockPath
	// for existing callers; internal/settings is the source of truth so
	// cgo-free consumers (e.g. e2e tests) can reference it without pulling
	// in this package's libbpf dependency.
	HealthCheckSockPath = settings.HealthCheckSockPath
)

type Event struct {
	Cookie cookie
}

// Probe is the set of BPF probe operations UserTracer drives. It is
// satisfied by *probe.Probe and exists so tests can inject a fake through
// WithTracerProbe without a BPF-capable kernel.
type Probe interface {
	Init(ctx context.Context) error
	Attach(ctx context.Context, exePath string, offsets, cookies []uint64) error
	InitEventBuf(ctx context.Context) (chan []byte, error)
	PollEventBuf()
	CloseEventBuf()
	DetachLinks()
	CloseBPFMod()
}

type UserTracer struct {
	// Tracer objects.
	probe Probe
	// Tracee objects.
	tracee *UserTracee
	// User functions being acknowledged.
	ack sync.Map
	// User functions being consumed.
	consumed uint64
	// HealthCheck server.
	hcServer *healthcheck.HealthCheckServer

	*UserTracerOptions
}

func NewUserTracer(opts ...UserTracerOpt) *UserTracer {
	tracer := &UserTracer{
		UserTracerOptions: &UserTracerOptions{},
	}
	for _, opt := range opts {
		opt(tracer)
	}

	return tracer
}

func (t *UserTracer) validateTracee() error {
	if t.tracee == nil {
		return ErrTraceeNil
	}
	if t.tracee.exePath == "" {
		return ErrTraceeExePathEmpty
	}
	if len(t.tracee.funcs) == 0 {
		return ErrTraceeFuncListEmpty
	}

	return nil
}

func (t *UserTracer) Init(ctx context.Context) (err error) {
	if t.writer == nil {
		t.writer = os.Stdout
	}

	t.logger = t.logger.With().Str("component", "tracer").Logger()

	t.logger.Info().Msg("initializing tracer")

	// Start the listener before initializing the BPF module
	// and the tracee, because we want to notify the tracer
	// is alive as soon as possible.
	t.hcServer = healthcheck.NewHealthCheckServer(HealthCheckSockPath, t.logger)
	if err := t.hcServer.InitializeListener(ctx); err != nil {
		return err
	}
	// Run only shuts the listener down once it starts, so every later Init
	// failure must do it here or the socket file outlives the process.
	defer func() {
		if err == nil {
			return
		}
		if serr := t.hcServer.ShutdownListener(); serr != nil {
			t.logger.Warn().Err(serr).Msg("failed to stop listener")
		}
	}()

	if t.probe == nil {
		probeOpts := []probe.Option{probe.WithLogger(t.logger)}
		if t.userspaceBPF {
			probeOpts = append(probeOpts, probe.WithUserspaceBPF())
		}
		t.probe = probe.NewProbe(probeOpts...)
	}
	if err := t.probe.Init(ctx); err != nil {
		return errors.Wrap(err, "error initializing BPF probe")
	}

	// Initialize the tracee includes to load all the data about
	// the tracee, like symbols and function offsets.
	if err := t.tracee.Init(ctx); err != nil {
		return errors.Wrapf(err, "failed to init tracer")
	}
	if err := t.validateTracee(); err != nil {
		return err
	}

	return nil
}

func (t *UserTracer) Run(ctx context.Context) error {
	// Stop the listener and remove the socket on every exit path, including
	// early error returns: otherwise the socket file outlives the process.
	defer func() {
		if err := t.hcServer.ShutdownListener(); err != nil {
			t.logger.Warn().Err(err).Msg("failed to stop listener")
		}
	}()

	// Defers run LIFO: CloseBPFMod must be registered before CloseEventBuf so
	// it runs second, after CloseEventBuf stops the ring buffer poll goroutine.
	// Registering it before attach also detaches the links created by the
	// batches that succeeded when a later batch or InitEventBuf fails.
	defer t.probe.CloseBPFMod()

	// Attach one uprobe per function to trace. Fail before signalling
	// readiness so `wait` never reports ready for a tracer with no probes.
	t.logger.Debug().Msg("attaching trace to selected functions")
	if err := t.attachProbe(ctx); err != nil {
		return err
	}

	eventsCh, err := t.probe.InitEventBuf(ctx)
	if err != nil {
		return errors.Wrap(err, "error initializing probe events buffer")
	}
	defer t.probe.CloseEventBuf()

	stop := make(chan struct{})
	wg := t.startPipeline(eventsCh, stop)

	// Signal via the UDS that the tracer is ready,
	// that is, it's consuming function events.
	t.logger.Info().Msg("tracing functions")
	t.hcServer.NotifyReadiness()

	// Print status bar.
	go t.printStatusBar(ctx, eventsCh)

	return t.waitAndReport(ctx, stop, wg)
}

// startPipeline starts polling the ring buffer and spawns the goroutine that
// consumes events from it until stop is closed, tracked by the returned
// WaitGroup so the caller can wait for it to drain on shutdown.
func (t *UserTracer) startPipeline(eventsCh <-chan []byte, stop <-chan struct{}) *sync.WaitGroup {
	// Because it is blocking, run ring_buffer__poll() in a non-locked goroutine,
	// hence outside of InitEventBuf(), because of CGO callback from C which can make
	// the go runtime to lock goroutine to the thread.
	go t.probe.PollEventBuf()

	t.logger.Debug().Msg("consuming events from ring buffer")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t.processEvents(eventsCh, stop)
	}()

	return &wg
}

// waitAndReport blocks until ctx is cancelled, detaches the uprobes so no new
// events are produced, closes stop so the consumer drains what is still in
// flight, waits for it, then writes the coverage report. The health check
// listener is stopped by Run's deferred ShutdownListener call.
func (t *UserTracer) waitAndReport(ctx context.Context, stop chan<- struct{}, wg *sync.WaitGroup) error {
	// Waiting for signals.
	<-ctx.Done()
	t.logger.Debug().Msg("received signal")

	// Detach before draining: the drain is bounded by a quiet period, so
	// events must stop being produced for it to terminate and to be complete.
	t.probe.DetachLinks()
	close(stop)

	// Waiting for the consumer to drain.
	wg.Wait()
	t.logger.Info().Msg("terminating...")

	return t.writeReport(ReportFileName)
}

// attachProbe attaches the probe to every tracee function in uprobe_multi
// sized batches and returns the first batch failure; links created by earlier
// batches are left for CloseBPFMod to destroy.
func (t *UserTracer) attachProbe(ctx context.Context) error {
	batchSize := bpfUprobeMultiAttachMaxOffsets

	offsets, cookies := t.tracee.GetFuncProbes()

	for i := 0; i < len(offsets); i += batchSize {
		end := i + batchSize
		if end > len(offsets) {
			end = len(offsets)
		}

		if err := t.probe.Attach(ctx, t.tracee.exePath, offsets[i:end], cookies[i:end]); err != nil {
			return errors.Wrap(err, "error attaching probe")
		}
	}

	return nil
}

// processEvents handles events until stop is closed, then keeps receiving
// until no event has arrived for drainQuietPeriod, so events still buffered
// in the channel, in the kernel ring, or in an in-flight poll callback are
// acked before the report is written. The caller detaches the uprobes before
// closing stop, so the quiet period is reached once the ring is empty.
func (t *UserTracer) processEvents(events <-chan []byte, stop <-chan struct{}) {
	for {
		select {
		case data := <-events:
			t.handleEvent(data)
		case <-stop:
			t.drainEvents(events)
			return
		}
	}
}

// drainEvents handles events until drainQuietPeriod elapses without one.
func (t *UserTracer) drainEvents(events <-chan []byte) {
	quiet := time.NewTimer(drainQuietPeriod)
	defer quiet.Stop()

	for {
		select {
		case data := <-events:
			t.handleEvent(data)
			// Go 1.23+ timer channels are synchronous, so Reset alone is
			// enough: no stale tick can be delivered after it.
			quiet.Reset(drainQuietPeriod)
		case <-quiet.C:
			// When the timer and the channel are both ready select picks at
			// random, so returning here could leave events queued for
			// RingBuffer.Stop to discard. Only return once the channel has
			// been observed empty; an event found here renews the quiet
			// period so a producer that wakes up late keeps its batch.
			select {
			case data := <-events:
				t.handleEvent(data)
				quiet.Reset(drainQuietPeriod)
			default:
				return
			}
		}
	}
}

// TODO: decouple handle from handler functions as argument.
func (t *UserTracer) handleEvent(data []byte) {
	atomic.AddUint64(&t.consumed, 1)

	event, err := t.decodeEvent(data)
	if err != nil {
		t.logger.Err(err).Msg("failed to read event")
	}

	if t.tracee == nil {
		return
	}

	fun, _ := t.lookupFunc(event.Cookie)
	t.ackFunc(event.Cookie, fun)
}

// decodeEvent deserializes the raw ring buffer bytes into an Event.
func (t *UserTracer) decodeEvent(data []byte) (Event, error) {
	var event Event

	buf := bytes.NewBuffer(data)
	err := binary.Read(buf, binary.LittleEndian, &event)

	return event, err
}

// lookupFunc resolves the function traced by cookie, logging a miss instead
// of failing so a single unmatched cookie doesn't stop the consumer. The
// caller acks the cookie regardless of the lookup result.
func (t *UserTracer) lookupFunc(ck cookie) (funcInfo, bool) {
	fun, ok := t.tracee.funcs[ck]
	if !ok {
		t.logger.Err(ErrFuncNotFoundForCookie).Uint64("cookie", uint64(ck)).Msg("failed getting function from cookie")
	}

	return fun, ok
}

// ackFunc records the first observation of fun and prints its name when
// verbose output is enabled. Subsequent events for the same cookie are
// no-ops.
func (t *UserTracer) ackFunc(ck cookie, fun funcInfo) {
	if _, ok := t.ack.Load(ck); !ok {
		if t.verbose && t.writer != nil {
			fmt.Fprintln(t.writer, fun.name)
		}
		t.ack.Store(ck, struct{}{})
	}
}

func (t *UserTracer) writeReport(reportPath string) error {
	if !t.report {
		return nil
	}

	traced := make([]string, 0, len(t.tracee.funcs))
	for _, fn := range t.tracee.funcs {
		traced = append(traced, fn.name)
	}

	ack := make([]string, 0, utils.LenSyncMap(&t.ack))
	t.ack.Range(func(k, v interface{}) bool {
		fun, ok := t.tracee.funcs[k.(cookie)]
		if !ok {
			return false
		}
		ack = append(ack, fun.name)
		return true
	})

	covByFunc := float64(utils.LenSyncMap(&t.ack)) / float64(len(t.tracee.funcs)) * 100

	report := coverage.NewCoverageReport(
		coverage.WithReportFuncsAck(ack),
		coverage.WithReportFuncsTraced(traced),
		coverage.WithReportFuncsCov(covByFunc),
		coverage.WithReportExePath(t.tracee.exePath),
	)

	file, err := os.Create(reportPath)
	if err != nil {
		t.logger.Err(err).Msg("failed to create report file")
	}
	defer file.Close()

	t.logger.Info().Str("path", reportPath).Msgf("report generated")

	return report.WriteReport(file)
}
