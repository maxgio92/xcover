//go:build integration

package trace_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/trace"
)

const debugFixtureSrc = `
#include <stdio.h>
__attribute__((noinline)) void alpha(void){ printf("a\n"); }
__attribute__((noinline)) void beta(void){ printf("b\n"); }
int main(void){ alpha(); beta(); return 0; }
`

// buildDebugFixture compiles src as a PIE with debug info, then derives a
// separate debug file (objcopy --only-keep-debug) and a fully stripped binary.
// Extra gcc args may be appended (e.g. -Wl,--build-id=none).
func buildDebugFixture(t *testing.T, dir, name, src string, extraGCC ...string) (exe, dbg, stripped string) {
	t.Helper()
	srcPath := filepath.Join(dir, name+".c")
	require.NoError(t, os.WriteFile(srcPath, []byte(src), 0o644))

	exe = filepath.Join(dir, name)
	args := append([]string{"-O2", "-g", "-fPIE", "-pie", "-o", exe, srcPath}, extraGCC...)
	if out, err := exec.Command("gcc", args...).CombinedOutput(); err != nil {
		t.Skipf("gcc unavailable or failed: %v: %s", err, out)
	}

	dbg = exe + ".debug"
	out, err := exec.Command("objcopy", "--only-keep-debug", exe, dbg).CombinedOutput()
	require.NoErrorf(t, err, "objcopy --only-keep-debug: %s", out)

	stripped = exe + ".stripped"
	require.NoError(t, debugCopyFile(stripped, exe))
	out, err = exec.Command("strip", "--strip-all", stripped).CombinedOutput()
	require.NoErrorf(t, err, "strip: %s", out)
	return exe, dbg, stripped
}

func debugCopyFile(dst, src string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o755)
}

func nameOffsets(t *testing.T, r trace.FunctionResolver) map[string]uint64 {
	t.Helper()
	entries, err := r(t.Context())
	require.NoError(t, err)
	m := make(map[string]uint64, len(entries))
	for _, e := range entries {
		m[e.Name] = e.Offset
	}
	return m
}

// TestSeparateDebugResolver_SymtabEquivalence is the core guarantee: resolving a
// stripped binary via its companion debug file yields exactly the same
// name->offset mapping as resolving the unstripped binary directly.
func TestSeparateDebugResolver_SymtabEquivalence(t *testing.T) {
	dir := t.TempDir()
	exe, dbg, stripped := buildDebugFixture(t, dir, "equiv", debugFixtureSrc)

	want := nameOffsets(t, trace.SymbolTableResolver(exe, testLogger, `^(alpha|beta|main)$`, "", nil, nil))
	got := nameOffsets(t, trace.SeparateDebugResolver(stripped, dbg, testLogger, `^(alpha|beta|main)$`, "", nil, nil, false))

	require.Len(t, got, 3)
	require.Equal(t, want, got)
}

// TestSeparateDebugResolver_BuildIDMismatch rejects a debug file that belongs to
// a different build.
func TestSeparateDebugResolver_BuildIDMismatch(t *testing.T) {
	dir := t.TempDir()
	_, _, stripped := buildDebugFixture(t, dir, "a", debugFixtureSrc)
	_, otherDbg, _ := buildDebugFixture(t, dir, "b", debugFixtureSrc+"\nint extra(void){return 7;}\n")

	_, err := trace.SeparateDebugResolver(stripped, otherDbg, testLogger, "", "", nil, nil, false)(t.Context())
	require.ErrorIs(t, err, trace.ErrDebugBuildIDMismatch)
}

// TestSeparateDebugResolver_NoBuildID fails closed when build-ids are absent,
// and proceeds when verification is explicitly disabled.
func TestSeparateDebugResolver_NoBuildID(t *testing.T) {
	dir := t.TempDir()
	_, dbg, stripped := buildDebugFixture(t, dir, "nobid", debugFixtureSrc, "-Wl,--build-id=none")

	_, err := trace.SeparateDebugResolver(stripped, dbg, testLogger, "", "", nil, nil, false)(t.Context())
	require.ErrorIs(t, err, trace.ErrNoBuildID)

	got := nameOffsets(t, trace.SeparateDebugResolver(stripped, dbg, testLogger, `^(alpha|beta|main)$`, "", nil, nil, true))
	require.Len(t, got, 3)
}

