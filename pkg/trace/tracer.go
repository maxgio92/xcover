package trace

import (
	"bytes"
	"context"
	"encoding/binary"
	stderrors "errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/coverage"
	"github.com/maxgio92/xcover/pkg/healthcheck"
	"github.com/maxgio92/xcover/pkg/probe"
)

const (
	// bpfUprobeMultiAttachMaxOffsets is the number of offsets attached per
	// uprobe_multi link. libbpf passes the offsets and cookies arrays to the
	// kernel by pointer with a count, so bpf_attr size is not a constraint; the
	// kernel caps a single link at MAX_UPROBE_MULTI_CNT (1<<20) entries. The
	// batch stays well below that cap while keeping the per-syscall arrays
	// bounded.
	bpfUprobeMultiAttachMaxOffsets = 1 << 16
)

var (
	ReportFileName = fmt.Sprintf("%s-report.json", settings.CmdName)
	// drainQuietPeriod is how long the event consumer keeps receiving after
	// shutdown starts without any event arriving before it gives up.
	//
	// This is a heuristic, not a completion signal. An empty event channel
	// for that long does not prove the ring or an in-flight poll callback has
	// been drained: ring_buffer__poll returning on its epoll timeout
	// (probe.evtRingBufPollTimeout, 60 ms) consumes nothing, and a poller
	// descheduled past the quiet period loses its batch to RingBuffer.Stop.
	// The uprobes are detached first, so the ring is quiescent; the
	// deterministic fix is one ring_buffer__consume after the poll goroutine
	// has stopped, which the pinned libbpfgo does not expose yet. Until it
	// does, that loss is a known limitation left for a follow-up. Tests
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
	// Drops returns how many calls the BPF program could not record because
	// the seen_funcs insert failed.
	Drops() (uint64, error)
	// CheckPIDFilter reports whether the kernel applies the uprobe_multi PID
	// filter to the whole thread group: nil when it does,
	// probe.ErrPIDFilterByThread when it matches one thread only, and another
	// error when the check was inconclusive. It must run after Init.
	CheckPIDFilter() error
}

type UserTracer struct {
	// Tracer objects.
	probe Probe
	// Tracee objects.
	tracee *UserTracee
	// User functions being acknowledged.
	ack sync.Map
	// Cookies with no matching function, recorded so each is warned about once.
	unknown sync.Map
	// User functions being consumed.
	consumed uint64
	// pidFilterErr is the CheckPIDFilter outcome recorded at attach time, so
	// the undercount warning repeats next to the report.
	pidFilterErr error
	// HealthCheck server.
	hcServer *healthcheck.HealthCheckServer

	*UserTracerOptions
}

func NewUserTracer(opts ...UserTracerOpt) *UserTracer {
	tracer := &UserTracer{
		UserTracerOptions: &UserTracerOptions{pid: -1},
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

	// Initialize the tracee first: it resolves the symbols and function
	// offsets, and the probe needs the resulting function count to size the
	// seen_funcs map before loading the BPF object. The tracee does not depend
	// on the probe, so initializing it first is safe.
	if err := t.tracee.Init(ctx); err != nil {
		return errors.Wrapf(err, "failed to init tracer")
	}
	if err := t.validateTracee(); err != nil {
		return err
	}

	if t.probe == nil {
		t.probe = t.defaultProbe(len(t.tracee.funcs))
	}
	if err := t.probe.Init(ctx); err != nil {
		return errors.Wrap(err, "error initializing BPF probe")
	}

	return nil
}

// defaultProbe builds the kernel (or bpftime) BPF probe sized for funcCount
// traced functions.
func (t *UserTracer) defaultProbe(funcCount int) Probe {
	probeOpts := []probe.Option{
		probe.WithLogger(t.logger),
		probe.WithFuncCount(funcCount),
		probe.WithPID(t.pid),
	}
	if t.userspaceBPF {
		probeOpts = append(probeOpts, probe.WithUserspaceBPF())
	}
	return probe.NewProbe(probeOpts...)
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
	t.checkPIDFilter()
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

	t.warnDrops()
	t.warnPIDFilter()

	return t.writeReport(ReportFileName)
}

// warnDrops reads the BPF drop counter and warns when calls could not be
// recorded because the seen_funcs insert failed: their functions are missing
// from the report.
func (t *UserTracer) warnDrops() {
	drops, err := t.probe.Drops()
	if err != nil {
		t.logger.Warn().Err(err).Msg("failed to read the dropped calls counter")
		return
	}
	if drops > 0 {
		t.logger.Warn().Uint64("dropped", drops).
			Msg("calls not recorded because the seen_funcs map rejected the insert; the report undercounts coverage, narrow the probe set with --scope or --exclude")
	}
}

// checkPIDFilter asks the probe whether the kernel applies the --pid filter
// to the whole thread group and warns when it does not, or when the check was
// inconclusive. It runs only for an active filter in kernel mode: bpftime
// never enforces the pid and the run command refuses that combination. The
// outcome is kept so waitAndReport repeats the warning next to the report,
// like the drop counter.
func (t *UserTracer) checkPIDFilter() {
	if t.pid <= 0 || t.userspaceBPF {
		return
	}
	t.pidFilterErr = t.probe.CheckPIDFilter()
	t.warnPIDFilter()
}

// warnPIDFilter warns that the report may undercount when checkPIDFilter
// found a kernel that filters uprobe_multi by thread, or could not tell.
func (t *UserTracer) warnPIDFilter() {
	if t.pidFilterErr == nil {
		return
	}
	ev := t.logger.Warn().Str("kernel", kernelRelease()).Int("pid", t.pid)
	if errors.Is(t.pidFilterErr, probe.ErrPIDFilterByThread) {
		ev.Msg("the kernel filters uprobe_multi by thread instead of thread group (missing commit 46ba0e49b642), so only hits from the main thread of the traced process are recorded; the report may undercount, use a kernel with the fix (6.6.35, 6.9.5, 6.10 or newer) or drop --pid")
		return
	}
	ev.Err(t.pidFilterErr).Msg("could not check whether the kernel filters uprobe_multi by thread group; the report may undercount on a multithreaded target, use a kernel with commit 46ba0e49b642 (6.6.35, 6.9.5, 6.10 or newer) or drop --pid")
}

// kernelRelease returns the running kernel release from uname, or "unknown"
// when uname fails.
func kernelRelease() string {
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		return "unknown"
	}
	return unix.ByteSliceToString(u.Release[:])
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
// in the channel or surfaced by a poll callback are acked before the report
// is written. The caller detaches the uprobes before closing stop, so no new
// records are committed during the drain. A poll callback delayed past the
// quiet period can still lose its batch; see drainQuietPeriod.
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

// drainEvents handles events until drainQuietPeriod elapses without one. An
// empty channel for that long is taken as the ring being empty; it is not a
// guarantee (see drainQuietPeriod).
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
		return
	}

	if t.tracee == nil {
		return
	}

	fun, ok := t.lookupFunc(event.Cookie)
	if !ok {
		return
	}
	t.ackFunc(event.Cookie, fun)
}

