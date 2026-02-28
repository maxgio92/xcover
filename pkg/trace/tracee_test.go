package trace_test

import (
	"debug/elf"
	"os"
	"path"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/trace"
)

var (
	testData         = "testdata"
	testBinary       = path.Join(testData, "gotest")
	testExcludedSyms = "^runtime.text$|^internal/cpu.Initialize$"
	testLogger       = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
)

func TestNewUserTracee_Defaults(t *testing.T) {
	tracee := trace.NewUserTracee()
	require.NotNil(t, tracee)
	require.NotNil(t, tracee.UserTraceeOptions)
}

func TestUserTracee_Validate(t *testing.T) {
	tracee := trace.NewUserTracee()
	err := tracee.Init()
	require.Error(t, err)
	require.Contains(t, err.Error(), "exe path is empty")
	require.ErrorIs(t, err, trace.ErrExePathEmpty)
}

func TestUserTracee_IncludeExclude(t *testing.T) {
	tracee := trace.NewUserTracee(
		trace.WithTraceeSymPatternInclude("^main.fooFunction$"),
	)

	sym := elf.Symbol{
		Name: "main.fooFunction",
		Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC),
	}

	include := tracee.ShouldIncludeSymbol(sym)
	require.True(t, include)

	sym = elf.Symbol{
		Name: "runtime.sched",
		Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC),
	}

	tracee = trace.NewUserTracee()
	require.True(t, tracee.ShouldIncludeSymbol(sym))

	tracee = trace.NewUserTracee(
		trace.WithTraceeSymPatternExclude("^runtime."),
	)
	require.False(t, tracee.ShouldIncludeSymbol(sym))
}
