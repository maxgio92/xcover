package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/maxgio92/utrace/pkg/trace"
	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

type FuncName struct {
	Name [funNameLen]byte
}

type Options struct {
	comm string
	pid  int

	symExcludePattern string
	symIncludePattern string

	logLevel string
	verbose  bool
	report   bool
	status   bool

	*CommonOptions
}

const (
	funNameLen   = 64
	logLevelInfo = "info"
)

func NewRootCmd(opts *CommonOptions) *cobra.Command {
	o := new(Options)
	o.CommonOptions = opts

	cmd := &cobra.Command{
		Use:               "utrace",
		Short:             "utrace is a userspace function tracer",
		Long:              `utrace is a kernel-assisted low-overhead userspace function tracer.`,
		DisableAutoGenTag: true,
		RunE:              o.Run,
	}
	cmd.Flags().StringVarP(&o.comm, "path", "p", "", "Path to the ELF executable")
	cmd.Flags().IntVar(&o.pid, "pid", -1, "Filter the process by PID")

	cmd.Flags().StringVar(&o.symExcludePattern, "exclude", "", "Regex pattern to exclude function symbol names")
	cmd.Flags().StringVar(&o.symIncludePattern, "include", "", "Regex pattern to include function symbol names")

	cmd.Flags().StringVar(&o.logLevel, "log-level", logLevelInfo, "Log level (trace, debug, info, warn, error, fatal, panic)")
	cmd.Flags().BoolVar(&o.verbose, "verbose", true, "Enable verbosity")
	cmd.Flags().BoolVar(&o.report, "report", false, "Generate report (as utrace-report.json)")
	cmd.Flags().BoolVar(&o.status, "status", false, "Periodically print a status of the trace")

	cmd.MarkFlagRequired("comm")

	return cmd
}

func Execute(probePath string) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	logger := log.New(
		log.ConsoleWriter{Out: os.Stderr},
	).With().Timestamp().Logger()

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
	if err := tracee.Init(); err != nil {
		return errors.Wrapf(err, "failed to init tracer")
	}

	tracer := trace.NewUserTracer(
		trace.WithTracerBpfModPath(o.ProbePath),
		trace.WithTracerBpfProgName("handle_user_function"),
		trace.WithTracerLogger(&o.Logger),
		trace.WithTracerEvtRingBufName("events"),
		trace.WithTracerVerbose(o.verbose),
		trace.WithTracerReport(o.report),
		trace.WithTracerStatus(o.status),
		trace.WithTracerTracee(tracee),
	)

	if err := tracer.Init(); err != nil {
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
