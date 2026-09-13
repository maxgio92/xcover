package trace

import (
	"debug/elf"
	"testing"

	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func mustSymFilter(t *testing.T, include, exclude string, bindInclude, bindExclude []elf.SymBind) symFilter {
	t.Helper()
	f, err := newSymFilter(include, exclude, bindInclude, bindExclude)
	require.NoError(t, err)
	return f
}

func TestShouldInclude(t *testing.T) {
	fooSym := elf.Symbol{
		Name: "main.fooFunction",
		Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC),
	}
	require.True(t, mustSymFilter(t, "^main.fooFunction$", "", nil, nil).shouldInclude(newFuncSym(fooSym)))

	runtimeSym := elf.Symbol{
		Name: "runtime.sched",
		Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC),
	}
	require.True(t, mustSymFilter(t, "", "", nil, nil).shouldInclude(newFuncSym(runtimeSym)))
	require.False(t, mustSymFilter(t, "", "^runtime.", nil, nil).shouldInclude(newFuncSym(runtimeSym)))
}

// TestShouldInclude_Precedence pins the filter order: bind filters win over
// name patterns, and exclude is applied before include.
func TestShouldInclude_Precedence(t *testing.T) {
	global := elf.Symbol{Name: "main.foo", Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC)}
	local := elf.Symbol{Name: "main.foo", Info: elf.ST_INFO(elf.STB_LOCAL, elf.STT_FUNC)}

	// exclude before include: a name matching both is excluded.
	require.False(t, mustSymFilter(t, "^main", "foo$", nil, nil).shouldInclude(newFuncSym(global)))

	// bindExclude wins regardless of name patterns.
	require.False(t, mustSymFilter(t, "^main", "", nil, []elf.SymBind{elf.STB_LOCAL}).shouldInclude(newFuncSym(local)))

	// bindInclude short-circuits the name patterns, in both directions.
	require.True(t, mustSymFilter(t, "", "^main", []elf.SymBind{elf.STB_GLOBAL}, nil).shouldInclude(newFuncSym(global)))
	require.False(t, mustSymFilter(t, "^main", "", []elf.SymBind{elf.STB_GLOBAL}, nil).shouldInclude(newFuncSym(local)))
}

func TestNewSymFilter_InvalidPattern(t *testing.T) {
	for name, patterns := range map[string][2]string{
		"include": {"(", ""},
		"exclude": {"", "["},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newSymFilter(patterns[0], patterns[1], nil, nil)
			require.ErrorIs(t, err, ErrInvalidPattern)
			require.ErrorIs(t, ValidateSymPatterns(patterns[0], patterns[1]), ErrInvalidPattern)
		})
	}
	require.NoError(t, ValidateSymPatterns("", ""))
	require.NoError(t, ValidateSymPatterns(`^main\.`, `^runtime\.`))
}

// TestFilterFuncSyms exercises the per-symbol filtering behind funcSymsFromELF
// with synthetic symbols: undefined imports, zero-address placeholders and the
// Go linker's text and FIPS markers are dropped; every other zero-size
// function is kept, including an unsized one in the last byte of .text.
func TestFilterFuncSyms(t *testing.T) {
	const textEnd = 0x5000
	funcInfo := elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC)
	syms := []elf.Symbol{
		{Name: "main.main", Info: funcInfo, Section: 1, Value: 0x1000, Size: 0x20},
		{Name: "puts@GLIBC_2.2.5", Info: funcInfo, Section: elf.SHN_UNDEF, Value: 0, Size: 0},
		{Name: "undef_with_value", Info: funcInfo, Section: elf.SHN_UNDEF, Value: 0x2000, Size: 0x10},
		{Name: "zero_value", Info: funcInfo, Section: 1, Value: 0, Size: 0x10},
		// The three Go linker markers: runtime.text aliases the address of the
		// first real function, runtime.text.N delimits a split text section,
		// runtime.etext sits MinLC bytes before the section end.
		{Name: "runtime.text", Info: funcInfo, Section: 1, Value: 0x1000, Size: 0},
		{Name: "runtime.text.1", Info: funcInfo, Section: 1, Value: 0x2800, Size: 0},
		{Name: "runtime.etext", Info: funcInfo, Section: 1, Value: textEnd - 1, Size: 0},
		// GOFIPS140 builds emit 1-byte padding symbols around the FIPS module.
		{Name: "go:textfipsstart", Info: funcInfo, Section: 1, Value: 0x2b00, Size: 1},
		{Name: "go:textfipsend", Info: funcInfo, Section: 1, Value: 0x2c00, Size: 1},
		// A sized function whose name only resembles a marker is not one.
		{Name: "runtime.textOff", Info: funcInfo, Section: 1, Value: 0x2900, Size: 0x10},
		{Name: "runtime.text.x", Info: funcInfo, Section: 1, Value: 0x2a00, Size: 0},
		// Unsized assembly functions are code, even in the last byte of .text
		// (a called one-byte ret with no .size directive).
		{Name: "asm_no_size", Info: funcInfo, Section: 1, Value: 0x3000, Size: 0},
		{Name: "tail_func", Info: funcInfo, Section: 1, Value: textEnd - 1, Size: 0},
		{Name: "sized_at_text_end", Info: funcInfo, Section: 1, Value: textEnd - 1, Size: 0x1},
		{Name: "fini_zero_size", Info: funcInfo, Section: 3, Value: textEnd + 0x10, Size: 0},
		{Name: "data_object", Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_OBJECT), Section: 2, Value: 0x4000, Size: 0x8},
	}
	kept := []string{"main.main", "runtime.textOff", "runtime.text.x", "asm_no_size", "tail_func", "sized_at_text_end", "fini_zero_size"}

	got := filterFuncSyms(syms, mustSymFilter(t, "", "", nil, nil))

	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	require.ElementsMatch(t, kept, names)

	// Name patterns are applied after the structural checks.
	got = filterFuncSyms(syms, mustSymFilter(t, "^main", "", nil, nil))
	require.Len(t, got, 1)
	require.Equal(t, "main.main", got[0].Name)
}

