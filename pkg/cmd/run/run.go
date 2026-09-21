package run

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sys/unix"

	"github.com/maxgio92/xcover/internal/preflight"
	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/bpftime"
	"github.com/maxgio92/xcover/pkg/cmd/common"
	"github.com/maxgio92/xcover/pkg/cmd/options"
	"github.com/maxgio92/xcover/pkg/probe"
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

	// ringBufSize is the --ringbuf-size value as typed; ringBufBytes is the
	// validated size parseRingBufSize derives from it in setup.
	ringBufSize  string
	ringBufBytes uint32

	detach        bool
	skipPreflight bool
	verbose       bool
	report        bool
	status        bool
	userspaceBPF  bool

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
	cmd.Flags().IntVar(&o.pid, "pid", -1, "Only trace the process with this PID (kernel mode only; -1 traces every process executing the binary)")

	cmd.Flags().StringVar(&o.symExcludePattern, "exclude", "", "Regex pattern to exclude function symbol names, matched against the raw and the demangled name")
	cmd.Flags().StringVar(&o.symIncludePattern, "include", "", "Regex pattern to include function symbol names, matched against the raw and the demangled name")

	cmd.Flags().StringVar(&o.debugPath, "debug-path", "", "Path to a separate debug/symbol file (e.g. objcopy --only-keep-debug output) to resolve function names for a stripped --path binary")
	cmd.Flags().BoolVar(&o.noBuildIDCheck, "no-build-id-check", false, "Skip GNU build-id verification between --path and --debug-path")
	cmd.Flags().StringVar(&o.ringBufSize, "ringbuf-size", "16MiB", "Size of the events ring buffer, in bytes or with a KiB, MiB or GiB suffix; must be a power of two multiple of the page size, at most 2GiB")

	cmd.Flags().BoolVarP(&o.detach, "detach", "d", false, fmt.Sprintf("Run %s as daemon", settings.CmdName))
	cmd.Flags().BoolVar(&o.verbose, "verbose", false, "Enable verbosity")
	cmd.Flags().BoolVar(&o.report, "report", true, fmt.Sprintf("Generate report (as %s)", trace.ReportFileName))
	cmd.Flags().BoolVar(&o.status, "status", true, "Periodically print a status of the trace")
	cmd.Flags().StringVar(&o.scope, "scope", string(trace.ScopeBinary), `Function scope: "binary" (all functions) or "project" (project module only, Go binaries)`)
	cmd.Flags().BoolVar(&o.userspaceBPF, "userspace-bpf", false, "Run BPF programs in userspace via bpftime (experimental, implies --"+preflight.SkipFlag+")")
	cmd.Flags().BoolVar(&o.skipPreflight, preflight.SkipFlag, false, fmt.Sprintf("Skip the preflight checks: the kernel version advisory (Linux %s+ upstream, or a backport of uprobe_multi) and the capability check (CAP_BPF and CAP_PERFMON, or CAP_SYS_ADMIN)", preflight.MinKernel))

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

	if err := o.preflight(); err != nil {
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

// setup performs the PID file bookkeeping, function scope parsing and symbol
// pattern validation needed before a tracer can be built. The PID file is
// written before any parsing so that the caller's deferred removal, armed
// right after this call, always cleans it up regardless of the returned
// error. A detached child skips the write when the file already names it.
// When the child runs ahead of the parent's write in daemonize it writes
// atomically, and the parent's later write carries the same PID, so a reader
// never sees the file truncated. Log-level configuration is handled centrally
// by the parent command's PersistentPreRunE before RunE runs, so o.Logger is
// already at the requested level here.
func (o *Options) setup() (trace.Scope, error) {
	if pid, err := common.ReadPID(); err != nil || pid != os.Getpid() {
		if err := common.WritePID(os.Getpid()); err != nil {
			o.Logger.Warn().Err(err).Msg("failed to write PID file")
		}
	}

	if err := validatePID(o.pid, o.userspaceBPF); err != nil {
		return "", err
	}

	ringBufBytes, err := parseRingBufSize(o.ringBufSize)
	if err != nil {
		return "", err
	}
	o.ringBufBytes = ringBufBytes

	scope, err := trace.ParseScope(o.scope)
	if err != nil {
		return "", err
	}

	if err := trace.ValidateSymPatterns(o.symIncludePattern, o.symExcludePattern); err != nil {
		return "", err
	}

	return scope, nil
}

// preflight warns on an old-looking kernel and validates privileges before
// any BPF object is loaded.
func (o *Options) preflight() error {
	return preflight.Run(
		preflight.WithSkip(o.skipPreflight),
		preflight.WithUserspaceBPF(o.userspaceBPF),
		preflight.WithLogger(o.Logger),
	)
}

// validatePID checks the --pid flag. libbpf maps 0 to xcover's own PID and
// treats any negative pid as unfiltered, so a stray negative value would
// silently trace every process; only -1 (all processes) or a real PID are
// accepted. The value is passed to libbpf as a C int, so anything above
// MaxInt32 would be truncated: 4294967295 becomes -1 and silently traces
// every process while the report records the bogus PID. A PID that does not
// exist is rejected here so the error reaches the user before daemonizing;
// the kernel would otherwise refuse the uprobe_multi link with ESRCH during
// attach. pidfd_open(2) (Linux 5.3+) succeeds only for a thread-group leader,
// matching the kernel's uprobe_multi lookup, and needs no signal permission,
// so a process owned by someone else is still accepted. A non-leader thread
// id fails with EINVAL before Linux 6.16 and ENOENT since; before 6.16 EINVAL
// is also what a leader that exits during the lookup gets, so the two cases
// share one message. An unreaped zombie also passes, so the check proves
// existence, not liveness, and it is inherently racy.
//
// bpftime stores the uprobe pid but never compares it when hooking, so under
// --userspace-bpf a positive --pid would silently record hits from every
// process that loaded the agent; the combination is refused.
func validatePID(pid int, userspaceBPF bool) error {
	if pid == -1 {
		return nil
	}
	if pid <= 0 {
		return errors.Errorf("invalid --pid %d: must be -1 or a positive PID", pid)
	}
	if pid > math.MaxInt32 {
		return errors.Errorf("invalid --pid %d: must not exceed %d", pid, math.MaxInt32)
	}
	if userspaceBPF {
		return errors.New("--pid is not enforced by bpftime; drop --pid or run without --userspace-bpf")
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		switch {
		case errors.Is(err, unix.ESRCH):
			return errors.Wrapf(err, "--pid %d: no such process", pid)
		case errors.Is(err, unix.EINVAL), errors.Is(err, unix.ENOENT):
			return errors.Wrapf(err, "--pid %d: is gone or is not a thread-group leader PID", pid)
		}
		return errors.Wrapf(err, "--pid %d: pidfd_open failed", pid)
	}
	_ = unix.Close(fd)
	return nil
}

// ringBufSizeUnits maps the accepted --ringbuf-size suffixes to their byte
// multiplier. The match is case-sensitive: KB, MB, k, m and mib are refused
// rather than guessed at.
var ringBufSizeUnits = map[string]uint64{
	"KiB": 1 << 10,
	"MiB": 1 << 20,
	"GiB": 1 << 30,
}

// parseRingBufSize parses a --ringbuf-size value: a decimal byte count with
// an optional KiB, MiB or GiB suffix. The result must satisfy
// probe.ValidateRingBufSize, so the rule reaches the user here instead of
// libbpf rounding the value up silently or the kernel refusing it at load.
func parseRingBufSize(s string) (uint32, error) {
	digits, unit := s, ""
	for i, r := range s {
		if r < '0' || r > '9' {
			digits, unit = s[:i], s[i:]
			break
		}
	}
	mult := uint64(1)
	if unit != "" {
		var ok bool
		if mult, ok = ringBufSizeUnits[unit]; !ok {
			return 0, errors.Errorf("invalid --ringbuf-size %q: use a byte count or a KiB, MiB or GiB suffix", s)
		}
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, errors.Errorf("invalid --ringbuf-size %q: use a byte count or a KiB, MiB or GiB suffix", s)
	}
	if n > math.MaxUint64/mult {
		return 0, errors.Errorf("invalid --ringbuf-size %q: does not fit in 64 bits", s)
	}
	size := n * mult
	if err := probe.ValidateRingBufSize(size); err != nil {
		return 0, errors.Wrapf(err, "--ringbuf-size %s", s)
	}

	return uint32(size), nil
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
		trace.WithTracerPID(o.pid),
		trace.WithTracerRingBufSize(o.ringBufBytes),
		trace.WithTracerTracee(tracee),
	)
}

