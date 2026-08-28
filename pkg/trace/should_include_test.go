package trace

import (
	"debug/elf"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldInclude(t *testing.T) {
	fooSym := elf.Symbol{
		Name: "main.fooFunction",
		Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC),
	}
	require.True(t, shouldInclude(fooSym, "^main.fooFunction$", "", nil, nil))

	runtimeSym := elf.Symbol{
		Name: "runtime.sched",
		Info: elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC),
	}
	require.True(t, shouldInclude(runtimeSym, "", "", nil, nil))
	require.False(t, shouldInclude(runtimeSym, "", "^runtime.", nil, nil))
}
