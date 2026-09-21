package run

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	log "github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/common"
	"github.com/maxgio92/xcover/pkg/cmd/options"
	"github.com/maxgio92/xcover/pkg/trace"
)

func newTestOptions(t *testing.T) *Options {
	t.Helper()

	logger := log.New(log.ConsoleWriter{Out: os.Stderr})
	o := new(Options)
	// Mirror the --pid and --ringbuf-size flag defaults; the zero values are
	// rejected by setup().
	o.pid = -1
	o.ringBufSize = "16MiB"
	o.Options = options.NewOptions(
		options.WithContext(context.Background()),
		options.WithLogger(logger),
		options.WithLogLevel(log.LevelInfoValue),
	)

	return o
}

func TestOptionsSetup(t *testing.T) {
	// setup() writes to the shared settings.PidFile path, so point it at a
	// per-test temp file to avoid clobbering a real daemon's PID file.
	origPidFile := settings.PidFile
	settings.PidFile = filepath.Join(t.TempDir(), "xcover.pid")
	t.Cleanup(func() { settings.PidFile = origPidFile })

	tests := []struct {
		name         string
		scope        string
		pid          int
		userspaceBPF bool
		include      string
		exclude      string
		ringBufSize  string // empty keeps the flag default
		wantScope    trace.Scope
		wantErr      bool
		wantErrIs    error
	}{
		{
			name:      "binary scope",
			scope:     string(trace.ScopeBinary),
			pid:       -1,
			wantScope: trace.ScopeBinary,
		},
		{
			name:      "valid patterns",
			scope:     string(trace.ScopeBinary),
			pid:       -1,
			include:   `^main\.`,
			exclude:   `^runtime\.`,
			wantScope: trace.ScopeBinary,
		},
		{
			name:      "invalid include pattern",
			scope:     string(trace.ScopeBinary),
			pid:       -1,
			include:   "(",
			wantErr:   true,
			wantErrIs: trace.ErrInvalidPattern,
		},
		{
			name:      "invalid exclude pattern",
			scope:     string(trace.ScopeBinary),
			pid:       -1,
			exclude:   "[",
			wantErr:   true,
			wantErrIs: trace.ErrInvalidPattern,
		},
		{
			name:      "project scope",
			scope:     string(trace.ScopeProject),
			pid:       -1,
			wantScope: trace.ScopeProject,
		},
		{
			name:    "unknown scope",
			scope:   "bogus",
			pid:     -1,
			wantErr: true,
		},
		{
			name:      "running pid",
			scope:     string(trace.ScopeBinary),
			pid:       os.Getpid(),
			wantScope: trace.ScopeBinary,
		},
		{
			// Above pid_max on every Linux configuration, so it can never be
			// a running process.
			name:    "nonexistent pid is rejected",
			scope:   string(trace.ScopeBinary),
			pid:     1<<22 + 1,
			wantErr: true,
		},
		{
			name:    "pid zero is rejected",
			scope:   string(trace.ScopeBinary),
			pid:     0,
			wantErr: true,
		},
		{
			name:    "negative pid other than -1 is rejected",
			scope:   string(trace.ScopeBinary),
			pid:     -2,
			wantErr: true,
		},
		{
			// libbpf takes a C int; anything wider would be truncated.
			name:    "pid above MaxInt32 is rejected",
			scope:   string(trace.ScopeBinary),
			pid:     math.MaxInt32 + 1,
			wantErr: true,
		},
		{
			// Truncates to pid_t -1, which libbpf reads as every process.
			name:    "pid 1<<32-1 is rejected",
			scope:   string(trace.ScopeBinary),
			pid:     1<<32 - 1,
			wantErr: true,
		},
		{
			// Truncates to 0, which libbpf maps to xcover's own PID.
			name:    "pid 1<<32 is rejected",
			scope:   string(trace.ScopeBinary),
			pid:     1 << 32,
			wantErr: true,
		},
		{
			// bpftime stores the pid but never enforces it.
			name:         "positive pid with userspace BPF is rejected",
			scope:        string(trace.ScopeBinary),
			pid:          os.Getpid(),
			userspaceBPF: true,
			wantErr:      true,
		},
		{
			name:         "all processes with userspace BPF",
			scope:        string(trace.ScopeBinary),
			pid:          -1,
			userspaceBPF: true,
			wantScope:    trace.ScopeBinary,
		},
		{
			// libbpf would round this up to 128KiB silently.
			name:        "bad ring buffer size is rejected",
			scope:       string(trace.ScopeBinary),
			pid:         -1,
			ringBufSize: "100KiB",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := newTestOptions(t)
			o.scope = tt.scope
			o.pid = tt.pid
			o.userspaceBPF = tt.userspaceBPF
			o.symIncludePattern = tt.include
			o.symExcludePattern = tt.exclude
			if tt.ringBufSize != "" {
				o.ringBufSize = tt.ringBufSize
			}

			// A stale PID from another process must be replaced.
			require.NoError(t, os.WriteFile(settings.PidFile, []byte("1"), 0644))
			t.Cleanup(func() { _ = os.Remove(settings.PidFile) })

			scope, err := o.setup()

			// setup() writes the PID file before parsing anything, so the
			// caller can unconditionally defer its removal.
			got, readErr := os.ReadFile(settings.PidFile)
			require.NoError(t, readErr)
			require.Equal(t, strconv.Itoa(os.Getpid()), string(got))

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrIs != nil {
					require.ErrorIs(t, err, tt.wantErrIs)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantScope, scope)
			require.Equal(t, uint32(1<<24), o.ringBufBytes)
		})
	}
}

