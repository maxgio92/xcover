package run

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/bpftime"
	"github.com/maxgio92/xcover/pkg/cmd/common"
	"github.com/maxgio92/xcover/pkg/cmd/options"
	"github.com/maxgio92/xcover/pkg/trace"
)

const CmdName = "run"

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

	if err := cmd.MarkFlagRequired("path"); err != nil {
		panic(err)
	}

	return cmd
}

func (o *Options) Run(cmd *cobra.Command, _ []string) error {
	if o.detach {
		return o.daemonize(cmd)
	}

	scope, err := o.setup()
	defer common.RemovePID()
	if err != nil {
		return err
	}

	if o.userspaceBPF {
		if err := bpftime.EnsureSyscallServer(); err != nil {
			return errors.Wrap(err, "failed to inject bpftime syscall-server")
		}
	}

	tracer := o.buildTracer(scope)

	if err := tracer.Init(o.Ctx); err != nil {
		return errors.Wrapf(err, "failed to init tracer")
	}
	if err := tracer.Run(o.Ctx); err != nil {
		return errors.Wrapf(err, "failed to run tracer")
	}

	return nil
}

// setup performs the PID file bookkeeping and function scope parsing needed
// before a tracer can be built. The PID file is written unconditionally so
// that the caller's deferred removal, armed right after this call, always
// cleans it up regardless of the returned error. Log-level configuration is
// handled centrally by the parent command's PersistentPreRunE before RunE
// runs, so o.Logger is already at the requested level here.
func (o *Options) setup() (trace.Scope, error) {
	// Store PID file.
	common.WritePID(os.Getpid())

	scope, err := trace.ParseScope(o.scope)
	if err != nil {
		return "", err
	}

	return scope, nil
}

// buildTracer constructs the tracee to trace and the tracer that drives it,
// applying the resolved scope and all tracer-related options.
func (o *Options) buildTracer(scope trace.Scope) *trace.UserTracer {
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

	return trace.NewUserTracer(
		trace.WithTracerLogger(o.Logger),
		trace.WithTracerVerbose(o.verbose),
		trace.WithTracerReport(o.report),
		trace.WithTracerStatus(o.status),
		trace.WithTracerUserspaceBPF(o.userspaceBPF),
		trace.WithTracerTracee(tracee),
	)
}

// daemonizeSkipFlags lists flags that must never be forwarded to the
// re-exec'd daemon process, because they control the re-exec itself
// (forwarding "detach" would make the daemon try to daemonize again).
var daemonizeSkipFlags = map[string]bool{
	"detach": true,
}

// forwardedFlagArgs walks the flags known to fs and returns the "--name=value"
// arguments needed to reproduce every flag the user explicitly set, skipping
// any flag named in skip. This lets a re-exec'd subprocess inherit whatever
// flags the current invocation was given without the caller having to
// hand-list every flag of the command.
func forwardedFlagArgs(fs *pflag.FlagSet, skip map[string]bool) []string {
	var args []string
	fs.VisitAll(func(f *pflag.Flag) {
		if !f.Changed || skip[f.Name] {
			return
		}
		args = append(args, fmt.Sprintf("--%s=%s", f.Name, f.Value.String()))
	})

	return args
}

func (o *Options) daemonize(cmd *cobra.Command) error {
	// Check if already running.
	if common.IsDaemonRunning() {
		fmt.Println("Daemon already running")
		return nil
	}

	// Start the daemon process, forwarding every flag the user set.
	args := append([]string{"run"}, forwardedFlagArgs(cmd.Flags(), daemonizeSkipFlags)...)

	daemonCmd := exec.Command(os.Args[0], args...)
	daemonCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// Redirect output to log file.
	if settings.LogFile != "" {
		f, err := os.OpenFile(settings.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			o.Logger.Error().Err(err).Msg("failed to open log file")
			return err
		}
		daemonCmd.Stdout = f
		daemonCmd.Stderr = f
	}

	err := daemonCmd.Start()
	if err != nil {
		o.Logger.Error().Err(err).Msgf("failed to start %s", settings.CmdName)
		return err
	}

	// Store PID file.
	err = common.WritePID(daemonCmd.Process.Pid)
	if err != nil {
		o.Logger.Error().Err(err).Msg("failed to write PID file")
		return err
	}

	return nil
}