// daemonizeSkipFlags lists flags that must never be forwarded to the
// re-exec'd daemon process, because they control the re-exec itself
// (forwarding "detach" would make the daemon try to daemonize again).
var daemonizeSkipFlags = map[string]bool{
	"detach": true,
}

// execCommand builds the daemon child process. Tests swap it to observe the
// arguments and to avoid spawning a real daemon.
var execCommand = exec.Command

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

// daemonArgs forwards the user's flags and turns preflight off in the child
// unless the user set --skip-preflight explicitly: daemonize has already run
// (or skipped) preflight in the parent, so the child repeating it would only
// duplicate the kernel advisory in the log file.
func daemonArgs(fs *pflag.FlagSet) []string {
	args := forwardedFlagArgs(fs, daemonizeSkipFlags)
	if f := fs.Lookup(preflight.SkipFlag); f != nil && !f.Changed {
		args = append(args, "--"+preflight.SkipFlag+"=true")
	}

	return args
}

func (o *Options) daemonize(cmd *cobra.Command) error {
	// Validate the target before forking so the error reaches the user
	// instead of only the daemon log.
	if err := validatePID(o.pid, o.userspaceBPF); err != nil {
		return err
	}

	// Refuse a bad ring buffer size in the parent, in the same position setup
	// checks it, before the running-daemon check and the preflight. The rule
	// reaches the user without root and no daemon is started only to fail at
	// load.
	if _, err := parseRingBufSize(o.ringBufSize); err != nil {
		return err
	}

	// Reject invalid symbol patterns here, in the parent: the daemon would
	// otherwise fail after this process has already returned success.
	if err := trace.ValidateSymPatterns(o.symIncludePattern, o.symExcludePattern); err != nil {
		return err
	}

	// Check if already running.
	if common.IsDaemonRunning() {
		fmt.Println("Daemon already running")
		return nil
	}

	// Fail fast in the foreground: the daemon would only report this in its
	// log file.
	if err := o.preflight(); err != nil {
		return err
	}

	// Start the daemon process, forwarding every flag the user set.
	args := append([]string{"run"}, daemonArgs(cmd.Flags())...)

	daemonCmd := execCommand(os.Args[0], args...)
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
