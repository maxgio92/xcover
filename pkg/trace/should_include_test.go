package trace

import (
	"debug/elf"
	"testing"

	"github.com/stretchr/testify/require"
)

func globalFunc(name string) funcSym {
	return newFuncSym(elf.Symbol{
		Name: name,
		Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC),
	})
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
}

// TestShouldInclude_Demangled checks that name patterns match the demangled
// C++ name as well as the raw one, and that exclude still wins.
func TestShouldInclude_Demangled(t *testing.T) {
	parseInt := globalFunc("_ZN3app3net5parseEi")
	parseStr := globalFunc("_ZN3app3net5parseEPKc")
	connOpen := globalFunc("_ZN3app3net4Conn4openEv")
	cEntry := globalFunc("c_entry")
	helper := globalFunc("_ZL13helper_statici")

	ns := mustSymFilter(t, "^app::net::", "")
	for _, sym := range []funcSym{parseInt, parseStr, connOpen} {
		require.Truef(t, ns.shouldInclude(sym), "%s should match the namespace", sym.Name)
	}
	for _, sym := range []funcSym{cEntry, helper} {
		require.Falsef(t, ns.shouldInclude(sym), "%s should not match the namespace", sym.Name)
	}

	// The raw mangled prefix keeps working.
	require.True(t, mustSymFilter(t, "^_ZN3app", "").shouldInclude(parseInt))

	// A template instantiation demangles with its return type first, as with
	// c++filt, so the anchored namespace misses it and the unanchored one hits.
	twice := globalFunc("_ZN3app3net5twiceIdEET_S2_")
	require.False(t, ns.shouldInclude(twice))
	require.True(t, mustSymFilter(t, "app::net::twice", "").shouldInclude(twice))

	// Exclude on the demangled name drops both overloads and wins over include.
	noParse := mustSymFilter(t, "^app::net::", `parse\(`)
	require.False(t, noParse.shouldInclude(parseInt))
	require.False(t, noParse.shouldInclude(parseStr))
	require.True(t, noParse.shouldInclude(connOpen))
}

func TestShouldInclude_Binding(t *testing.T) {
	local := newFuncSym(elf.Symbol{Name: "helper", Info: elf.ST_INFO(elf.STB_LOCAL, elf.STT_FUNC)})
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