func TestParseRingBufSize(t *testing.T) {
	accepted := []struct {
		in   string
		want uint32
	}{
		{strconv.Itoa(os.Getpagesize()), uint32(os.Getpagesize())},
		{"64KiB", 64 << 10},
		{"16MiB", 16 << 20},
		{"1GiB", 1 << 30},
		{"2GiB", 1 << 31},
	}
	for _, tt := range accepted {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseRingBufSize(tt.in)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	const suffixes = "use a byte count or a KiB, MiB or GiB suffix"
	const rule = "must be a power of two, a multiple of the"
	rejected := []struct {
		in      string
		wantErr string
	}{
		{"", suffixes},
		{"16MB", suffixes},
		{"16KB", suffixes},
		{"16k", suffixes},
		{"16m", suffixes},
		{"16mib", suffixes},
		{"-1", suffixes},
		{"abc", suffixes},
		{"100KiB", rule},
		{"4GiB", rule},
		// 2^64-1 GiB: the multiplication would wrap to a size that passes
		// the rule.
		{"18446744073709551615GiB", "does not fit in 64 bits"},
	}
	for _, tt := range rejected {
		t.Run(strconv.Quote(tt.in), func(t *testing.T) {
			_, err := parseRingBufSize(tt.in)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// TestOptionsSetup_SkipsRewriteWhenPIDFileNamesSelf proves the detached
// child leaves a PID file that already names it untouched: no truncate and no
// rename, so the inode survives setup().
func TestOptionsSetup_SkipsRewriteWhenPIDFileNamesSelf(t *testing.T) {
	origPidFile := settings.PidFile
	settings.PidFile = filepath.Join(t.TempDir(), "xcover.pid")
	t.Cleanup(func() { settings.PidFile = origPidFile })

	want := strconv.Itoa(os.Getpid())
	require.NoError(t, os.WriteFile(settings.PidFile, []byte(want), 0644))
	// Age the file so any write, in place or by rename, moves its mtime.
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(settings.PidFile, old, old))
	before, err := os.Stat(settings.PidFile)
	require.NoError(t, err)

	o := newTestOptions(t)
	o.scope = string(trace.ScopeBinary)
	_, err = o.setup()
	require.NoError(t, err)

	got, err := os.ReadFile(settings.PidFile)
	require.NoError(t, err)
	require.Equal(t, want, string(got))

	after, err := os.Stat(settings.PidFile)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after), "setup replaced the PID file")
	require.Equal(t, before.ModTime(), after.ModTime(), "setup wrote the PID file")
}

// TestValidatePIDThread proves the pre-check rejects a non-leader thread id:
// the kernel resolves the uprobe_multi pid as a thread group, so kill(2)
// accepting a tid would only defer the ESRCH to the daemon log.
func TestValidatePIDThread(t *testing.T) {
	// Pin the test to its thread and lock a helper goroutine to another one;
	// both stay locked and alive until the check has run. Either thread may
	// be the thread-group leader, so pick whichever is not.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	self := unix.Gettid()

	tid := make(chan int)
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		tid <- unix.Gettid()
		<-done
	}()
	defer close(done)

	id := <-tid
	require.NotEqual(t, self, id, "helper goroutine must run on a different thread")
	if id == os.Getpid() {
		id = self
	}

	err := validatePID(id, false)
	require.ErrorContains(t, err, "is gone or is not a thread-group leader PID")
	require.NoError(t, validatePID(os.Getpid(), false))
	require.ErrorContains(t, validatePID(1<<22+1, false), "no such process")
}

// TestOptionsDaemonizeRefusesBadRingBufSize proves the parent refuses a bad
// --ringbuf-size before forking: the error reaches the caller instead of only
// the daemon log, and no PID file names a daemon that never started.
func TestOptionsDaemonizeRefusesBadRingBufSize(t *testing.T) {
	origPidFile := settings.PidFile
	settings.PidFile = filepath.Join(t.TempDir(), "xcover.pid")
	t.Cleanup(func() { settings.PidFile = origPidFile })

	o := newTestOptions(t)
	o.ringBufSize = "100KiB"

	// The refusal precedes every read of cmd, so an empty command suffices.
	err := o.daemonize(&cobra.Command{})
	require.ErrorContains(t, err, "must be a power of two")

	_, err = os.Stat(settings.PidFile)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestOptionsBuildTracer(t *testing.T) {
	o := newTestOptions(t)
	o.comm = "/bin/true"

	tracer := o.buildTracer(trace.ScopeBinary)

	require.NotNil(t, tracer)
}

// newRunFlagSet mirrors the run command flags for forwarding tests.
func newRunFlagSet() *pflag.FlagSet {
	fs := pflag.NewFlagSet("run", pflag.ContinueOnError)
	fs.String("path", "", "")
	fs.Int("pid", -1, "")
	fs.String("exclude", "", "")
	fs.String("include", "", "")
	fs.String("debug-path", "", "")
	fs.Bool("no-build-id-check", false, "")
	fs.String("ringbuf-size", "16MiB", "")
	fs.Bool("detach", false, "")
	fs.Bool("verbose", false, "")
	fs.Bool("report", true, "")
	fs.Bool("status", true, "")
	fs.String("scope", "binary", "")
	fs.Bool("userspace-bpf", false, "")
	fs.Bool("skip-preflight", false, "")
	fs.String("log-level", "info", "")

	return fs
}

func TestForwardedFlagArgs(t *testing.T) {
	tests := []struct {
		name string
		set  func(fs *pflag.FlagSet)
		want []string
	}{
		{
			name: "only explicitly set flags are forwarded",
			set: func(fs *pflag.FlagSet) {
				require.NoError(t, fs.Set("path", "/bin/true"))
			},
			want: []string{"--path=/bin/true"},
		},
		{
			name: "detach is never forwarded even when set",
			set: func(fs *pflag.FlagSet) {
				require.NoError(t, fs.Set("path", "/bin/true"))
				require.NoError(t, fs.Set("detach", "true"))
			},
			want: []string{"--path=/bin/true"},
		},
		{
			name: "boolean flags are forwarded as true/false",
			set: func(fs *pflag.FlagSet) {
				require.NoError(t, fs.Set("path", "/bin/true"))
				require.NoError(t, fs.Set("report", "false"))
				require.NoError(t, fs.Set("no-build-id-check", "true"))
				require.NoError(t, fs.Set("skip-preflight", "true"))
			},
			want: []string{"--no-build-id-check=true", "--path=/bin/true", "--report=false", "--skip-preflight=true"},
		},
		{
			name: "pid and log-level are forwarded like any other flag",
			set: func(fs *pflag.FlagSet) {
				require.NoError(t, fs.Set("path", "/bin/true"))
				require.NoError(t, fs.Set("pid", "1234"))
				require.NoError(t, fs.Set("log-level", "debug"))
			},
			want: []string{"--log-level=debug", "--path=/bin/true", "--pid=1234"},
		},
		{
			name: "ringbuf-size is forwarded as typed",
			set: func(fs *pflag.FlagSet) {
				require.NoError(t, fs.Set("path", "/bin/true"))
				require.NoError(t, fs.Set("ringbuf-size", "64KiB"))
			},
			want: []string{"--path=/bin/true", "--ringbuf-size=64KiB"},
		},
		{
			name: "unset flags are not forwarded",
			set:  func(fs *pflag.FlagSet) {},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newRunFlagSet()
			tt.set(fs)

			got := forwardedFlagArgs(fs, daemonizeSkipFlags)
			require.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestDaemonArgs(t *testing.T) {
	tests := []struct {
		name string
		set  map[string]string
		want []string
	}{
		{
			name: "skip-preflight unset is forced on for the child",
			set:  map[string]string{"path": "/bin/true", "detach": "true"},
			want: []string{"--path=/bin/true", "--skip-preflight=true"},
		},
		{
			name: "explicit true is forwarded once",
			set:  map[string]string{"skip-preflight": "true"},
			want: []string{"--skip-preflight=true"},
		},
		{
			name: "explicit false is respected",
			set:  map[string]string{"skip-preflight": "false"},
			want: []string{"--skip-preflight=false"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newRunFlagSet()
			for k, v := range tt.set {
				require.NoError(t, fs.Set(k, v))
			}
			require.ElementsMatch(t, tt.want, daemonArgs(fs))
		})
	}
}

// withTempDaemonFiles points the PID and log file settings at per-test paths
// so daemonize never touches a real daemon's files.
func withTempDaemonFiles(t *testing.T) (pidFile, logFile string) {
	t.Helper()

	origPid, origLog := settings.PidFile, settings.LogFile
	dir := t.TempDir()
	settings.PidFile = filepath.Join(dir, "xcover.pid")
	settings.LogFile = filepath.Join(dir, "xcover.log")
	t.Cleanup(func() {
		settings.PidFile = origPid
		settings.LogFile = origLog
	})

	return settings.PidFile, settings.LogFile
}

// fakeExec records the command daemonize asked for and returns cmd in its
// place. It restores the real exec seam when the test ends.
type fakeExec struct {
	calls int
	name  string
	args  []string
	cmd   *exec.Cmd
}

func swapExecCommand(t *testing.T, cmd *exec.Cmd) *fakeExec {
	t.Helper()

	f := &fakeExec{cmd: cmd}
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		f.calls++
		f.name = name
		f.args = args
		return f.cmd
	}
	t.Cleanup(func() { execCommand = orig })

	return f
}

// newDaemonizeCommand attaches the run flag set to a cobra command and
// parses args, so daemonize sees flags the way a user invocation sets them.
func newDaemonizeCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: CmdName}
	cmd.Flags().AddFlagSet(newRunFlagSet())
	require.NoError(t, cmd.ParseFlags(args))

	return cmd
}

func TestDaemonize(t *testing.T) {
	pidFile, logFile := withTempDaemonFiles(t)

	fake := swapExecCommand(t, exec.Command("true"))

	o := newTestOptions(t)
	o.skipPreflight = true
	o.pid = os.Getpid()
	cmd := newDaemonizeCommand(t,
		"--detach",
		"--skip-preflight=true",
		"--pid="+strconv.Itoa(os.Getpid()),
		"--verbose",
	)

	require.NoError(t, o.daemonize(cmd))

	require.Equal(t, 1, fake.calls)
	require.Equal(t, os.Args[0], fake.name)
	require.NotEmpty(t, fake.args)
	require.Equal(t, "run", fake.args[0])
	require.Contains(t, fake.args, "--verbose=true")
	require.Contains(t, fake.args, "--skip-preflight=true")
	require.Contains(t, fake.args, "--pid="+strconv.Itoa(os.Getpid()))
	for _, a := range fake.args {
		require.NotContains(t, a, "--detach")
	}

	require.NotNil(t, fake.cmd.Process)
	pid, err := common.ReadPID()
	require.NoError(t, err)
	require.Equal(t, fake.cmd.Process.Pid, pid)
	_, err = os.Stat(pidFile)
	require.NoError(t, err)
	_, err = os.Stat(logFile)
	require.NoError(t, err)

	// Reap the child so no process outlives the test.
	require.NoError(t, fake.cmd.Wait())
}

func TestDaemonize_AlreadyRunning(t *testing.T) {
	withTempDaemonFiles(t)
	require.NoError(t, common.WritePID(os.Getpid()))

	fake := swapExecCommand(t, exec.Command("true"))

	o := newTestOptions(t)
	o.skipPreflight = true
	cmd := newDaemonizeCommand(t, "--detach", "--skip-preflight=true")

	require.NoError(t, o.daemonize(cmd))
	require.Equal(t, 0, fake.calls)
}

func TestDaemonize_PreForkErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(o *Options)
		skip  func() bool
	}{
		{
			name:  "invalid pid",
			setup: func(o *Options) { o.pid = 0 },
		},
		{
			name:  "invalid symbol pattern",
			setup: func(o *Options) { o.symIncludePattern = "(" },
		},
		{
			// Preflight rejects a process without BPF capabilities. Root
			// passes it, so the case only runs unprivileged.
			name:  "preflight failure",
			setup: func(o *Options) { o.skipPreflight = false },
			skip:  func() bool { return os.Geteuid() == 0 },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip != nil && tt.skip() {
				t.Skip("requires an unprivileged test process")
			}
			pidFile, _ := withTempDaemonFiles(t)
			fake := swapExecCommand(t, exec.Command("true"))

			o := newTestOptions(t)
			o.skipPreflight = true
			tt.setup(o)
			cmd := newDaemonizeCommand(t, "--detach")

			require.Error(t, o.daemonize(cmd))
			require.Equal(t, 0, fake.calls)
			_, err := os.Stat(pidFile)
			require.True(t, os.IsNotExist(err))
		})
	}
}

