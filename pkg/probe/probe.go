package probe

import (
	"context"
	"embed"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	bpf "github.com/aquasecurity/libbpfgo"
	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
	"golang.org/x/sys/unix"
)

//go:embed output/*
var probeFS embed.FS

const (
	outputPath            = "output"
	ProbePath             = "trace.bpf.o"
	ProgName              = "handle_user_function"
	EventsChBufSize       = 4096
	evtRingBufBPFMapName  = "events"
	evtRingBufPollTimeout = 60
	seenFuncsBPFMapName   = "seen_funcs"
	dropsBPFMapName       = "drops"

	// maxRingBufSize is the largest events ring buffer the kernel accepts:
	// max_entries is a __u32 that must be a power of two.
	maxRingBufSize uint64 = 1 << 31
)

type Probe struct {
	Name string
	data []byte

	bpfMod  *bpf.Module
	bpfProg *bpf.BPFProg
	links   []*bpf.BPFLink

	EvtBuf *bpf.RingBuffer

	userspaceBPF bool
	funcCount    int

	// ringBufSize is the events ring buffer capacity in bytes; 0 keeps the
	// max_entries compiled into the BPF object.
	ringBufSize uint32

	// pid restricts uprobe attachment to one process (thread group); -1
	// traces every process executing the target binary.
	pid int

	logger log.Logger
}

type Option func(p *Probe)

func WithLogger(logger log.Logger) Option {
	return func(p *Probe) {
		p.logger = logger
	}
}

// WithUserspaceBPF configures the probe to use the classic single-uprobe
// perf_event_open path instead of uprobe_multi. bpftime supports the former
// but silently no-ops the latter, so this must be set when running under
// bpftime.
func WithUserspaceBPF() Option {
	return func(p *Probe) {
		p.userspaceBPF = true
	}
}

// WithFuncCount sets the number of functions the probe will trace, used to
// size the seen_funcs map before the BPF object is loaded. With 0 (the
// default) the max_entries compiled into the BPF object is kept.
func WithFuncCount(n int) Option {
	return func(p *Probe) {
		p.funcCount = n
	}
}

// FuncCount returns the number of functions the probe was configured to
// trace with WithFuncCount (0 when unset).
func (p *Probe) FuncCount() int {
	return p.funcCount
}

// resizeSeenFuncs sets the seen_funcs map capacity to funcCount before the
// object is loaded; max_entries is immutable afterwards. A preallocated BPF
// hash holds exactly max_entries distinct keys and cookies are bounded by the
// traced function count, so no headroom is needed. With funcCount <= 0 the
// max_entries compiled into the object is kept. A binary built with the
// e2etest tag may cap the size further through seenFuncsCapOverride so the
// e2e suite can provoke the drops warning.
func resizeSeenFuncs(seenFuncs *bpf.BPFMap, funcCount int) error {
	if funcCount <= 0 {
		return nil
	}
	if capOverride := seenFuncsCapOverride(); capOverride > 0 && capOverride < funcCount {
		funcCount = capOverride
	}
	if err := seenFuncs.SetMaxEntries(uint32(funcCount)); err != nil {
		return errors.Wrapf(err, "failed to resize bpf map %s", seenFuncsBPFMapName)
	}
	return nil
}

// WithRingBufSize sets the events ring buffer capacity in bytes, applied
// before the BPF object is loaded. With 0 (the default) the max_entries
// compiled into the BPF object is kept. Callers validate n with
// ValidateRingBufSize first: libbpf silently rounds any other value up to
// the next power-of-two multiple of the page size.
func WithRingBufSize(n uint32) Option {
	return func(p *Probe) {
		p.ringBufSize = n
	}
}

// RingBufSize returns the events ring buffer size the probe was configured
// with through WithRingBufSize (0 when unset).
func (p *Probe) RingBufSize() uint32 {
	return p.ringBufSize
}

// ValidateRingBufSize reports whether n is an events ring buffer size the
// kernel accepts as is: a power of two, a multiple of the page size, greater
// than zero and at most maxRingBufSize. The kernel rejects any other size
// with EINVAL and libbpf would round a smaller one up silently, so the rule
// is enforced here, where the caller can still name it.
func ValidateRingBufSize(n uint64) error {
	pageSize := uint64(os.Getpagesize())
	if n == 0 || n&(n-1) != 0 || n%pageSize != 0 || n > maxRingBufSize {
		return fmt.Errorf("invalid ring buffer size %d: must be a power of two, a multiple of the %d byte page size, greater than zero and at most %d", n, pageSize, maxRingBufSize)
	}
	return nil
}

// resizeEventsRingBuf sets the events ring buffer capacity in bytes before
// the object is loaded; max_entries is immutable afterwards. With size 0 the
// max_entries compiled into the object is kept.
func resizeEventsRingBuf(events *bpf.BPFMap, size uint32) error {
	if size == 0 {
		return nil
	}
	if err := events.SetMaxEntries(size); err != nil {
		return errors.Wrapf(err, "failed to resize bpf map %s", evtRingBufBPFMapName)
	}
	return nil
}