// decodeEvent deserializes the raw ring buffer bytes into an Event.
func (t *UserTracer) decodeEvent(data []byte) (Event, error) {
	var event Event

	buf := bytes.NewBuffer(data)
	err := binary.Read(buf, binary.LittleEndian, &event)

	return event, err
}

// lookupFunc resolves the function traced by cookie. A miss is warned about
// once per cookie instead of failing, so a single unmatched cookie neither
// stops the consumer nor floods the log; the caller must not ack it.
func (t *UserTracer) lookupFunc(ck cookie) (funcInfo, bool) {
	fun, ok := t.tracee.funcs[ck]
	if !ok {
		if _, seen := t.unknown.LoadOrStore(ck, struct{}{}); !seen {
			t.logger.Warn().Err(ErrFuncNotFoundForCookie).Uint64("cookie", uint64(ck)).Msg("failed getting function from cookie")
		}
	}

	return fun, ok
}

// ackFunc records the first observation of fun and prints its demangled name
// when verbose output is enabled. Subsequent events for the same cookie are
// no-ops.
func (t *UserTracer) ackFunc(ck cookie, fun funcInfo) {
	if _, ok := t.ack.Load(ck); !ok {
		if t.verbose && t.writer != nil {
			fmt.Fprintln(t.writer, fun.demangled)
		}
		t.ack.Store(ck, struct{}{})
	}
}

func (t *UserTracer) writeReport(reportPath string) error {
	if !t.report {
		return nil
	}

	report := t.buildReport()

	file, err := os.Create(reportPath)
	if err != nil {
		return errors.Wrap(err, "failed to create report file")
	}

	// Close before logging success: a failed close means the file on disk
	// may not hold what was written.
	werr := report.WriteReport(file)
	if cerr := file.Close(); cerr != nil {
		werr = stderrors.Join(werr, errors.Wrap(cerr, "failed to close report file"))
	}
	if werr != nil {
		return errors.Wrap(werr, "failed to write report")
	}

	t.logger.Info().Str("path", reportPath).Msg("report generated")

	return nil
}

// buildReport assembles the coverage report from the traced function set and
// the acked cookies. Lists are sorted so that identical sessions produce
// byte-identical reports apart from generated_at. Acked cookies that no
// longer resolve to a function are skipped and do not count as coverage.
func (t *UserTracer) buildReport() *coverage.CoverageReport {
	traced := make([]string, 0, len(t.tracee.funcs))
	functions := make([]coverage.FunctionCoverage, 0, len(t.tracee.funcs))
	for ck, fn := range t.tracee.funcs {
		_, hit := t.ack.Load(ck)
		traced = append(traced, fn.name)
		functions = append(functions, coverage.FunctionCoverage{Name: fn.name, Offset: fn.offset, Hit: hit})
	}
	sort.Strings(traced)
	// Offsets are unique: funcs is keyed by offset, so no tie-break is needed.
	sort.Slice(functions, func(i, j int) bool { return functions[i].Offset < functions[j].Offset })

	ack := make([]string, 0, len(functions))
	t.ack.Range(func(k, v interface{}) bool {
		if fun, ok := t.tracee.funcs[k.(cookie)]; ok {
			ack = append(ack, fun.name)
		}
		return true
	})
	sort.Strings(ack)

	covByFunc := float64(len(ack)) / float64(len(t.tracee.funcs)) * 100

	return coverage.NewCoverageReport(
		coverage.WithReportFuncsAck(ack),
		coverage.WithReportFuncsTraced(traced),
		coverage.WithReportFuncsCov(covByFunc),
		coverage.WithReportExePath(t.tracee.exePath),
		coverage.WithReportPID(t.pid),
		coverage.WithReportBuildID(t.tracee.exeBuildID()),
		coverage.WithReportKernel(kernelRelease()),
		coverage.WithReportXcoverVersion(settings.Version),
		coverage.WithReportGeneratedAt(time.Now().UTC().Format(time.RFC3339)),
		coverage.WithReportFunctions(functions),
	)
}
