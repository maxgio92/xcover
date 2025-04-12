package cmd

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"unsafe"

	"github.com/aquasecurity/libbpfgo/helpers"
	"github.com/maxgio92/libbpfgo"
	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
	progressbar "github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"

	"github.com/maxgio92/utrace/pkg/static"
)

type FuncName struct {
	Name [funNameLen]byte
}

type Options struct {
	comm string
	pid  int
	*CommonOptions
}

const (
	funNameLen  = 64
	funCountMax = 16384
)

func NewRootCmd(opts *CommonOptions) *cobra.Command {
	o := &Options{"", -1, opts}
	cmd := &cobra.Command{
		Use:               "utrace",
		Short:             "utrace is a userspace function tracer",
		Long:              `utrace is a kernel-assisted low-overhead userspace function tracer.`,
		DisableAutoGenTag: true,
		RunE:              o.Run,
	}
	cmd.PersistentFlags().BoolVar(&o.Debug, "debug", false, "Sets log level to debug")
	cmd.Flags().StringVarP(&o.comm, "comm", "p", "", "Path to the ELF executable")
	cmd.Flags().IntVar(&o.pid, "pid", -1, "Filter the process by PID")
	cmd.MarkFlagRequired("comm")

	return cmd
}

func Execute(probePath string) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	logger := log.New(os.Stderr).Level(log.InfoLevel)

	go func() {
		<-ctx.Done()
		cancel()
	}()

	opts := NewCommonOptions(
		WithProbePath(probePath),
		WithContext(ctx),
		WithLogger(logger),
	)

	if err := NewRootCmd(opts).Execute(); err != nil {
		os.Exit(1)
	}
}

func (o *Options) Run(_ *cobra.Command, _ []string) error {
	if o.Debug {
		o.Logger = o.Logger.Level(log.DebugLevel)
	}

	syms, err := static.GetFuncs(o.comm)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	libbpfgo.SetLoggerCbs(libbpfgo.Callbacks{
		Log: func(level int, msg string) {
			return
		},
	})

	bpfModule, err := libbpfgo.NewModuleFromFile(o.ProbePath)
	if err != nil {
		return errors.Wrapf(err, "failed to load bpf module: %v", o.ProbePath)
	}
	defer bpfModule.Close()

	o.Logger.Debug().Msg("getting bpf program")
	prog, err := bpfModule.GetProgram("handle_user_function")
	if err != nil {
		return errors.Wrapf(err, "failed to get program: %v", prog.Name())
	}

	// Do not use perf events but uprobe_multi, since we attach one uprobe per function.
	// See: https://lore.kernel.org/bpf/20230424160447.2005755-1-jolsa@kernel.org/
	err = prog.SetExpectedAttachType(libbpfgo.BPFAttachTypeTraceUprobeMulti)
	if err != nil {
		return errors.Wrapf(err, "failed to set expected attach type %s", libbpfgo.BPFAttachTypeTraceUprobeMulti)
	}

	err = bpfModule.BPFLoadObject()
	if err != nil {
		return errors.Wrapf(err, "failed to load bpf object: %v", o.ProbePath)
	}

	ipMap, err := bpfModule.GetMap("ip_to_func_name_map")
	if err != nil {
		return errors.Wrapf(err, "failed to get map: %v", "ip_to_func_name_map")
	}

	// Fill the IP to function name map.
	i := 0
	for _, sym := range syms {
		if i > funCountMax {
			return errors.New("number of symbols is more than the IP-to-symbol-name BPF map max_entries")
		}
		var fn FuncName
		copy(fn.Name[:], []byte(sym.Name))

		key := make([]byte, funNameLen/8)
		binary.LittleEndian.PutUint64(key, sym.Value)

		err = ipMap.Update(unsafe.Pointer(&key[0]), unsafe.Pointer(&fn))
		if err != nil {
			return errors.Wrapf(err, "failed to update map: %v", ipMap.Name())
		}
		i++
	}

	o.Logger.Debug().Msg("analysing symbols")
	bar := progressbar.Default(int64(len(syms)))
	offsets := []uint64{}
	for _, sym := range syms {
		offset, err := helpers.SymbolToOffset(o.comm, sym.Name)
		if err != nil {
			return errors.Wrapf(err, "error finding function (%s) offset", sym.Name)
		}
		offsets = append(offsets, uint64(offset))
		bar.Add(1)
	}

	// Attach the uprobe for each function.
	o.Logger.Debug().Msgf("attaching bpf program to uprobes multi for pid %d", o.pid)
	_, err = prog.AttachUprobeMulti(o.pid, o.comm, offsets)
	if err != nil {
		errors.Wrapf(err, "error attaching uprobe at offsets: %v", offsets)
	}

	// Consume from ring buffer.
	o.Logger.Debug().Msg("initializing ring buffer")
	eventsCh := make(chan []byte)
	ringBuf, err := bpfModule.InitRingBuf("events", eventsCh)
	if err != nil {
		return errors.Wrapf(err, "failed to init ringBuf")
	}
	defer ringBuf.Close()

	// Poll from the ring buffer in background.
	o.Logger.Debug().Msg("polling ring buffer")
	go ringBuf.Poll(0)

	// Consume events from the ring buffer.
	captured := make(map[string]struct{}, 0)
	o.Logger.Debug().Msg("consuming events from ring buffer")
	for data := range eventsCh {
		var evt FuncName
		buf := bytes.NewBuffer(data)
		if err := binary.Read(buf, binary.LittleEndian, &evt); err != nil {
			o.Logger.Err(err).Msg("failed to read event")
		}

		name := string(bytes.TrimRight(evt.Name[:], "\x00"))
		if _, ok := captured[name]; !ok {
			fmt.Println(name)
			captured[name] = struct{}{}
		}
	}

	return nil
}