// loadError describes a failed BPF object load. The kernel allocates every
// page of the events ring buffer at load, so the message names its size and,
// when the cause is ENOMEM, suggests lowering it.
func loadError(name string, size uint32, err error) error {
	hint := ""
	if errors.Is(err, unix.ENOMEM) {
		hint = "; lower --ringbuf-size"
	}
	return fmt.Errorf("failed to load bpf module %s with a %d byte events ring buffer: %w%s", name, size, err, hint)
}

// WithPID restricts the uprobes to the given process. The default of -1
// traces every process that executes the target binary.
func WithPID(pid int) Option {
	return func(p *Probe) {
		p.pid = pid
	}
}

func NewProbe(opts ...Option) *Probe {
	p := &Probe{pid: -1}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *Probe) read(path string) ([]byte, error) {
	data, err := probeFS.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (p *Probe) Data() []byte {
	return p.data
}

func (p *Probe) Init(_ context.Context) error {
	p.Name = ProgName
	p.configureBPFLogger()

	var err error
	p.data, err = p.read(filepath.Join(outputPath, ProbePath))
	if err != nil {
		return errors.Wrap(err, "error reading bpf program file")
	}

	p.bpfMod, err = bpf.NewModuleFromBufferArgs(bpf.NewModuleArgs{
		BPFObjBuff:      p.Data(),
		BPFObjName:      p.Name,
		SkipMemlockBump: true,
	})
	if err != nil {
		return errors.Wrapf(err, "failed to load bpf module: %s", p.Name)
	}

	p.bpfProg, err = p.bpfMod.GetProgram(p.Name)
	if err != nil {
		return errors.Wrapf(err, "failed to get bpf program: %s", p.Name)
	}

	// uprobe_multi requires an explicit expected attach type so the kernel knows
	// to use BPF_LINK_TYPE_UPROBE_MULTI. For the classic single-uprobe path
	// (bpftime mode) we leave expected_attach_type at its default (0).
	if !p.userspaceBPF {
		if err := p.bpfProg.SetExpectedAttachType(bpf.BPFAttachTypeTraceUprobeMulti); err != nil {
			return errors.Wrapf(err, "failed to set expected attach type %s", bpf.BPFAttachTypeTraceUprobeMulti)
		}
	}

	// The seen_funcs hash must hold one entry per traced function, otherwise
	// functions beyond its capacity lose in-kernel dedup.
	seenFuncs, err := p.bpfMod.GetMap(seenFuncsBPFMapName)
	if err != nil {
		return errors.Wrapf(err, "failed to get bpf map %s", seenFuncsBPFMapName)
	}
	if err := resizeSeenFuncs(seenFuncs, p.funcCount); err != nil {
		return err
	}

	events, err := p.bpfMod.GetMap(evtRingBufBPFMapName)
	if err != nil {
		return errors.Wrapf(err, "failed to get bpf map %s", evtRingBufBPFMapName)
	}
	if err := resizeEventsRingBuf(events, p.ringBufSize); err != nil {
		return err
	}
	// libbpf may round the size, so read back what the kernel is asked to
	// allocate at load.
	effective := events.MaxEntries()
	p.logger.Info().Uint32("bytes", effective).Msg("events ring buffer size")

	if err := p.bpfMod.BPFLoadObject(); err != nil {
		return loadError(p.Name, effective, err)
	}

	return nil
}

// noisyAttachFailureSubstrings lists libbpf warning-level log fragments that
// are emitted for uprobe/uprobe_multi attach failures we already surface as
// wrapped Go errors (see Attach and attachSingleUprobes). The multi-uprobe
// path logs one "failed to attach multi-uprobe" warning per bpf_link_create()
// call for the whole batch, not per offset. The legacy uprobe path is taken
// whenever the kernel lacks the modern perf uprobe PMU, and can also fire on
// tracefs permission or mount issues unrelated to any specific offset. In
// both cases the warning carries no information beyond what the caller-level
// error already conveys, so it is downgraded to Debug. Any other warning
// (e.g. malformed BPF object, missing kernel features) is left at Warn since
// it is not otherwise surfaced and may be actionable.
//
// These substrings are coupled to libbpf's English log wording and may drift
// across libbpf version bumps; re-check them against libbpf.c when updating
// the vendored libbpf/libbpfgo version.
var noisyAttachFailureSubstrings = []string{
	"failed to attach multi-uprobe",              // bpf_link_create() for BPF_TRACE_UPROBE_MULTI
	"failed to add legacy uprobe event",          // legacy uprobe: uprobe_events write
	"failed to determine legacy uprobe event id", // legacy uprobe: id lookup after registration
	"legacy uprobe perf_event_open() failed",     // legacy uprobe: perf_event_open()
}

// isNoisyAttachFailure reports whether msg matches a known, already-reported
// uprobe attach failure that should be downgraded to Debug rather than
// surfaced at Warn.
func isNoisyAttachFailure(msg string) bool {
	for _, substr := range noisyAttachFailureSubstrings {
		if strings.Contains(msg, substr) {
			return true
		}
	}
	return false
}

func (p *Probe) configureBPFLogger() {
	bpf.SetLoggerCbs(bpf.Callbacks{
		Log: func(level int, msg string) {
			if level != bpf.LibbpfWarnLevel {
				return
			}
			if isNoisyAttachFailure(msg) {
				p.logger.Debug().Msgf("libbpf warning: %s", msg)
				return
			}
			p.logger.Warn().Msgf("libbpf warning: %s", msg)
		},
	})
}

func (p *Probe) Attach(_ context.Context, exePath string, offsets, cookies []uint64) error {
	if p.userspaceBPF {
		return p.attachSingleUprobes(exePath, offsets, cookies)
	}

	// Kernels without commit 46ba0e49b642 (before 6.6.35 and 6.9.5) filter
	// pid by thread instead of thread group. The explicit attach used here
	// does not consult libbpf's feature probe for that bug; CheckPIDFilter
	// replicates it so the tracer can warn before attaching.
	link, err := p.bpfProg.AttachUprobeMulti(p.pid, exePath, offsets, cookies)
	if err != nil {
		if len(cookies) > 0 {
			return errors.Wrapf(err, "error attaching uprobe_multi link for %d functions (first cookie 0x%x)", len(cookies), cookies[0])
		}
		return errors.Wrapf(err, "error attaching uprobe_multi link for %d functions", len(cookies))
	}
	p.links = append(p.links, link)
	return nil
}

// Drops returns how many calls the BPF program could not record because the
// ring buffer was full or the seen_funcs insert was rejected. It counts every
// such call, not distinct functions, so it is an upper bound on the functions
// missing from the report.
func (p *Probe) Drops() (uint64, error) {
	m, err := p.bpfMod.GetMap(dropsBPFMapName)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to get bpf map %s", dropsBPFMapName)
	}
	key := uint32(0)
	val, err := m.GetValue(unsafe.Pointer(&key))
	if err != nil {
		return 0, errors.Wrapf(err, "failed to lookup bpf map %s", dropsBPFMapName)
	}
	if len(val) != 8 {
		return 0, errors.Errorf("bpf map %s: value size %d, want 8", dropsBPFMapName, len(val))
	}
	return binary.LittleEndian.Uint64(val), nil
}

