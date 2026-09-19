package trace

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// TestUserTracee_Init_Demangled checks that Init keeps a resolver's demangled
// name and falls back to the raw name when the resolver leaves it empty, as
// custom resolvers injected with WithTraceeResolver may do.
func TestUserTracee_Init_Demangled(t *testing.T) {
	resolver := func(context.Context) ([]FunctionEntry, error) {
		return []FunctionEntry{
			{Name: "custom.Alpha", Offset: 0x1000},
			{Name: "_ZN3app3net5parseEi", Demangled: "app::net::parse(int)", Offset: 0x2000},
		}, nil
	}
	tracee := NewUserTracee(
		WithTraceeExePath("dummy-path"),
		WithTraceeLogger(zerolog.Nop()),
		WithTraceeResolver(resolver),
	)
	require.NoError(t, tracee.Init(t.Context()))

	require.Len(t, tracee.funcs, 2)
	require.Equal(t, "custom.Alpha", tracee.funcs[cookie(0x1000)].name)
	require.Equal(t, "custom.Alpha", tracee.funcs[cookie(0x1000)].demangled)
	require.Equal(t, "_ZN3app3net5parseEi", tracee.funcs[cookie(0x2000)].name)
	require.Equal(t, "app::net::parse(int)", tracee.funcs[cookie(0x2000)].demangled)
}
