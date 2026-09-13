package run

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	log "github.com/rs/zerolog"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/options"
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
		wantScope trace.Scope
		wantErr   bool
	}{
		{
			name:      "binary scope",
			scope:     string(trace.ScopeBinary),
			wantScope: trace.ScopeBinary,
		},
		{
			name:      "project scope",
			scope:     string(trace.ScopeProject),
			wantScope: trace.ScopeProject,
		},
		{
			name:    "unknown scope",
			scope:   "bogus",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := newTestOptions(t)
			o.scope = tt.scope

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
