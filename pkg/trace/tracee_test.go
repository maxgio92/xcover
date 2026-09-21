package trace_test

import (
	"bytes"
	"context"
	"debug/elf"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/trace"
)

//nolint:unused // consumed by integration-tagged tests in this package (e.g. resolver_integration_test.go)
var (
	testData         = "testdata"
	testBinary       = path.Join(testData, "gotest")
	testExcludedSyms = "^runtime.text$|^internal/cpu.Initialize$"
)

var testLogger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

func TestNewUserTracee_Defaults(t *testing.T) {
	tracee := trace.NewUserTracee()
	require.NotNil(t, tracee)
	require.NotNil(t, tracee.UserTraceeOptions)
}

func TestUserTracee_Validate(t *testing.T) {
	tracee := trace.NewUserTracee()
	err := tracee.Init(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "exe path is empty")
	require.ErrorIs(t, err, trace.ErrExePathEmpty)
}

// TestUserTracee_Init_RecoveryRefusesFilters drives Init into the stripped
// binary fallback through a resolver stub that fails like SymbolTableResolver
// does on a binary with neither .symtab nor .gopclntab. Recovery names
// functions func_0x<addr>, so any name or bind filter must make Init refuse
// with ErrFilterNeedsSymbols instead of silently ignoring the filter. Without
// a filter the fallback runs as before and logs that it is doing so.
//
// The unfiltered cases point exePath at a file that is not an ELF so the
// recovery step fails deterministically: RecoveryResolver succeeds on
// testdata/gotest, whose prologues resurgo recovers without .eh_frame.
func TestUserTracee_Init_RecoveryRefusesFilters(t *testing.T) {
	const fallbackMsg = "falling back to binary recovery"

	noSymbolTable := func(context.Context) ([]trace.FunctionEntry, error) {
		return nil, errors.Wrap(trace.ErrNoSymbolTable, "no symbol table")
	}

	notELF := filepath.Join(t.TempDir(), "not-an-elf")
	require.NoError(t, os.WriteFile(notELF, []byte("not an ELF binary\n"), 0o600))

	tests := []struct {
		name    string
		exePath string
		opts    []trace.UserTraceeOption
		refuses bool
	}{
		{
			name:    "include pattern",
			exePath: testBinary,
			opts:    []trace.UserTraceeOption{trace.WithTraceeSymPatternInclude("^main$")},
			refuses: true,
		},
		{
			name:    "exclude pattern",
			exePath: testBinary,
			opts:    []trace.UserTraceeOption{trace.WithTraceeSymPatternExclude("^main$")},
			refuses: true,
		},
		{
			name:    "bind include",
			exePath: testBinary,
			opts:    []trace.UserTraceeOption{trace.WithTraceeSymBindInclude(elf.STB_GLOBAL)},
			refuses: true,
		},
		{
			name:    "bind exclude",
			exePath: testBinary,
			opts:    []trace.UserTraceeOption{trace.WithTraceeSymBindExclude(elf.STB_LOCAL)},
			refuses: true,
		},
		{
			name:    "no filter",
			exePath: notELF,
		},
		{
			name:    "empty bind include",
			exePath: notELF,
			opts:    []trace.UserTraceeOption{trace.WithTraceeSymBindInclude()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := append([]trace.UserTraceeOption{
				trace.WithTraceeExePath(tt.exePath),
				trace.WithTraceeResolver(noSymbolTable),
				trace.WithTraceeLogger(zerolog.New(&buf)),
			}, tt.opts...)

			err := trace.NewUserTracee(opts...).Init(t.Context())
			require.Error(t, err)
			require.Contains(t, err.Error(), "failed to resolve functions")

			if tt.refuses {
				require.ErrorIs(t, err, trace.ErrFilterNeedsSymbols)
				require.NotContains(t, buf.String(), fallbackMsg)
				return
			}
			require.NotErrorIs(t, err, trace.ErrFilterNeedsSymbols)
			require.Contains(t, buf.String(), fallbackMsg)
		})
	}
}
