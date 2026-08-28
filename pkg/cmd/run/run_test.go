package run

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	log "github.com/rs/zerolog"
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
