package trace

import (
	"debug/elf"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldInclude(t *testing.T) {
	fooSym := elf.Symbol{
		Name: "main.fooFunction",
		Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC),
	}
	require.True(t, shouldInclude(fooSym, regexp.MustCompile("^main.fooFunction$"), nil, nil, nil))

	runtimeSym := elf.Symbol{
		Name: "runtime.sched",
		Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC),
	}
	require.True(t, shouldInclude(runtimeSym, nil, nil, nil, nil))
	require.False(t, shouldInclude(runtimeSym, nil, regexp.MustCompile("^runtime."), nil, nil))
}

func TestCompileSymPatterns(t *testing.T) {
	inc, exc, err := compileSymPatterns("", "")
	require.NoError(t, err)
	require.Nil(t, inc)
	require.Nil(t, exc)

	inc, exc, err = compileSymPatterns("^main", "^runtime")
	require.NoError(t, err)
	require.True(t, inc.MatchString("main.foo"))
	require.True(t, exc.MatchString("runtime.sched"))

	_, _, err = compileSymPatterns("(", "")
	require.ErrorContains(t, err, "invalid include pattern")

	_, _, err = compileSymPatterns("", "(")
	require.ErrorContains(t, err, "invalid exclude pattern")
}
