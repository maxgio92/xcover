package profile

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/common"
	"github.com/maxgio92/xcover/pkg/trace"
)

type FuncName struct {
	Name [funNameLen]byte
}

const (
	funNameLen = 64
	CmdName    = "start"
)

func NewCommand(o *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   CmdName,
		Short: "Start the coverage profiling for a program",
		Long: fmt.Sprintf(`
%s starts the coverage profiling for functional tests by tracing all the functions supported by the program being tested.
It supports programs compiled to ELF.
`, CmdName),
		DisableAutoGenTag: true,
		RunE:              o.Run,
	}

	cmd.Flags().StringVarP(&o.comm, "path", "p", "", "Path to the ELF executable")
	cmd.Flags().IntVar(&o.pid, "pid", -1, "Filter the process by PID")

	cmd.Flags().StringVar(&o.symExcludePattern, "exclude", "", "Regex pattern to exclude function symbol names")
	cmd.Flags().StringVar(&o.symIncludePattern, "include", "", "Regex pattern to include function symbol names")

	cmd.Flags().BoolVarP(&o.detached, "detached", "d", false, "Run as daemon")
	cmd.Flags().BoolVar(&o.verbose, "verbose", false, "Enable verbosity")
	cmd.Flags().BoolVar(&o.report, "report", true, fmt.Sprintf("Generate report (as %s)", trace.ReportFileName))
	cmd.Flags().BoolVar(&o.status, "status", true, "Periodically print a status of the trace")

	cmd.MarkFlagRequired("path")

	return cmd
}

func (o *Options) Run(cmd *cobra.Command, _ []string) error {
	if o.detached {
		return o.daemonize()
	}

	// Store PID file.
	os.WriteFile(settings.PidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	defer os.Remove(settings.PidFile)

	var err error
	o.LogLevel, err = cmd.Flags().GetString("log-level")
	if err != nil {
		return errors.Wrap(err, "failed to get log level")
	}

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

func (o *Options) daemonize() error {
	// Check if already running.
	if common.IsDaemonRunning() {
		fmt.Println("Daemon already running")
		return nil
	}

	// Start daemon process.
	args := []string{"start"}
	args = append(args, fmt.Sprintf("--path=%s", o.comm))
	args = append(args, fmt.Sprintf("--exclude=%s", o.symExcludePattern))
	args = append(args, fmt.Sprintf("--include=%s", o.symIncludePattern))
	args = append(args, fmt.Sprintf("--report=%s", strconv.FormatBool(o.report)))
	args = append(args, fmt.Sprintf("--status=%s", strconv.FormatBool(o.status)))
	args = append(args, fmt.Sprintf("--verbose=%s", strconv.FormatBool(o.verbose)))

	cmd := exec.Command(os.Args[0], args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// Redirect output to log file.
	if settings.LogFile != "" {
		f, err := os.OpenFile(settings.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			o.Logger.Error().Err(err).Msg("failed to open log file")
			return err
		}
		cmd.Stdout = f
		cmd.Stderr = f
	}

	err := cmd.Start()
	if err != nil {
		o.Logger.Error().Err(err).Msg("failed to start daemon")
		return err
	}

	// Store PID file.
	err = os.WriteFile(settings.PidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644)
	if err != nil {
		o.Logger.Error().Err(err).Msg("failed to write PID file")
		return err
	}

	return nil
}
