package trace

import (
	"debug/elf"
	"testing"

	"github.com/stretchr/testify/require"
)

func globalFunc(name string) elf.Symbol {
	return elf.Symbol{
		Name: name,
		Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC),
	}
}

func mustSymFilter(t *testing.T, include, exclude string) symFilter {
	t.Helper()
	f, err := newSymFilter(include, exclude, nil, nil)
	require.NoError(t, err)
	return f
}

func TestShouldInclude(t *testing.T) {
	fooSym := globalFunc("main.fooFunction")
	require.True(t, mustSymFilter(t, "^main.fooFunction$", "").shouldInclude(fooSym))

	runtimeSym := globalFunc("runtime.sched")
	require.True(t, mustSymFilter(t, "", "").shouldInclude(runtimeSym))
	require.False(t, mustSymFilter(t, "", "^runtime.").shouldInclude(runtimeSym))

	// Exclude wins over include.
	require.False(t, mustSymFilter(t, "^runtime.", "sched$").shouldInclude(runtimeSym))
}

func TestShouldInclude_Binding(t *testing.T) {
	local := elf.Symbol{Name: "helper", Info: elf.ST_INFO(elf.STB_LOCAL, elf.STT_FUNC)}
	global := globalFunc("api")

	f, err := newSymFilter("", "", nil, []elf.SymBind{elf.STB_LOCAL})
	require.NoError(t, err)
	require.False(t, f.shouldInclude(local))
	require.True(t, f.shouldInclude(global))

	// A binding include short-circuits the name patterns.
	f, err = newSymFilter("^nomatch$", "", []elf.SymBind{elf.STB_GLOBAL}, nil)
	require.NoError(t, err)
	require.True(t, f.shouldInclude(global))
	require.False(t, f.shouldInclude(local))
}

func TestNewSymFilter_InvalidPattern(t *testing.T) {
	_, err := newSymFilter("(", "", nil, nil)
	require.ErrorContains(t, err, "invalid include pattern")

	_, err = newSymFilter("", "[", nil, nil)
	require.ErrorContains(t, err, "invalid exclude pattern")
}
