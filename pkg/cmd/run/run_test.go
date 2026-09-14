package run

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	log "github.com/rs/zerolog"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/options"
	"github.com/maxgio92/xcover/pkg/trace"
)

func newTestOptions(t *testing.T) *Options {
	t.Helper()

	logger := log.New(log.ConsoleWriter{Out: os.Stderr})
	o := new(Options)
	// Mirror the --pid flag default; the zero value is rejected by setup().
	o.pid = -1
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
		wantScope    trace.Scope
		wantErr      bool
	}{
		{
			name:      "binary scope",
			scope:     string(trace.ScopeBinary),
			pid:       -1,
			wantScope: trace.ScopeBinary,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := newTestOptions(t)
			o.scope = tt.scope
			o.pid = tt.pid
			o.userspaceBPF = tt.userspaceBPF

			scope, err := o.setup()

			// setup() always writes the PID file before parsing anything, so
			// the caller can unconditionally defer its removal.
			_, statErr := os.Stat(settings.PidFile)
			require.NoError(t, statErr)
			t.Cleanup(func() { _ = os.Remove(settings.PidFile) })

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantScope, scope)
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
	require.ErrorContains(t, err, "not a process (thread-group leader) PID")
	require.NoError(t, validatePID(os.Getpid(), false))
	require.ErrorContains(t, validatePID(1<<22+1, false), "no such process")
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
