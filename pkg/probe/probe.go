package probe

import (
	"context"
	"embed"
	"fmt"
	"path/filepath"

	bpf "github.com/aquasecurity/libbpfgo"
	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
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
)

type Probe struct {
	Name string
	data []byte

	bpfMod  *bpf.Module
	bpfProg *bpf.BPFProg
	links   []*bpf.BPFLink

	EvtBuf *bpf.RingBuffer

	userspaceBPF bool

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

func NewProbe(opts ...Option) *Probe {
	p := new(Probe)
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

	if err := p.bpfMod.BPFLoadObject(); err != nil {
		return errors.Wrapf(err, "failed to load bpf module %s", p.Name)
	}

	return nil
}

func (p *Probe) configureBPFLogger() {
	bpf.SetLoggerCbs(bpf.Callbacks{
		Log: func(level int, msg string) {
			if level == bpf.LibbpfWarnLevel {
				// TODO: filter for specific attach failures.
				p.logger.Debug().Msgf("libbpf warning: %s", msg)
			}
		},
	})
}

func (p *Probe) Attach(_ context.Context, exePath string, offsets, cookies []uint64) error {
	if p.userspaceBPF {
		return p.attachSingleUprobes(exePath, offsets, cookies)
	}

	link, err := p.bpfProg.AttachUprobeMulti(-1, exePath, offsets, cookies)
	if err != nil {
		p.logger.Warn().Err(errors.Wrapf(err, "error attaching uprobe for functions with cookies: %v", cookies))
		return nil
	}
	p.links = append(p.links, link)
	return nil
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

// CloseBPFMod destroys all BPF links (detaching uprobes) and then closes
// the BPF module. Links must be explicitly destroyed because AttachUprobeMulti
// and AttachUprobeWithOpts both return a BPFLink that is the sole owner of the
// uprobe attachment - closing the module alone does not detach the probes.
// Must be called after CloseEventBuf so the ring buffer poll goroutine has
// already stopped.
func (p *Probe) CloseBPFMod() {
	for _, link := range p.links {
		link.Destroy()
	}
	p.links = nil
	if p.bpfMod != nil {
		p.bpfMod.Close()
	}
}

// attachSingleUprobes attaches the probe to each (offset, cookie) pair using
// bpf_program__attach_uprobe_opts via libbpfgo. This is the path used in
// userspace BPF mode (bpftime), which supports single uprobes via perf_event_open
// but silently no-ops BPF_TRACE_UPROBE_MULTI.
func (p *Probe) attachSingleUprobes(exePath string, offsets, cookies []uint64) error {
	var firstErr error
	attached := 0

	for i, offset := range offsets {
		cookie := cookies[i]
		link, err := p.bpfProg.AttachUprobeWithOpts(-1, exePath, offset, cookie)
		if err != nil {
			p.logger.Debug().
				Err(err).
				Uint64("offset", offset).
				Uint64("cookie", cookie).
				Msg("failed to attach uprobe with opts")
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		p.links = append(p.links, link)
		attached++
	}

	if attached == 0 && len(offsets) > 0 {
		return fmt.Errorf("all %d uprobe attachments failed (first error: %w)", len(offsets), firstErr)
	}

	p.logger.Debug().
		Int("attached", attached).
		Int("total", len(offsets)).
		Msg("single uprobes attached")

	return nil
}
