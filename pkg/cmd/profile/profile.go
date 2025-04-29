package profile

import (
	"fmt"
	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/maxgio92/xcover/pkg/trace"
)

type FuncName struct {
	Name [funNameLen]byte
}

const (
	funNameLen = 64
	CmdName    = "profile"
)

func NewCommand(o *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   CmdName,
		Short: "Profile the functional test coverage of a program",
		Long: fmt.Sprintf(`
%s runs the coverage profiling for functional tests by tracing all the functions supported by the program being tested.
It supports programs compiled to ELF.
`, CmdName),
		DisableAutoGenTag: true,
		RunE:              o.Run,
	}
	cmd.Flags().StringVarP(&o.comm, "path", "p", "", "Path to the ELF executable")
	cmd.Flags().IntVar(&o.pid, "pid", -1, "Filter the process by PID")

	cmd.Flags().StringVar(&o.symExcludePattern, "exclude", "", "Regex pattern to exclude function symbol names")
	cmd.Flags().StringVar(&o.symIncludePattern, "include", "", "Regex pattern to include function symbol names")

	cmd.Flags().BoolVar(&o.verbose, "verbose", false, "Enable verbosity")
	cmd.Flags().BoolVar(&o.report, "report", true, fmt.Sprintf("Generate report (as %s)", trace.ReportFileName))
	cmd.Flags().BoolVar(&o.status, "status", true, "Periodically print a status of the trace")

	cmd.MarkFlagRequired("path")

	return cmd
}

func (o *Options) Run(_ *cobra.Command, _ []string) error {
	logLevel, err := log.ParseLevel(o.LogLevel)
	if err != nil {
		o.Logger.Fatal().Err(err).Msg("invalid log level")
	}
	o.Logger = o.Logger.Level(logLevel)

	tracee := trace.NewUserTracee(
		trace.WithTraceeExePath(o.comm),
		trace.WithTraceeSymPatternInclude(o.symIncludePattern),
		trace.WithTraceeSymPatternExclude(o.symExcludePattern),
		trace.WithTraceeLogger(o.Logger),
	)

	tracer := trace.NewUserTracer(
		trace.WithTracerBpfObjBuf(o.Probe),
		trace.WithTracerBpfObjName(o.ProbeObjName),
		trace.WithTracerBpfProgName("handle_user_function"),
		trace.WithTracerLogger(o.Logger),
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