func TestSymbolTableResolver_InvalidPattern(t *testing.T) {
	for name, patterns := range map[string][2]string{
		"include": {"(", ""},
		"exclude": {"", "["},
	} {
		t.Run(name, func(t *testing.T) {
			// The pattern is rejected before the binary is opened, so no real
			// ELF file is needed.
			_, err := SymbolTableResolver("/nonexistent-binary-path", log.Nop(), patterns[0], patterns[1], nil, nil)(t.Context())
			require.True(t, errors.Is(err, ErrInvalidPattern), "got %v", err)

			_, err = GoProjectResolver("/nonexistent-binary-path", log.Nop(), patterns[0], patterns[1], nil, nil)(t.Context())
			require.True(t, errors.Is(err, ErrInvalidPattern), "got %v", err)

			_, err = SeparateDebugResolver("/nonexistent-binary-path", "/nonexistent-debug-path", log.Nop(), patterns[0], patterns[1], nil, nil, false)(t.Context())
			require.True(t, errors.Is(err, ErrInvalidPattern), "got %v", err)
		})
	}
}

func globalFunc(name string) funcSym {
	return newFuncSym(elf.Symbol{
		Name: name,
		Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC),
	})
}

// TestShouldInclude_Demangled checks that name patterns match the demangled
// C++ name as well as the raw one, and that exclude still wins.
func TestShouldInclude_Demangled(t *testing.T) {
	parseInt := globalFunc("_ZN3app3net5parseEi")
	parseStr := globalFunc("_ZN3app3net5parseEPKc")
	connOpen := globalFunc("_ZN3app3net4Conn4openEv")
	cEntry := globalFunc("c_entry")
	helper := globalFunc("_ZL13helper_statici")

	ns := mustSymFilter(t, "^app::net::", "", nil, nil)
	for _, sym := range []funcSym{parseInt, parseStr, connOpen} {
		require.Truef(t, ns.shouldInclude(sym), "%s should match the namespace", sym.Name)
	}
	for _, sym := range []funcSym{cEntry, helper} {
		require.Falsef(t, ns.shouldInclude(sym), "%s should not match the namespace", sym.Name)
	}

	// The raw mangled prefix keeps working.
	require.True(t, mustSymFilter(t, "^_ZN3app", "", nil, nil).shouldInclude(parseInt))

	// A template instantiation demangles with its return type first, as with
	// c++filt, so the anchored namespace misses it and the unanchored one hits.
	twice := globalFunc("_ZN3app3net5twiceIdEET_S2_")
	require.False(t, ns.shouldInclude(twice))
	require.True(t, mustSymFilter(t, "app::net::twice", "", nil, nil).shouldInclude(twice))

	// Exclude on the demangled name drops both overloads and wins over include.
	noParse := mustSymFilter(t, "^app::net::", `parse\(`, nil, nil)
	require.False(t, noParse.shouldInclude(parseInt))
	require.False(t, noParse.shouldInclude(parseStr))
	require.True(t, noParse.shouldInclude(connOpen))
}
