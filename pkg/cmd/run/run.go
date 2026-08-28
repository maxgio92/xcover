package run

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/bpftime"
	"github.com/maxgio92/xcover/pkg/cmd/common"
	"github.com/maxgio92/xcover/pkg/cmd/options"
	"github.com/maxgio92/xcover/pkg/trace"
)

const (
	funNameLen = 64
	CmdName    = "run"
)

type FuncName struct {
	Name [funNameLen]byte
}

type Options struct {
	comm string
	pid  int

	symExcludePattern string
	symIncludePattern string
	scope             string

	debugPath      string
	noBuildIDCheck bool

	detach       bool
	verbose      bool
	report       bool
	status       bool
	userspaceBPF bool

	*options.Options
}

func NewCommand(opts *options.Options) *cobra.Command {
	o := new(Options)
	o.Options = opts
	cmd := &cobra.Command{
		Use:   CmdName,
		Short: "Run the coverage profiling for a program",
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

	cmd.Flags().StringVar(&o.debugPath, "debug-path", "", "Path to a separate debug/symbol file (e.g. objcopy --only-keep-debug output) to resolve function names for a stripped --path binary")
	cmd.Flags().BoolVar(&o.noBuildIDCheck, "no-build-id-check", false, "Skip GNU build-id verification between --path and --debug-path")

	cmd.Flags().BoolVarP(&o.detach, "detach", "d", false, fmt.Sprintf("Run %s as daemon", settings.CmdName))
	cmd.Flags().BoolVar(&o.verbose, "verbose", false, "Enable verbosity")
	cmd.Flags().BoolVar(&o.report, "report", true, fmt.Sprintf("Generate report (as %s)", trace.ReportFileName))
	cmd.Flags().BoolVar(&o.status, "status", true, "Periodically print a status of the trace")
	cmd.Flags().StringVar(&o.scope, "scope", string(trace.ScopeBinary), `Function scope: "binary" (all functions) or "project" (project module only, Go binaries)`)
	cmd.Flags().BoolVar(&o.userspaceBPF, "userspace-bpf", false, "Run BPF programs in userspace via bpftime (experimental)")

	cmd.MarkFlagRequired("path")

	return cmd
}

func (o *Options) Run(cmd *cobra.Command, _ []string) error {
	if o.detach {
		return o.daemonize()
	}

	// Store PID file.
	os.WriteFile(settings.PidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	defer os.Remove(settings.PidFile)

	scope, err := trace.ParseScope(o.scope)
	if err != nil {
		return err
	}

	if o.userspaceBPF {
		if err := bpftime.EnsureSyscallServer(); err != nil {
			return errors.Wrap(err, "failed to inject bpftime syscall-server")
		}
	}

	traceeOpts := []trace.UserTraceeOption{
		trace.WithTraceeExePath(o.comm),
		trace.WithTraceeSymPatternInclude(o.symIncludePattern),
		trace.WithTraceeSymPatternExclude(o.symExcludePattern),
		trace.WithTraceeScope(scope),
		trace.WithTraceeLogger(o.Logger),
	}
	if o.debugPath != "" {
		// Resolve names/addresses from the companion debug file while computing
		// uprobe offsets against the (stripped) executable at --path.
		traceeOpts = append(traceeOpts, trace.WithTraceeResolver(
			trace.SeparateDebugResolver(o.comm, o.debugPath, o.Logger,
				o.symIncludePattern, o.symExcludePattern, nil, nil, o.noBuildIDCheck),
		))
	}
	tracee := trace.NewUserTracee(traceeOpts...)

	tracer := trace.NewUserTracer(
		trace.WithTracerLogger(o.Logger),
		trace.WithTracerVerbose(o.verbose),
		trace.WithTracerReport(o.report),
		trace.WithTracerStatus(o.status),
		trace.WithTracerUserspaceBPF(o.userspaceBPF),
		trace.WithTracerTracee(tracee),
	)

	if err := tracer.Init(o.Ctx); err != nil {
		return errors.Wrapf(err, "failed to init tracer")
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

	// Start the daemon process.
	args := []string{"run"}
	args = append(args, fmt.Sprintf("--path=%s", o.comm))
	args = append(args, fmt.Sprintf("--exclude=%s", o.symExcludePattern))
	args = append(args, fmt.Sprintf("--include=%s", o.symIncludePattern))
	args = append(args, fmt.Sprintf("--report=%s", strconv.FormatBool(o.report)))
	args = append(args, fmt.Sprintf("--status=%s", strconv.FormatBool(o.status)))
	args = append(args, fmt.Sprintf("--verbose=%s", strconv.FormatBool(o.verbose)))
	args = append(args, fmt.Sprintf("--scope=%s", o.scope))
	if o.debugPath != "" {
		args = append(args, fmt.Sprintf("--debug-path=%s", o.debugPath))
	}
	if o.noBuildIDCheck {
		args = append(args, "--no-build-id-check")
	}
	if o.userspaceBPF {
		args = append(args, "--userspace-bpf")
	}

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
		o.Logger.Error().Err(err).Msgf("failed to start %s", settings.CmdName)
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
