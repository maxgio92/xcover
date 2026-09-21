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
	// Mirror the --pid, --ringbuf-size and --scope flag defaults; the zero
	// values are rejected by validate().
	o.pid = -1
	o.ringBufSize = "16MiB"
	o.scope = string(trace.ScopeBinary)
	o.Options = options.NewOptions(
		options.WithContext(context.Background()),
		options.WithLogger(logger),
		options.WithLogLevel(log.LevelInfoValue),
	)

	return o
}

func TestOptionsValidate(t *testing.T) {
	// Pin the bpftime segment size so the developer's shell environment
	// cannot flip the userspace ring buffer rows.
	t.Setenv("BPFTIME_SHM_MEMORY_MB", "50")
	t.Setenv("BPFTIME_GLOBAL_SHM_NAME", "xcover-test-absent-segment")

	tests := []struct {
		name            string
		scope           string
		pid             int
		userspaceBPF    bool
		include         string
		exclude         string
		ringBufSize     string // empty keeps the flag default
		wantScope       trace.Scope
		wantErr         bool
		wantErrIs       error
		wantErrContains string
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
		{
			// Both run paths refuse before any daemon starts: validate runs
			// in the foreground run and in the detached parent.
			name:            "userspace 32MiB does not fit the segment",
			scope:           string(trace.ScopeBinary),
			pid:             -1,
			userspaceBPF:    true,
			ringBufSize:     "32MiB",
			wantErr:         true,
			wantErrContains: "BPFTIME_SHM_MEMORY_MB",
		},
		{
			// The checks run in flag order, so --pid is reported first.
			name:            "invalid pid before invalid scope",
			scope:           "bogus",
			pid:             0,
			wantErr:         true,
			wantErrContains: "invalid --pid 0",
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

			scope, err := o.validate()

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrIs != nil {
					require.ErrorIs(t, err, tt.wantErrIs)
				}
				if tt.wantErrContains != "" {
					require.ErrorContains(t, err, tt.wantErrContains)
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

// segmentInfo is the os.FileInfo of a regular file with the given size.
type segmentInfo struct {
	os.FileInfo
	size int64
}

func (s segmentInfo) Size() int64       { return s.size }
func (s segmentInfo) Mode() os.FileMode { return 0o644 }

func TestValidateUserspaceRingBufSize(t *testing.T) {
	env := func(kv map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) {
			v, ok := kv[k]
			return v, ok
		}
	}
	unset := env(nil)
	set := func(v string) func(string) (string, bool) {
		return env(map[string]string{"BPFTIME_SHM_MEMORY_MB": v})
	}
	noSegment := func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	segment := func(size int64) func(string) (os.FileInfo, error) {
		return func(string) (os.FileInfo, error) { return segmentInfo{size: size}, nil }
	}
	defaultPath := "/dev/shm/bpftime_maps_shm"
	// exact is the byte count bpftime allocates for a 16MiB ring buffer.
	exact := int64(2*(16<<20) + 2*os.Getpagesize())

	tests := []struct {
		name     string
		size     uint32
		env      func(string) (string, bool)
		stat     func(string) (os.FileInfo, error)
		wantPath string
		wantErr  []string
	}{
		{"16MiB unset", 16 << 20, unset, noSegment, "", nil},
		{"32MiB unset", 32 << 20, unset, noSegment, "", []string{"32MiB", "50 MiB", "BPFTIME_SHM_MEMORY_MB", "applies only when " + defaultPath + " is created"}},
		{"32MiB with 128", 32 << 20, set("128"), noSegment, "", nil},
		// 16MiB needs twice its size plus two pages, so a 32 MiB segment is
		// two pages short and 33 is the next whole MiB that fits.
		{"16MiB with 32 is too small", 16 << 20, set("32"), noSegment, "", []string{"16MiB", "32 MiB"}},
		{"16MiB with 33 fits", 16 << 20, set("33"), noSegment, "", nil},
		{"1MiB with 0 clamps to 1", 1 << 20, set("0"), noSegment, "", []string{"1MiB", "1 MiB"}},
		{"2GiB fits under a large value", 1 << 31, set("99999"), noSegment, "", nil},
		{"32MiB with abc falls back", 32 << 20, set("abc"), noSegment, "", []string{"50 MiB"}},
		{"16MiB with abc falls back", 16 << 20, set("abc"), noSegment, "", nil},
		{"32MiB with empty falls back", 32 << 20, set(""), noSegment, "", []string{"50 MiB"}},
		{"16MiB with empty falls back", 16 << 20, set(""), noSegment, "", nil},
		{"leading space and trailing text still parse", 32 << 20, set(" 128MB"), noSegment, "", nil},
		{"32MiB with overflow falls back", 32 << 20, set("99999999999999999999"), noSegment, "", []string{"50 MiB"}},
		// An existing segment keeps its size: the env value is ignored in both
		// directions.
		{"32MiB with 128 but a 50MiB segment", 32 << 20, set("128"), segment(50 << 20), defaultPath, []string{"32MiB", "50 MiB", "existing segment " + defaultPath, "Stop any running xcover", "remove it"}},
		{"32MiB with 50 but a 128MiB segment", 32 << 20, set("50"), segment(128 << 20), defaultPath, nil},
		{"32MiB unset but a 128MiB segment", 32 << 20, unset, segment(128 << 20), defaultPath, nil},
		{"16MiB with 128 but a 20MiB segment", 16 << 20, set("128"), segment(20 << 20), defaultPath, []string{"20 MiB", "existing segment"}},
		// The rule is strict: a segment of exactly the needed size fails.
		{"16MiB with an exact segment", 16 << 20, unset, segment(exact), defaultPath, []string{"16MiB", strconv.FormatInt(exact, 10) + " bytes", "existing segment"}},
		{"16MiB with an exact segment plus one byte", 16 << 20, unset, segment(exact + 1), defaultPath, nil},
		{"32MiB with a renamed 128MiB segment", 32 << 20, env(map[string]string{"BPFTIME_GLOBAL_SHM_NAME": "custom"}), segment(128 << 20), "/dev/shm/custom", nil},
		{"32MiB with stat error falls back to env", 32 << 20, set("128"), func(string) (os.FileInfo, error) { return nil, os.ErrPermission }, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			stat := func(path string) (os.FileInfo, error) {
				gotPath = path
				return tt.stat(path)
			}
			err := validateUserspaceRingBufSize(tt.size, tt.env, stat)
			if tt.wantPath != "" {
				require.Equal(t, tt.wantPath, gotPath)
			}
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			for _, want := range tt.wantErr {
				require.ErrorContains(t, err, want)
			}
		})
	}
}

func TestBpftimeShmMemoryMiB(t *testing.T) {
	set := func(v string) func(string) (string, bool) {
		return func(string) (string, bool) { return v, true }
	}
	unset := func(string) (string, bool) { return "", false }

	// Each row mirrors what std::stoi does with the same text.
	tests := []struct {
		name string
		env  func(string) (string, bool)
		want int
	}{
		{"unset", unset, 50},
		{"empty", set(""), 50},
		{"sign only", set("+"), 50},
		{"negative zero clamps to min", set("-0"), 1},
		{"negative clamps to min", set("-5"), 1},
		{"hex prefix parses zero", set("0x10"), 1},
		{"exponent stops at e", set("1e3"), 1},
		{"leading space", set(" 64"), 64},
		{"trailing text", set("64abc"), 64},
		{"int overflow falls back", set("99999999999"), 50},
		{"clamps to max", set("20000"), 10240},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, bpftimeShmMemoryMiB(tt.env))
		})
	}
}

