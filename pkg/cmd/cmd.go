package cmd

import (
	"context"
	"github.com/maxgio92/utrace/pkg/trace"
	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"os"
	"os/signal"
	"syscall"
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

	tracer, err := trace.NewUserTracer(
		trace.WithBpfModPath(o.ProbePath),
		trace.WithBpfProgName("handle_user_function"),
		trace.WithLogger(&o.Logger),
		trace.WithCookiesMapName("ip_to_func_name_map"),
		trace.WithEvtRingBufName("events"),
	)
	if err != nil {
		return errors.Wrapf(err, "failed to create tracer")
	}

	if err := tracer.Init(); err != nil {
		return errors.Wrapf(err, "failed to init tracer")
	}

	tracee := trace.NewUserTracee(
		trace.WithExePath(o.comm),
	)
	if err := tracee.Init(); err != nil {
		return errors.Wrapf(err, "failed to init tracer")
	}
	if err := tracer.Load(tracee); err != nil {
		return errors.Wrapf(err, "failed to load tracer")
	}
	if err := tracer.Run(); err != nil {
		return errors.Wrapf(err, "failed to run tracer")
	}

	return nil
}
