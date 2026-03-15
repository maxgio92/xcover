//go:build integration

package trace

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

var (
	integrationTestBinary       = "testdata/gotest"
	integrationTestExcludedSyms = "^runtime.text$|^internal/cpu.Initialize$"
)

// TestUserTracee_Init verifies end-to-end function resolution against the
// testdata binary, and that a nonexistent path surfaces os.ErrNotExist.
func TestUserTracee_Init(t *testing.T) {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	tracee := NewUserTracee(
		WithTraceeExePath(integrationTestBinary),
		WithTraceeLogger(logger),
		WithTraceeSymPatternExclude(integrationTestExcludedSyms),
	)
	err := tracee.Init(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, tracee.GetFuncNames())
	require.NotEmpty(t, tracee.GetFuncOffsets())
	require.NotEmpty(t, tracee.GetFuncCookies())

	tracee = NewUserTracee(
		WithTraceeExePath("nonexistent-binary-file"),
		WithTraceeLogger(logger),
		WithTraceeSymPatternExclude(integrationTestExcludedSyms),
	)
	err = tracee.Init(t.Context())
	require.Error(t, err)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestUserTracee_Init_NoMatch verifies that Init returns ErrNoFunctionSymbols
// when the include pattern matches nothing in the binary.
func TestUserTracee_Init_NoMatch(t *testing.T) {
	tracee := NewUserTracee(
		WithTraceeExePath(integrationTestBinary),
		WithTraceeSymPatternInclude("^nonexistentSymbol$"),
	)
	err := tracee.Init(t.Context())
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoFunctionSymbols)
}