func TestDaemonize_StartFailure(t *testing.T) {
	pidFile, _ := withTempDaemonFiles(t)
	fake := swapExecCommand(t, exec.Command("/nonexistent/binary"))

	o := newTestOptions(t)
	o.skipPreflight = true
	cmd := newDaemonizeCommand(t, "--detach", "--skip-preflight=true")

	require.Error(t, o.daemonize(cmd))
	require.Equal(t, 1, fake.calls)
	_, err := os.Stat(pidFile)
	require.True(t, os.IsNotExist(err))
}

func TestRun_DetachRoutesToDaemonize(t *testing.T) {
	withTempDaemonFiles(t)
	fake := swapExecCommand(t, exec.Command("true"))

	o := newTestOptions(t)
	o.detach = true
	o.skipPreflight = true
	cmd := newDaemonizeCommand(t, "--detach", "--skip-preflight=true")

	require.NoError(t, o.Run(cmd, nil))
	require.Equal(t, 1, fake.calls)
	require.NoError(t, fake.cmd.Wait())
}

func TestDaemonize_LogFileOpenFailure(t *testing.T) {
	pidFile, _ := withTempDaemonFiles(t)
	// A directory cannot be opened for writing, so the log file open fails.
	settings.LogFile = t.TempDir()
	fake := swapExecCommand(t, exec.Command("true"))

	o := newTestOptions(t)
	o.skipPreflight = true
	cmd := newDaemonizeCommand(t, "--detach", "--skip-preflight=true")

	require.Error(t, o.daemonize(cmd))
	require.Equal(t, 1, fake.calls)
	require.Nil(t, fake.cmd.Process)
	_, err := os.Stat(pidFile)
	require.True(t, os.IsNotExist(err))
}