// TestSeparateDebugResolver_FiltersUndefined ensures imported/zero-address
// symbols are dropped (no probe at file offset 0 on a PIE).
func TestSeparateDebugResolver_FiltersUndefined(t *testing.T) {
	dir := t.TempDir()
	_, dbg, stripped := buildDebugFixture(t, dir, "undef", debugFixtureSrc)

	entries, err := trace.SeparateDebugResolver(stripped, dbg, testLogger, "", "", nil, nil, false)(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, e := range entries {
		require.NotZerof(t, e.Offset, "function %q resolved to offset 0", e.Name)
		require.NotContainsf(t, e.Name, "printf", "imported symbol %q leaked", e.Name)
	}
}

// TestSeparateDebugResolver_DWARFFallback resolves via DWARF subprograms when
// the debug file has no .symtab.
func TestSeparateDebugResolver_DWARFFallback(t *testing.T) {
	dir := t.TempDir()
	_, dbg, stripped := buildDebugFixture(t, dir, "dw", debugFixtureSrc)

	out, err := exec.Command("objcopy", "--remove-section=.symtab", "--remove-section=.strtab", dbg, dbg).CombinedOutput()
	require.NoErrorf(t, err, "objcopy --remove-section: %s", out)

	got := nameOffsets(t, trace.SeparateDebugResolver(stripped, dbg, testLogger, `^(alpha|beta|main)$`, "", nil, nil, false))
	require.Contains(t, got, "alpha")
	require.Contains(t, got, "beta")
	require.Contains(t, got, "main")
}

// TestSeparateDebugResolver_DWARFCppMembers verifies the DWARF fallback resolves
// C++ member and namespaced functions, whose definition DIE carries low_pc but
// gets its name only via a DW_AT_specification reference, and disambiguates
// overloads via DW_AT_linkage_name.
func TestSeparateDebugResolver_DWARFCppMembers(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ not available")
	}
	dir := t.TempDir()
	src := `
#include <cstdio>
namespace ns { int helper(int x){ return x+1; } }
struct Widget { int method(int x){ return x*2; } };
int foo(int x){ return x+1; }
double foo(double x){ return x+0.5; }
int main(){ Widget w; volatile int s = ns::helper(1)+w.method(2)+foo(3)+(int)foo(4.0); printf("%d\n", s); return 0; }
`
	srcPath := filepath.Join(dir, "cpp.cpp")
	require.NoError(t, os.WriteFile(srcPath, []byte(src), 0o644))
	exe := filepath.Join(dir, "cpp")
	if out, err := exec.Command("g++", "-O0", "-g", "-fPIE", "-pie", "-o", exe, srcPath).CombinedOutput(); err != nil {
		t.Skipf("g++ failed: %v: %s", err, out)
	}
	dbg := exe + ".debug"
	out, err := exec.Command("objcopy", "--only-keep-debug", exe, dbg).CombinedOutput()
	require.NoErrorf(t, err, "objcopy --only-keep-debug: %s", out)
	stripped := exe + ".stripped"
	require.NoError(t, debugCopyFile(stripped, exe))
	out, err = exec.Command("strip", "--strip-all", stripped).CombinedOutput()
	require.NoErrorf(t, err, "strip: %s", out)
	// Force the DWARF path by removing the symbol table from the debug file.
	out, err = exec.Command("objcopy", "--remove-section=.symtab", "--remove-section=.strtab", dbg, dbg).CombinedOutput()
	require.NoErrorf(t, err, "objcopy --remove-section: %s", out)

	got := nameOffsets(t, trace.SeparateDebugResolver(stripped, dbg, testLogger, "", "", nil, nil, false))

	// Member/namespaced definitions (name via DW_AT_specification) and overloads
	// (distinct linkage names) must all resolve.
	for _, want := range []string{"_ZN6Widget6methodEi", "_ZN2ns6helperEi", "_Z3fooi", "_Z3food"} {
		_, ok := got[want]
		require.Truef(t, ok, "DWARF fallback missed %q; resolved: %v", want, keysOf(got))
	}
}

func keysOf(m map[string]uint64) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// TestSeparateDebugResolver_SectionlessExe verifies the resolver still works on
// an executable whose section table has been removed (eu-strip --strip-sections):
// build-id is read from PT_NOTE and offsets from PT_LOAD, so it matches the
// unstripped resolution. This is the case where no other resolver can work.
func TestSeparateDebugResolver_SectionlessExe(t *testing.T) {
	if _, err := exec.LookPath("eu-strip"); err != nil {
		t.Skip("eu-strip not available")
	}
	dir := t.TempDir()
	exe, dbg, _ := buildDebugFixture(t, dir, "sl", debugFixtureSrc)

	sectionless := exe + ".nosections"
	require.NoError(t, debugCopyFile(sectionless, exe))
	out, err := exec.Command("eu-strip", "--strip-sections", sectionless).CombinedOutput()
	require.NoErrorf(t, err, "eu-strip --strip-sections: %s", out)

	want := nameOffsets(t, trace.SymbolTableResolver(exe, testLogger, `^(alpha|beta|main)$`, "", nil, nil))
	got := nameOffsets(t, trace.SeparateDebugResolver(sectionless, dbg, testLogger, `^(alpha|beta|main)$`, "", nil, nil, false))
	require.Equal(t, want, got)
}
