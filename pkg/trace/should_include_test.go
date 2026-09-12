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
