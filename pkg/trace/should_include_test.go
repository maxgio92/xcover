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
	require.True(t, mustSymFilter(t, "^main.fooFunction$", "", nil, nil).shouldInclude(fooSym))

	runtimeSym := elf.Symbol{
		Name: "runtime.sched",
		Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC),
	}
	require.True(t, mustSymFilter(t, "", "", nil, nil).shouldInclude(runtimeSym))
	require.False(t, mustSymFilter(t, "", "^runtime.", nil, nil).shouldInclude(runtimeSym))
}

// TestShouldInclude_Precedence pins the filter order: bind filters win over
// name patterns, and exclude is applied before include.
func TestShouldInclude_Precedence(t *testing.T) {
	global := elf.Symbol{Name: "main.foo", Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC)}
	local := elf.Symbol{Name: "main.foo", Info: elf.ST_INFO(elf.STB_LOCAL, elf.STT_FUNC)}

	// exclude before include: a name matching both is excluded.
	require.False(t, mustSymFilter(t, "^main", "foo$", nil, nil).shouldInclude(global))

	// bindExclude wins regardless of name patterns.
	require.False(t, mustSymFilter(t, "^main", "", nil, []elf.SymBind{elf.STB_LOCAL}).shouldInclude(local))

	// bindInclude short-circuits the name patterns, in both directions.
	require.True(t, mustSymFilter(t, "", "^main", []elf.SymBind{elf.STB_GLOBAL}, nil).shouldInclude(global))
	require.False(t, mustSymFilter(t, "^main", "", []elf.SymBind{elf.STB_GLOBAL}, nil).shouldInclude(local))
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
// .text end marker are dropped, other zero-size functions are kept.
func TestFilterFuncSyms(t *testing.T) {
	const textEnd = 0x5000
	funcInfo := elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC)
	syms := []elf.Symbol{
		{Name: "main.main", Info: funcInfo, Section: 1, Value: 0x1000, Size: 0x20},
		{Name: "puts@GLIBC_2.2.5", Info: funcInfo, Section: elf.SHN_UNDEF, Value: 0, Size: 0},
		{Name: "undef_with_value", Info: funcInfo, Section: elf.SHN_UNDEF, Value: 0x2000, Size: 0x10},
		{Name: "zero_value", Info: funcInfo, Section: 1, Value: 0, Size: 0x10},
		// runtime.etext sits MinLC bytes before the section end: 1 on amd64,
		// 4 on arm64. An exact-end marker is covered too.
		{Name: "runtime.etext", Info: funcInfo, Section: 1, Value: textEnd - 1, Size: 0},
		{Name: "runtime.etext.arm64", Info: funcInfo, Section: 1, Value: textEnd - 4, Size: 0},
		{Name: "etext_exact", Info: funcInfo, Section: 1, Value: textEnd, Size: 0},
		{Name: "asm_no_size", Info: funcInfo, Section: 1, Value: 0x3000, Size: 0},
		{Name: "asm_no_size_near_end", Info: funcInfo, Section: 1, Value: textEnd - 5, Size: 0},
		{Name: "sized_at_text_end", Info: funcInfo, Section: 1, Value: textEnd - 1, Size: 0x1},
		{Name: "fini_zero_size", Info: funcInfo, Section: 3, Value: textEnd + 0x10, Size: 0},
		{Name: "data_object", Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_OBJECT), Section: 2, Value: 0x4000, Size: 0x8},
	}
	kept := []string{"main.main", "asm_no_size", "asm_no_size_near_end", "sized_at_text_end", "fini_zero_size"}

	got := filterFuncSyms(syms, textEnd, mustSymFilter(t, "", "", nil, nil))

	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	require.ElementsMatch(t, kept, names)

	// With an unknown .text end (0) the marker check is disabled but the
	// undefined/zero-address checks still apply.
	got = filterFuncSyms(syms, 0, mustSymFilter(t, "", "", nil, nil))
	names = names[:0]
	for _, s := range got {
		names = append(names, s.Name)
	}
	require.ElementsMatch(t, append(kept, "runtime.etext", "runtime.etext.arm64", "etext_exact"), names)

	// Name patterns are applied after the structural checks.
	got = filterFuncSyms(syms, textEnd, mustSymFilter(t, "^main", "", nil, nil))
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