func (p *Probe) InitEventBuf(ctx context.Context) (chan []byte, error) {
	var err error

	events := make(chan []byte, EventsChBufSize)

	p.EvtBuf, err = p.bpfMod.InitRingBuf(evtRingBufBPFMapName, events)
	if err != nil {
		return nil, errors.Wrapf(err, "error initializing ring buffer %s", evtRingBufBPFMapName)
	}

	return events, nil
}

// PollEventBuf runs libbpf ring_buffer__poll() on the probe events ring
// buffer.
// PollEventBuf must be called out of a thread-locked goroutine,
// hence after InitEventBuf that calls libbpfgo InitRingBuffer().
// CGO goroutine thread-locked cannot use blocking operations like send
// to channel. Go runtime locks the goroutine to the thread when receiving
// the callback from C.
func (p *Probe) PollEventBuf() {
	p.EvtBuf.Poll(evtRingBufPollTimeout)
}

func (p *Probe) CloseEventBuf() {
	p.EvtBuf.Close()
}

// DetachLinks destroys all BPF links, detaching the uprobes so no new events
// are produced. Links must be explicitly destroyed because AttachUprobeMulti
// and AttachUprobeWithOpts both return a BPFLink that is the sole owner of the
// uprobe attachment - closing the module alone does not detach the probes.
// It is safe to call more than once; CloseBPFMod calls it as well.
func (p *Probe) DetachLinks() {
	for _, link := range p.links {
		if err := link.Destroy(); err != nil {
			p.logger.Warn().Err(err).Msg("failed to destroy BPF link")
		}
	}
	p.links = nil
}

// CloseBPFMod detaches any links still attached and then closes the BPF
// module. Must be called after CloseEventBuf so the ring buffer poll goroutine
// has already stopped.
func (p *Probe) CloseBPFMod() {
	p.DetachLinks()
	if p.bpfMod != nil {
		p.bpfMod.Close()
	}
}

// attachSingleUprobes attaches the probe to each (offset, cookie) pair using
// bpf_program__attach_uprobe_opts via libbpfgo. This is the path used in
// userspace BPF mode (bpftime), which supports single uprobes via perf_event_open
// but silently no-ops BPF_TRACE_UPROBE_MULTI.
func (p *Probe) attachSingleUprobes(exePath string, offsets, cookies []uint64) error {
	for i, offset := range offsets {
		cookie := cookies[i]
		link, err := p.bpfProg.AttachUprobeWithOpts(p.pid, exePath, offset, cookie)
		if err != nil {
			return fmt.Errorf("attach uprobe at offset 0x%x cookie 0x%x: %w", offset, cookie, err)
		}
		p.links = append(p.links, link)
	}

	p.logger.Debug().
		Int("attached", len(p.links)).
		Int("total", len(offsets)).
		Msg("single uprobes attached")

	return nil
}
