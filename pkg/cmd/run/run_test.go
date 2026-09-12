package run

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	log "github.com/rs/zerolog"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/options"
	"github.com/maxgio92/xcover/pkg/probe"
	"github.com/maxgio92/xcover/pkg/trace"
)

func newTestOptions(t *testing.T) *Options {
	t.Helper()

	logger := log.New(log.ConsoleWriter{Out: os.Stderr})
	o := new(Options)
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
		name      string
		scope     string
		pid       int
		wantScope trace.Scope
		wantErr   bool
	}{
		{
			name:      "binary scope",
			scope:     string(trace.ScopeBinary),
			pid:       probe.PIDAll,
			wantScope: trace.ScopeBinary,
		},
		{
			name:      "project scope",
			scope:     string(trace.ScopeProject),
			pid:       probe.PIDAll,
			wantScope: trace.ScopeProject,
		},
		{
			name:    "unknown scope",
			scope:   "bogus",
			pid:     probe.PIDAll,
			wantErr: true,
		},
		{
			name:      "positive pid",
			scope:     string(trace.ScopeBinary),
			pid:       os.Getpid(),
			wantScope: trace.ScopeBinary,
		},
		{
			// libbpf resolves pid 0 to xcover's own process, which never maps
			// the traced executable, so it must be rejected instead of
			// producing an empty report.
			name:    "zero pid",
			scope:   string(trace.ScopeBinary),
			pid:     0,
			wantErr: true,
		},
		{
			// libbpf treats every negative pid as "all processes"; only
			// PIDAll is documented, so other negatives are rejected.
			name:    "negative pid other than PIDAll",
			scope:   string(trace.ScopeBinary),
			pid:     -7,
			wantErr: true,
		},
		{
			name:      "largest C int pid",
			scope:     string(trace.ScopeBinary),
			pid:       math.MaxInt32,
			wantScope: trace.ScopeBinary,
		},
		{
			// libbpfgo narrows the pid to a C int, so 4294967295 would wrap
			// to -1 and silently trace every process.
			name:    "pid above MaxInt32",
			scope:   string(trace.ScopeBinary),
			pid:     math.MaxInt32 + 1,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := newTestOptions(t)
			o.scope = tt.scope
			o.pid = tt.pid

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

func TestOptionsBuildTracer(t *testing.T) {
	o := newTestOptions(t)
	o.comm = "/bin/true"

	tracer := o.buildTracer(trace.ScopeBinary)

	require.NotNil(t, tracer)
}

func TestForwardedFlagArgs(t *testing.T) {
	newFlagSet := func() *pflag.FlagSet {
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
		fs.String("log-level", "info", "")

		return fs
	}

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
			},
			want: []string{"--no-build-id-check=true", "--path=/bin/true", "--report=false"},
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
			fs := newFlagSet()
			tt.set(fs)

			got := forwardedFlagArgs(fs, daemonizeSkipFlags)
			require.ElementsMatch(t, tt.want, got)
		})
	}
}
