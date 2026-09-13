//go:build integration

package trace_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/trace"
)

// TestSymbolTableResolver_Direct calls SymbolTableResolver directly with a real
// binary path, bypassing UserTracee. This verifies the resolver contract
// independently of the tracee wiring.
func TestSymbolTableResolver_Direct(t *testing.T) {
	resolver := trace.SymbolTableResolver(testBinary, testLogger, "", testExcludedSyms, nil, nil)
	entries, err := resolver(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
	for _, e := range entries {
		assert.NotEmpty(t, e.Name)
		assert.NotZero(t, e.Offset)
	}
}

// TestSymbolTableResolver_IncludePattern verifies that the include filter is
// applied when calling the resolver directly.
func TestSymbolTableResolver_IncludePattern(t *testing.T) {
	resolver := trace.SymbolTableResolver(testBinary, testLogger, `^main\.`, "", nil, nil)
	entries, err := resolver(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
	for _, e := range entries {
		assert.Regexp(t, `^main\.`, e.Name)
	}
}

// TestSymbolTableResolver_NoMatch verifies that ErrNoFunctionSymbols is
// returned when the include pattern matches nothing.
func TestSymbolTableResolver_NoMatch(t *testing.T) {
	resolver := trace.SymbolTableResolver(testBinary, testLogger, `^nonexistentsymbol\.$`, "", nil, nil)
	_, err := resolver(t.Context())
	assert.ErrorIs(t, err, trace.ErrNoFunctionSymbols)
}

// cppFixtureSrc is the C++ sample from issue #192: two overloads and two
// template instantiations in a namespace, two member functions, a static
// helper, an extern "C" function and main.
const cppFixtureSrc = "testdata/cpp/demo.cpp"

// cppNamespaceFuncs are the raw names of the non-template functions in
// app::net, mapped to the demangled form demangle.Filter produces with no
// options. Their demangled names start with the namespace.
var cppNamespaceFuncs = map[string]string{
	"_ZN3app3net5parseEPKc":    "app::net::parse(char const*)",
	"_ZN3app3net5parseEi":      "app::net::parse(int)",
	"_ZN3app3net4Conn4openEv":  "app::net::Conn::open()",
	"_ZN3app3net4Conn5closeEv": "app::net::Conn::close()",
}

// cppTemplateFuncs are the two instantiations of app::net::twice. As with
// c++filt, the demangled form of a template instantiation starts with its
// return type, so an include pattern anchored at "^app::net::" does not select
// them while an unanchored "app::net::" does.
var cppTemplateFuncs = map[string]string{
	"_ZN3app3net5twiceIiEET_S2_": "int app::net::twice<int>(int)",
	"_ZN3app3net5twiceIdEET_S2_": "double app::net::twice<double>(double)",
}

// buildCppFixture compiles the C++ sample with debug info into dir and returns
// the binary path. It skips the test when g++ is unavailable. The build-id is
// requested explicitly because not every gcc emits one by default and the
// separate-debug tests verify it.
func buildCppFixture(t *testing.T, dir string) string {
	t.Helper()
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ not available")
	}
	exe := filepath.Join(dir, "demo")
	if out, err := exec.Command("g++", "-O0", "-g", "-Wl,--build-id", "-o", exe, cppFixtureSrc).CombinedOutput(); err != nil {
		t.Skipf("g++ failed: %v: %s", err, out)
	}
	return exe
}

// allCppNetFuncs merges the namespace and template maps.
func allCppNetFuncs() map[string]string {
	all := make(map[string]string, len(cppNamespaceFuncs)+len(cppTemplateFuncs))
	for k, v := range cppNamespaceFuncs {
		all[k] = v
	}
	for k, v := range cppTemplateFuncs {
		all[k] = v
	}
	return all
}

// demangledNames maps each resolved raw name to its Demangled value.
func demangledNames(t *testing.T, r trace.FunctionResolver) map[string]string {
	t.Helper()
	entries, err := r(t.Context())
	require.NoError(t, err)
	m := make(map[string]string, len(entries))
	for _, e := range entries {
		m[e.Name] = e.Demangled
	}
	return m
}

// TestSymbolTableResolver_CppIncludeDemangled verifies that an include
// pattern written against the demangled namespace keeps exactly the app::net
// functions, with the raw name as the entry name and the demangled form next
// to it. helper_static, c_entry and main are dropped.
func TestSymbolTableResolver_CppIncludeDemangled(t *testing.T) {
	exe := buildCppFixture(t, t.TempDir())

	anchored := demangledNames(t, trace.SymbolTableResolver(exe, testLogger, "^app::net::", "", nil, nil))
	require.Equal(t, cppNamespaceFuncs, anchored)

	unanchored := demangledNames(t, trace.SymbolTableResolver(exe, testLogger, "app::net::", "", nil, nil))
	require.Equal(t, allCppNetFuncs(), unanchored)
}

// TestSymbolTableResolver_CppExcludeDemangled verifies that an exclude pattern
// written against the demangled name drops both parse overloads and wins over
// an include that matches them.
func TestSymbolTableResolver_CppExcludeDemangled(t *testing.T) {
	exe := buildCppFixture(t, t.TempDir())

	got := demangledNames(t, trace.SymbolTableResolver(exe, testLogger, "app::net::", `parse\(`, nil, nil))
	require.NotContains(t, got, "_ZN3app3net5parseEi")
	require.NotContains(t, got, "_ZN3app3net5parseEPKc")
	require.Len(t, got, len(allCppNetFuncs())-2)
	require.Equal(t, "app::net::Conn::open()", got["_ZN3app3net4Conn4openEv"])
	require.Equal(t, "double app::net::twice<double>(double)", got["_ZN3app3net5twiceIdEET_S2_"])
}

// TestSymbolTableResolver_CppUnmangledPassThrough verifies that names without
// mangling resolve with Demangled equal to Name, and that a raw mangled prefix
// still matches.
func TestSymbolTableResolver_CppUnmangledPassThrough(t *testing.T) {
	exe := buildCppFixture(t, t.TempDir())

	got := demangledNames(t, trace.SymbolTableResolver(exe, testLogger, "^(c_entry|main)$|^_ZN3app3net5parse", "", nil, nil))
	require.Equal(t, map[string]string{
		"c_entry":               "c_entry",
		"main":                  "main",
		"_ZN3app3net5parseEPKc": "app::net::parse(char const*)",
		"_ZN3app3net5parseEi":   "app::net::parse(int)",
	}, got)
}

// TestSymbolTableResolver_InvalidPattern verifies that a malformed pattern is
// returned as an error instead of panicking.
func TestSymbolTableResolver_InvalidPattern(t *testing.T) {
	_, err := trace.SymbolTableResolver(testBinary, testLogger, "(", "", nil, nil)(t.Context())
	require.ErrorContains(t, err, "invalid include pattern")
}

// TestRecoveryResolver verifies that RecoveryResolver returns
// entries with synthesized func_0x<addr> names and valid file offsets
// from a stripped C binary.
func TestRecoveryResolver(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xcover-recovery-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	src := filepath.Join(tmpDir, "main.c")
	err = os.WriteFile(src, []byte(`
#include <stdio.h>

void greet(const char *name) {
	printf("hello %s\n", name);
}

void farewell(const char *name) {
	printf("bye %s\n", name);
}

int main() {
	greet("world");
	farewell("world");
	return 0;
}
`), 0644)
	require.NoError(t, err)

	bin := filepath.Join(tmpDir, "bin")
	out, err := exec.Command("gcc", "-o", bin, src).CombinedOutput()
	if err != nil {
		t.Errorf("gcc not available: %v: %s", err, out)
	}
	require.NoError(t, exec.Command("strip", bin).Run())

	resolver := trace.RecoveryResolver(bin, testLogger)
	entries, err := resolver(t.Context())
	require.NoError(t, err)
	// The C source defines greet, farewell, and main; _start is the ELF entry point.
	assert.Len(t, entries, 4)

	for _, e := range entries {
		assert.Regexp(t, `^func_0x[0-9a-f]+$`, e.Name)
		assert.Equal(t, e.Name, e.Demangled, "synthetic names carry no mangling")
		assert.NotZero(t, e.Offset)
	}
}