// TestOptionsSetup proves setup replaces a PID file naming another process
// with this process's PID.
func TestOptionsSetup(t *testing.T) {
	pidFile, _ := withTempDaemonFiles(t)
	require.NoError(t, os.WriteFile(pidFile, []byte("1"), 0644))

	o := newTestOptions(t)
	o.setup()

	got, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	require.Equal(t, strconv.Itoa(os.Getpid()), string(got))
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
	o.setup()

	got, err := os.ReadFile(settings.PidFile)
	require.NoError(t, err)
	require.Equal(t, want, string(got))

	after, err := os.Stat(settings.PidFile)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after), "setup replaced the PID file")
	require.Equal(t, before.ModTime(), after.ModTime(), "setup wrote the PID file")
}

// TestOptionsRefuseIfDaemonRunning_SelfPID proves the guard lets the detached
// child through when the PID file already names it, and leaves the file
// untouched: no truncate and no rename.
func TestOptionsRefuseIfDaemonRunning_SelfPID(t *testing.T) {
	pidFile, _ := withTempDaemonFiles(t)

	want := strconv.Itoa(os.Getpid())
	require.NoError(t, os.WriteFile(pidFile, []byte(want), 0644))
	// Age the file so any write, in place or by rename, moves its mtime.
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(pidFile, old, old))
	before, err := os.Stat(pidFile)
	require.NoError(t, err)

	o := newTestOptions(t)
	require.NoError(t, o.refuseIfDaemonRunning())

	got, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	require.Equal(t, want, string(got))

	after, err := os.Stat(pidFile)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after), "guard replaced the PID file")
	require.Equal(t, before.ModTime(), after.ModTime(), "guard wrote the PID file")
}

