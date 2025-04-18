package trace

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"

	bpf "github.com/maxgio92/libbpfgo"
	"github.com/pkg/errors"
)

const (
	funNameLen                     = 64
	bpfMaxBufferSize               = 1024                 // Maximum size of bpf_attr needed to batch offsets for uprobe_multi attachments.
	bpfUprobeMultiAttachMaxOffsets = bpfMaxBufferSize / 8 // 8 is the byte size of uint64 used to represent offsets.
)

var (
	libbpfErrKeywords = []string{"failed", "invalid", "error"}
)

type FuncName struct {
	Name [funNameLen]byte
}

type Event struct {
	Cookie cookie
}

type UserTracer struct {
	// Tracer objects.
	bpfMod     *bpf.Module
	bpfProg    *bpf.BPFProg
	cookiesMap *bpf.BPFMap
	evtRingBuf *bpf.RingBuffer

	// Tracee objects.
	// TODO: decouple trace(s) from tracer and tracee.
	tracee *UserTracee

	ack map[cookie]struct{}

	*UserTracerOptions
}

func NewUserTracer(opts ...UserTracerOpt) (*UserTracer, error) {
	tracer := &UserTracer{
		UserTracerOptions: &UserTracerOptions{},
		ack:               make(map[cookie]struct{}),
	}
	for _, opt := range opts {
		opt(tracer)
	}
	if tracer.bpfModPath == "" {
		return nil, errors.New("no BPF module path specified")
	}
	if tracer.cookiesMapName == "" {
		return nil, errors.New("no cookies map name specified")
	}

	return tracer, nil
}

func (t *UserTracer) Init() error {
	t.configureBPFLogger()

	var err error
	t.bpfMod, err = bpf.NewModuleFromFile(t.bpfModPath)
	if err != nil {
		return errors.Wrapf(err, "failed to load bpf module: %v", t.bpfModPath)
	}

	t.bpfProg, err = t.bpfMod.GetProgram(t.bpfProgName)
	if err != nil {
		return errors.Wrapf(err, "failed to get bpf program: %v", t.bpfProgName)
	}

	err = t.bpfProg.SetExpectedAttachType(bpf.BPFAttachTypeTraceUprobeMulti)
	if err != nil {
		return errors.Wrapf(err, "failed to set expected attach type %s", bpf.BPFAttachTypeTraceUprobeMulti)
	}

	return nil
}

func (t *UserTracer) Load(tracee *UserTracee) error {
	t.tracee = tracee

	if err := t.bpfMod.BPFLoadObject(); err != nil {
		return errors.Wrapf(err, "failed to load bpf module: %v", t.bpfModPath)
	}

	return nil
}

func (t *UserTracer) Run(ctx context.Context) error {
	if t.tracee == nil {
		return errors.New("tracee is nil")
	}
	if t.tracee.exePath == "" {
		return errors.New("tracee exe path is empty")
	}
	if len(t.tracee.funcs) == 0 {
		return errors.New("tracee offsets is empty")
	}

	batchSize := bpfUprobeMultiAttachMaxOffsets
	fmt.Println("batching in size", batchSize)
	j := 0
	offsets := t.tracee.getFuncOffsets()
	cookies := t.tracee.getFuncCookies()
	for i := 0; i < len(offsets); i += batchSize {
		end := i + batchSize
		if end > len(offsets) {
			end = len(offsets)
		}

		if _, err := t.bpfProg.AttachUprobeMulti(-1, t.tracee.exePath, offsets[i:end], cookies[i:end]); err != nil {
			t.logger.Warn().Err(errors.Wrapf(err, "error attaching uprobe for functions with cookies: %v", cookies[i:end]))
		}
		j++
	}

	eventsCh := make(chan []byte, 108192)
	feedCh := make(chan []byte, 108192)
	var err error
	t.evtRingBuf, err = t.bpfMod.InitRingBuf(t.evtRingBufName, eventsCh)
	if err != nil {
		return errors.Wrapf(err, "error attaching uprobe at offsets: %v", t.evtRingBufName)
	}

	go t.evtRingBuf.Poll(60)

	// Read events from the ring buffer to internal feed.
	t.logger.Debug().Msg("consuming events from ring buffer")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t.readEvents(ctx, eventsCh, feedCh)
	}()

	// Consume events from internal feed.
	wg.Add(1)
	go func() {
		defer wg.Done()
		t.consumeFeed(ctx, feedCh)
	}()

	// Waiting for signals.
	<-ctx.Done()
	t.logger.Info().Msg("received signal")

	// Waiting for reader and consumer to complete.
	wg.Wait()
	t.logger.Info().Msg("terminating...")

	// Waiting to close ring buffer resources.
	t.evtRingBuf.Close()

	return nil
}

func (t *UserTracer) readEvents(ctx context.Context, events <-chan []byte, feed chan<- []byte) {
	for {
		select {
		case data := <-events:
			// This must be as fast as possible.
			feed <- data
		case <-ctx.Done():
			return
		}
	}
}

func (t *UserTracer) consumeFeed(ctx context.Context, feed <-chan []byte) {
	for {
		select {
		case data := <-feed:
			t.handleEvent(data)
		case <-ctx.Done():
			return
		}
	}
}

func (t *UserTracer) handleEvent(data []byte) {
	var event Event

	buf := bytes.NewBuffer(data)
	if err := binary.Read(buf, binary.LittleEndian, &event); err != nil {
		t.logger.Err(err).Msg("failed to read event")
	}

	fun, ok := t.tracee.funcs[event.Cookie]
	if !ok {
		t.logger.Err(fmt.Errorf("tracee function not found for cookie %d", event.Cookie))
	}
	if _, ok := t.ack[event.Cookie]; !ok {
		fmt.Println(fun.name)
		t.ack[event.Cookie] = struct{}{}
	}
}

func (t *UserTracer) configureBPFLogger() {
	bpf.SetLoggerCbs(bpf.Callbacks{
		Log: func(level int, msg string) {
			if level == bpf.LibbpfWarnLevel {
				// TODO: filter for specific attach failures.
				t.logger.Warn().Msgf("libbpf warning:", msg)
			}
		},
	})
}

func shouldAbortOn(msg string) bool {
	for _, keyword := range libbpfErrKeywords {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}

func hash(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))

	return h.Sum64()
}
