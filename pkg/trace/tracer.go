package trace

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	bpf "github.com/maxgio92/libbpfgo"
	"github.com/pkg/errors"
	log "github.com/rs/zerolog"

	"github.com/maxgio92/utrace/internal/utils"
)

const (
	funNameLen                     = 64
	bpfMaxBufferSize               = 1024                 // Maximum size of bpf_attr needed to batch offsets for uprobe_multi attachments.
	bpfUprobeMultiAttachMaxOffsets = bpfMaxBufferSize / 8 // 8 is the byte size of uint64 used to represent offsets.
	reportFileName                 = "utrace-report.json"
)

var (
	libbpfErrKeywords = []string{"failed", "invalid", "error"}
	consumed          uint64
	eventsChBufSize   = 4096
	feedChBufSize     = 4096
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
	evtRingBuf *bpf.RingBuffer

	// Tracee objects.
	// TODO: decouple trace(s) from tracer and tracee.
	tracee *UserTracee

	// User functions being acknowledged.
	ack sync.Map

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

func (t *UserTracer) Init() error {
	if err := t.validate(); err != nil {
		return err
	}
	if t.writer == nil {
		t.writer = os.Stdout
	}
	if t.logger == nil {
		logger := log.New(log.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
		t.logger = &logger
	}
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

func (t *UserTracer) validate() error {
	if t.bpfModPath == "" {
		return errors.New("no BPF module path specified")
	}

	return nil
}

func (t *UserTracer) Load() error {
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

	// Attach one uprobe per function to trace.
	t.logger.Debug().Msg("attaching trace to selected functions")
	t.attachUprobes()

	eventsCh := make(chan []byte, eventsChBufSize)
	feedCh := make(chan []byte, feedChBufSize)
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
		t.ingestEvents(ctx, eventsCh, feedCh)
	}()

	// Consume events from internal feed.
	wg.Add(1)
	go func() {
		defer wg.Done()
		t.processEvents(ctx, feedCh)
	}()

	// Print status bar.
	go t.printStatusBar(ctx, eventsCh, feedCh)

	// Waiting for signals.
	<-ctx.Done()
	t.logger.Debug().Msg("received signal")

	// Waiting for reader and consumer to complete.
	wg.Wait()
	t.logger.Debug().Msg("terminating...")

	// Waiting to close ring buffer resources.
	t.evtRingBuf.Close()

	// Write report.
	return t.writeReport(reportFileName)
}

func (t *UserTracer) attachUprobes() {
	batchSize := bpfUprobeMultiAttachMaxOffsets

	offsets := t.tracee.GetFuncOffsets()
	cookies := t.tracee.GetFuncCookies()

	for i := 0; i < len(offsets); i += batchSize {
		end := i + batchSize
		if end > len(offsets) {
			end = len(offsets)
		}

		if _, err := t.bpfProg.AttachUprobeMulti(-1, t.tracee.exePath, offsets[i:end], cookies[i:end]); err != nil {
			t.logger.Warn().Err(errors.Wrapf(err, "error attaching uprobe for functions with cookies: %v", cookies[i:end]))
		}
	}
}

func (t *UserTracer) ingestEvents(ctx context.Context, events <-chan []byte, feed chan<- []byte) {
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

func (t *UserTracer) processEvents(ctx context.Context, feed <-chan []byte) {
	for {
		select {
		case data := <-feed:
			t.handleEvent(data)
		case <-ctx.Done():
			return
		}
	}
}

// TODO: decouple handle from handler functions as argument.
func (t *UserTracer) handleEvent(data []byte) {
	atomic.AddUint64(&consumed, 1)

	var event Event

	buf := bytes.NewBuffer(data)
	if err := binary.Read(buf, binary.LittleEndian, &event); err != nil {
		t.logger.Err(err).Msg("failed to read event")
	}

	if t.tracee == nil {
		return
	}
	fun, ok := t.tracee.funcs[event.Cookie]
	if !ok {
		t.logger.Err(fmt.Errorf("tracee function not found for cookie %d", event.Cookie))
	}

	if _, ok := t.ack.Load(event.Cookie); !ok {
		if t.verbose && t.writer != nil {
			fmt.Fprintln(t.writer, fun.name)
		}
		t.ack.Store(event.Cookie, struct{}{})
	}
}

func (t *UserTracer) configureBPFLogger() {
	bpf.SetLoggerCbs(bpf.Callbacks{
		Log: func(level int, msg string) {
			if level == bpf.LibbpfWarnLevel {
				// TODO: filter for specific attach failures.
				t.logger.Debug().Msgf("libbpf warning: %s", msg)
			}
		},
	})
}

func (t *UserTracer) writeReport(reportPath string) error {
	if !t.report {
		return nil
	}

	traced := make([]string, len(t.tracee.funcs))
	for _, fn := range t.tracee.funcs {
		traced = append(traced, fn.name)
	}

	ack := make([]string, utils.LenSyncMap(&t.ack))
	t.ack.Range(func(k, v interface{}) bool {
		fun, ok := t.tracee.funcs[k.(cookie)]
		if !ok {
			return false
		}
		ack = append(ack, fun.name)
		return true
	})

	report := NewReport(
		WithReportFuncsAck(ack),
		WithReportFuncsTraced(traced),
		WithReportFuncsCov(float64(len(ack))/float64(len(traced))*100),
		WithReportExePath(t.tracee.exePath),
	)

	file, err := os.Create(reportPath)
	if err != nil {
		t.logger.Err(err).Msg("failed to create report file")
	}
	defer file.Close()

	t.logger.Info().Msgf("written report to %s", reportPath)

	return report.WriteReport(file)
}

func shouldAbortOn(msg string) bool {
	for _, keyword := range libbpfErrKeywords {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}
