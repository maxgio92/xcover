package trace

import (
	"bytes"
	"encoding/binary"
	"fmt"
	bpf "github.com/maxgio92/libbpfgo"
	"github.com/pkg/errors"
	"unsafe"
)

const funNameLen = 64

type FuncName struct {
	Name [funNameLen]byte
}

type UserTracer struct {
	// Tracer objects.
	bpfMod     *bpf.Module
	bpfProg    *bpf.BPFProg
	cookiesMap *bpf.BPFMap
	evtRingBuf *bpf.RingBuffer

	// Tracee objects.
	tracee *UserTracee

	*UserTracerOptions
}

func NewUserTracer(opts ...UserTracerOpt) (*UserTracer, error) {
	tracer := &UserTracer{
		UserTracerOptions: &UserTracerOptions{},
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
	var err error
	if err = t.bpfMod.BPFLoadObject(); err != nil {
		return errors.Wrapf(err, "failed to load bpf module: %v", t.bpfModPath)
	}

	t.cookiesMap, err = t.bpfMod.GetMap(t.cookiesMapName)
	if err != nil {
		return errors.Wrapf(err, "failed to get cookies map: %v", t.bpfModPath)
	}

	t.tracee = tracee

	if err = t.loadCookies(); err != nil {
		return errors.Wrapf(err, "failed to load cookies: %v", t.cookiesMapName)
	}

	return nil
}

func (t *UserTracer) Run() error {
	if t.tracee == nil {
		return errors.New("tracee is nil")
	}
	if t.tracee.exePath == "" {
		return errors.New("tracee exe path is empty")
	}
	if len(t.tracee.funcOffsets) == 0 {
		return errors.New("tracee offsets is empty")
	}
	if _, err := t.bpfProg.AttachUprobeMulti(-1, t.tracee.exePath, t.tracee.funcOffsets); err != nil {
		errors.Wrapf(err, "error attaching uprobe at offsets: %v", t.tracee.funcOffsets)
	}

	evtCh := make(chan []byte)
	var err error
	t.evtRingBuf, err = t.bpfMod.InitRingBuf(t.evtRingBufName, evtCh)
	if err != nil {
		return errors.Wrapf(err, "error attaching uprobe at offsets: %v", t.evtRingBufName)
	}
	defer t.evtRingBuf.Close()
	go t.evtRingBuf.Poll(0)

	// Consume events from the ring buffer.
	captured := make(map[string]struct{}, 0)
	t.logger.Debug().Msg("consuming events from ring buffer")
	for data := range evtCh {
		var evt FuncName
		buf := bytes.NewBuffer(data)
		if err := binary.Read(buf, binary.LittleEndian, &evt); err != nil {
			t.logger.Err(err).Msg("failed to read event")
		}

		name := string(bytes.TrimRight(evt.Name[:], "\x00"))
		if _, ok := captured[name]; !ok {
			fmt.Println(name)
			captured[name] = struct{}{}
		}
	}

	return nil
}

func (t *UserTracer) loadCookies() error {
	if t.tracee.funcSyms == nil {
		return errors.New("no function symbols found in the tracee")
	}
	if t.cookiesMap == nil {
		return errors.New("no cookies map found in the tracee")
	}
	for _, sym := range t.tracee.funcSyms {
		var fn FuncName

		copy(fn.Name[:], sym.Name)

		key := make([]byte, funNameLen/8)
		binary.LittleEndian.PutUint64(key, sym.Value)

		if err := t.cookiesMap.Update(unsafe.Pointer(&key[0]), unsafe.Pointer(&fn)); err != nil {
			return errors.Wrapf(err, "failed to update map: %v", t.cookiesMap.Name())
		}
	}

	return nil
}
