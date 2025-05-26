package probe

import (
	"context"
	"embed"
	"path/filepath"

	bpf "github.com/maxgio92/libbpfgo"
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

	EvtBuf *bpf.RingBuffer

	logger log.Logger
}

type Option func(p *Probe)

func WithLogger(logger log.Logger) Option {
	return func(p *Probe) {
		p.logger = logger
	}
}

func NewProbe(opts ...Option) *Probe {
	return new(Probe)
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

	p.bpfMod, err = bpf.NewModuleFromBuffer(p.Data(), p.Name)
	if err != nil {
		return errors.Wrapf(err, "failed to load bpf module: %s", p.Name)
	}

	p.bpfProg, err = p.bpfMod.GetProgram(p.Name)
	if err != nil {
		return errors.Wrapf(err, "failed to get bpf program: %s", p.Name)
	}

	if err := p.bpfProg.SetExpectedAttachType(bpf.BPFAttachTypeTraceUprobeMulti); err != nil {
		return errors.Wrapf(err, "failed to set expected attach type %s", bpf.BPFAttachTypeTraceUprobeMulti)
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
	if _, err := p.bpfProg.AttachUprobeMulti(-1, exePath, offsets, cookies); err != nil {
		p.logger.Warn().Err(errors.Wrapf(err, "error attaching uprobe for functions with cookies: %v", cookies))
	}
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
