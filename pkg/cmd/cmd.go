package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/wait"
	"github.com/maxgio92/xcover/pkg/trace"
)

type FuncName struct {
	Name [funNameLen]byte
}

const (
	funNameLen   = 64
	logLevelInfo = "info"
)

func NewCommand(o *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               settings.CmdName,
		Short:             fmt.Sprintf("%s is a userspace function tracer", settings.CmdName),
		Long:              fmt.Sprintf(`%s is a kernel-assisted low-overhead userspace function tracer.`, settings.CmdName),
		DisableAutoGenTag: true,
		RunE:              o.Run,
	}
	cmd.Flags().StringVarP(&o.comm, "path", "p", "", "Path to the ELF executable")
	cmd.Flags().IntVar(&o.pid, "pid", -1, "Filter the process by PID")

	cmd.Flags().StringVar(&o.symExcludePattern, "exclude", "", "Regex pattern to exclude function symbol names")
	cmd.Flags().StringVar(&o.symIncludePattern, "include", "", "Regex pattern to include function symbol names")

	cmd.Flags().StringVar(&o.logLevel, "log-level", logLevelInfo, "Log level (trace, debug, info, warn, error, fatal, panic)")
	cmd.Flags().BoolVar(&o.verbose, "verbose", true, "Enable verbosity")
	cmd.Flags().BoolVar(&o.report, "report", false, fmt.Sprintf("Generate report (as %s)", trace.ReportFileName))
	cmd.Flags().BoolVar(&o.status, "status", false, "Periodically print a status of the trace")

	cmd.MarkFlagRequired("path")

	cmd.AddCommand(wait.NewCommand(
		wait.NewOptions(
			wait.WithLogger(o.Logger),
			wait.WithContext(o.Ctx),
		),
	))

	return cmd
}

func Execute(probe []byte, probeObjName string) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	logger := log.New(
		log.ConsoleWriter{Out: os.Stderr},
	).With().Timestamp().Logger()

	go func() {
		<-ctx.Done()
		cancel()
	}()

	opts := NewOptions(
		WithProbe(probe),
		WithProbeObjName(probeObjName),
		WithContext(ctx),
		WithLogger(logger),
	)

	if err := NewCommand(opts).Execute(); err != nil {
		os.Exit(1)
	}
}

func (o *Options) Run(_ *cobra.Command, _ []string) error {
	logLevel, err := log.ParseLevel(o.logLevel)
	if err != nil {
		o.Logger.Fatal().Err(err).Msg("invalid log level")
	}
	o.Logger = o.Logger.Level(logLevel)

	tracee := trace.NewUserTracee(
		trace.WithTraceeExePath(o.comm),
		trace.WithTraceeSymPatternInclude(o.symIncludePattern),
		trace.WithTraceeSymPatternExclude(o.symExcludePattern),
		trace.WithTraceeLogger(&o.Logger),
	)

	tracer := trace.NewUserTracer(
		trace.WithTracerBpfObjBuf(o.Probe),
		trace.WithTracerBpfObjName(o.ProbeObjName),
		trace.WithTracerBpfProgName("handle_user_function"),
		trace.WithTracerLogger(&o.Logger),
		trace.WithTracerEvtRingBufName("events"),
		trace.WithTracerVerbose(o.verbose),
		trace.WithTracerReport(o.report),
		trace.WithTracerStatus(o.status),
		trace.WithTracerTracee(tracee),
	)

	if err := tracer.Init(o.Ctx); err != nil {
		return errors.Wrapf(err, "failed to init tracer")
	}
	if err := tracer.Load(); err != nil {
		return errors.Wrapf(err, "failed to load tracer")
	}
	if err := tracer.Run(o.Ctx); err != nil {
		return errors.Wrapf(err, "failed to run tracer")
	}

	return nil
}