// TestOptionsRefuseIfDaemonRunning proves the guard refuses only a PID file
// naming another live process. Every other state falls through to setup.
func TestOptionsRefuseIfDaemonRunning(t *testing.T) {
	tests := []struct {
		name    string
		content string // empty writes no PID file
		wantErr string
	}{
		{
			name: "no pid file",
		},
		{
			// Above pid_max on every Linux configuration, so never alive.
			name:    "stale pid",
			content: strconv.Itoa(1<<22 + 1),
		},
		{
			name:    "invalid content",
			content: "abc",
		},
		{
			// PID 1 counts alive for every caller: kill succeeds as root and
			// fails with EPERM unprivileged, which processAlive treats as alive.
			name:    "live other pid",
			content: "1",
			wantErr: "Daemon already running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pidFile, _ := withTempDaemonFiles(t)
			if tt.content != "" {
				require.NoError(t, os.WriteFile(pidFile, []byte(tt.content), 0644))
			}

			o := newTestOptions(t)
			err := o.refuseIfDaemonRunning()

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
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

// TestOptionsDaemonizeRefusesUserspaceRingBufSize proves the parent refuses
// a --ringbuf-size the bpftime segment cannot hold before forking, and that a
// size that fits passes the check.
func TestOptionsDaemonizeRefusesUserspaceRingBufSize(t *testing.T) {
	origPidFile := settings.PidFile
	settings.PidFile = filepath.Join(t.TempDir(), "xcover.pid")
	t.Cleanup(func() { settings.PidFile = origPidFile })
	t.Setenv("BPFTIME_SHM_MEMORY_MB", "50")
	t.Setenv("BPFTIME_GLOBAL_SHM_NAME", "xcover-test-absent-segment")

	t.Run("32MiB refused", func(t *testing.T) {
		o := newTestOptions(t)
		o.userspaceBPF = true
		o.ringBufSize = "32MiB"

		err := o.daemonize(&cobra.Command{})
		require.ErrorContains(t, err, "BPFTIME_SHM_MEMORY_MB")

		_, err = os.Stat(settings.PidFile)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("16MiB passes", func(t *testing.T) {
		o := newTestOptions(t)
		o.userspaceBPF = true
		o.ringBufSize = "16MiB"
		// Stop daemonize at the next check so nothing starts.
		o.symIncludePattern = "("

		err := o.daemonize(&cobra.Command{})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "BPFTIME_SHM_MEMORY_MB")
	})

	t.Run("32MiB kernel mode unchecked", func(t *testing.T) {
		o := newTestOptions(t)
		o.ringBufSize = "32MiB"
		o.symIncludePattern = "("

		err := o.daemonize(&cobra.Command{})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "BPFTIME_SHM_MEMORY_MB")
	})
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

// withTempDaemonFiles points the PID, log and socket path settings at
// per-test paths so neither daemonize nor a run that reaches the tracer
// touches a real daemon's files.
func withTempDaemonFiles(t *testing.T) (pidFile, logFile string) {
	t.Helper()

	origPid, origLog, origSock := settings.PidFile, settings.LogFile, trace.HealthCheckSockPath
	dir := t.TempDir()
	settings.PidFile = filepath.Join(dir, "xcover.pid")
	settings.LogFile = filepath.Join(dir, "xcover.log")
	trace.HealthCheckSockPath = filepath.Join(dir, "xcover.sock")
	t.Cleanup(func() {
		settings.PidFile = origPid
		settings.LogFile = origLog
		trace.HealthCheckSockPath = origSock
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
		name    string
		setup   func(o *Options)
		skip    func() bool
		wantErr string // empty accepts any error
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
			name:    "invalid scope",
			setup:   func(o *Options) { o.scope = "bogus" },
			wantErr: `unknown scope "bogus"`,
		},
		{
			// The parent checks the flags in the same order as the foreground
			// run, so --pid is reported first.
			name:    "invalid pid before invalid scope",
			setup:   func(o *Options) { o.pid = 0; o.scope = "bogus" },
			wantErr: "invalid --pid 0",
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

			err := o.daemonize(cmd)
			require.Error(t, err)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			}
			require.Equal(t, 0, fake.calls)
			_, err = os.Stat(pidFile)
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

// TestRun_RefusesWhenDaemonRunning proves a foreground run refuses to start
// while the PID file names another live process, and that the refusal leaves
// the file in place: RemovePID is armed only after the guard passes. The
// second case proves a bad flag is reported before the guard.
func TestRun_RefusesWhenDaemonRunning(t *testing.T) {
	tests := []struct {
		name    string
		scope   string // empty keeps the flag default
		wantErr string
	}{
		{
			name:    "live daemon",
			wantErr: "Daemon already running",
		},
		{
			name:    "bad flag before the guard",
			scope:   "bogus",
			wantErr: `unknown scope "bogus"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pidFile, _ := withTempDaemonFiles(t)
			require.NoError(t, os.WriteFile(pidFile, []byte("1"), 0644))

			o := newTestOptions(t)
			o.skipPreflight = true
			o.comm = "/bin/true"
			if tt.scope != "" {
				o.scope = tt.scope
			}

			// Run reads cmd only under --detach, so an empty command suffices.
			err := o.Run(&cobra.Command{}, nil)
			require.ErrorContains(t, err, tt.wantErr)

			got, err := os.ReadFile(pidFile)
			require.NoError(t, err)
			require.Equal(t, "1", string(got))
		})
	}
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
