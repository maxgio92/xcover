package trace_test

import (
	"os"
	"path"
	"testing"

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
