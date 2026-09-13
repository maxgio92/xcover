package trace

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDemangleName covers one Itanium C++ function, one C++ template
// instantiation, one Rust legacy name, one Rust v0 name, and names that carry
// no mangling and must pass through unchanged (Go, C, and recovery names).
func TestDemangleName(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"_ZN3app3net5parseEi", "app::net::parse(int)"},
		{"_ZN3app3net5parseEPKc", "app::net::parse(char const*)"},
		{"_ZN3app3net5twiceIdEET_S2_", "double app::net::twice<double>(double)"},
		{"_ZN4core3fmt9Formatter3pad17h5d6b4c8a0f1e2d3bE", "core::fmt::Formatter::pad"},
		{"_RNvNtCs1234_7mycrate3net5parse", "mycrate::net::parse"},
		{"main.(*Server).Handle", "main.(*Server).Handle"},
		{"c_entry", "c_entry"},
		{"func_0x401000", "func_0x401000"},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			require.Equal(t, tc.want, demangleName(tc.raw))
		})
	}
}

// TestDemangleNameBoundsOutput feeds a Rust v0 symbol whose tuples each
// back-reference the previous tuple twice, so the demangled text doubles per
// level: 155 input bytes expand to about 1.5 MB unbounded, and a few hundred
// bytes would exhaust memory. The result must stay within the configured cap
// plus at most one trailing token, since the demangler checks the cap before
// each write.
func TestDemangleNameBoundsOutput(t *testing.T) {
	const raw = "_RIC1xTuuETB3_B3_ETB7_B7_ETBf_Bf_ETBn_Bn_ETBv_Bv_ETBD_BD_ETBL_BL_ETBT_BT_ETB11_B11_ETB19_B19_ETB1j_B1j_ETB1t_B1t_ETB1D_B1D_ETB1N_B1N_ETB1X_B1X_ETB27_B27_EE"

	got := demangleName(raw)

	require.LessOrEqual(t, len(got), 1<<maxDemangledLenPow+len(raw))
}

// TestDemangleNameBoundsInput feeds an Itanium symbol one byte over the input
// bound. Parsed, it would demangle to f(int***...) after a stack-deep pointer
// recursion; instead it must pass through unchanged. A symbol exactly at the
// bound is still demangled, so the bound is inclusive.
func TestDemangleNameBoundsInput(t *testing.T) {
	over := "_Z1f" + strings.Repeat("P", maxMangledLen-4) + "i"
	require.Len(t, over, maxMangledLen+1)
	require.Equal(t, over, demangleName(over))

	atBound := "_Z1f" + strings.Repeat("P", maxMangledLen-5) + "i"
	require.Len(t, atBound, maxMangledLen)
	require.Equal(t, "f(int"+strings.Repeat("*", maxMangledLen-5)+")", demangleName(atBound))
}
