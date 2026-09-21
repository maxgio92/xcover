//go:build integration

package trace_test

import (
	"debug/elf"
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
		// The Go linker's zero-size end-of-code marker is not a function.
		assert.NotEqual(t, "runtime.etext", e.Name)
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

// recoveryFixtureSrc is a C program with three defined functions. Once
// stripped, its .symtab is gone but .eh_frame stays, so RecoveryResolver
// finds greet, farewell, main and the _start entry point: four functions.
const recoveryFixtureSrc = `
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
`

// TestRecoveryResolver verifies that RecoveryResolver returns
// entries with synthesized func_0x<addr> names and valid file offsets
// from a stripped C binary.
func TestRecoveryResolver(t *testing.T) {
	bin := buildStrippedCBinary(t)

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

// buildStrippedCBinary compiles recoveryFixtureSrc and strips it, returning
// the binary path. It skips the test when gcc is unavailable.
func buildStrippedCBinary(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not available")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.c")
	require.NoError(t, os.WriteFile(src, []byte(recoveryFixtureSrc), 0o644))

	bin := filepath.Join(dir, "bin")
	if out, err := exec.Command("gcc", "-o", bin, src).CombinedOutput(); err != nil {
		t.Fatalf("gcc failed: %v: %s", err, out)
	}
	require.NoError(t, exec.Command("strip", bin).Run())
	return bin
}

// TestUserTracee_Init_RecoveryWithoutFilters runs the default resolver chain
// on a stripped C binary: SymbolTableResolver finds neither .symtab nor
// .gopclntab and returns ErrNoSymbolTable, so Init falls back to recovery and
// collects the four functions with no filter set.
func TestUserTracee_Init_RecoveryWithoutFilters(t *testing.T) {
	bin := buildStrippedCBinary(t)

	tracee := trace.NewUserTracee(
		trace.WithTraceeExePath(bin),
		trace.WithTraceeLogger(testLogger),
	)
	require.NoError(t, tracee.Init(t.Context()))
	assert.Len(t, tracee.GetFuncNames(), 4)
}

// TestUserTracee_Init_RecoveryRefusesInclude runs the same chain with an
// include pattern. Recovery names functions func_0x<addr>, which the pattern
// cannot match, so Init refuses with ErrFilterNeedsSymbols instead of
// silently ignoring the filter.
func TestUserTracee_Init_RecoveryRefusesInclude(t *testing.T) {
	bin := buildStrippedCBinary(t)

	tracee := trace.NewUserTracee(
		trace.WithTraceeExePath(bin),
		trace.WithTraceeLogger(testLogger),
		trace.WithTraceeSymPatternInclude("^greet$"),
	)
	err := tracee.Init(t.Context())
	require.ErrorIs(t, err, trace.ErrFilterNeedsSymbols)
	require.Contains(t, err.Error(), "failed to resolve functions")
}

// TestSymbolTableResolver_PIESkipsUndefinedImports compiles a dynamically
// linked PIE, whose first PT_LOAD has Vaddr 0, and checks that undefined
// imports such as puts@GLIBC_2.2.5 (STT_FUNC, SHN_UNDEF, Value 0) are not
// resolved to file offset 0 and probed as functions.
func TestSymbolTableResolver_PIESkipsUndefinedImports(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not available")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.c")
	require.NoError(t, os.WriteFile(src, []byte(`
#include <stdio.h>
__attribute__((noinline)) void greet(void) { puts("hello"); }
int main(void) { greet(); return 0; }
`), 0o644))

	bin := filepath.Join(dir, "pie")
	if out, err := exec.Command("gcc", "-O0", "-fPIE", "-pie", "-o", bin, src).CombinedOutput(); err != nil {
		t.Fatalf("gcc failed: %v: %s", err, out)
	}

	// Collect the fixture's undefined symbols (puts@GLIBC_2.2.5 and friends)
	// so the check does not depend on the versioned spelling.
	f, err := elf.Open(bin)
	require.NoError(t, err)
	defer f.Close()
	syms, err := f.Symbols()
	require.NoError(t, err)
	undefined := make(map[string]bool)
	for _, s := range syms {
		if s.Section == elf.SHN_UNDEF && elf.ST_TYPE(s.Info) == elf.STT_FUNC && s.Name != "" {
			undefined[s.Name] = true
		}
	}
	require.NotEmpty(t, undefined, "fixture has no undefined function imports; puts should be imported")

	entries, err := trace.SymbolTableResolver(bin, testLogger, "", "", nil, nil)(t.Context())
	require.NoError(t, err)

	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name] = true
		assert.NotZerof(t, e.Offset, "function %q resolved to file offset 0", e.Name)
		assert.Falsef(t, undefined[e.Name], "undefined import %q leaked into the function list", e.Name)
	}
	assert.True(t, names["greet"], "expected defined function greet, got %v", entries)
	assert.True(t, names["main"], "expected defined function main, got %v", entries)
}
