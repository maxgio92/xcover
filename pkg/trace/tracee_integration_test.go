//go:build integration

package trace

import (
	"debug/elf"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	integrationTestBinary       = path.Join("testdata", "gotest")
	integrationTestExcludedSyms = "^runtime.text$|^internal/cpu.Initialize$"
)

// TestLoadFunctionsFromGoPclntab tests that we can load function symbols
// from the .gopclntab section of a stripped Go binary.
//
// IMPORTANT: This test validates that offsets are correctly converted from
// virtual addresses (VA) to file offsets. The .gopclntab section contains
// function entry points as VAs, but uprobes require file offsets.
// The conversion formula is: fileOffset = VA - textSection.Addr + textSection.Offset
func TestLoadFunctionsFromGoPclntab(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "xcover-gopclntab-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a simple Go program to test with
	testProgramPath := filepath.Join(tmpDir, "testprogram.go")
	testProgram := `package main

import "fmt"

//go:noinline
func hello() {
	fmt.Println("Hello")
}

//go:noinline
func world() {
	fmt.Println("World")
}

//go:noinline
func greet(name string) {
	fmt.Printf("Hello, %s!\n", name)
}

func main() {
	hello()
	world()
	greet("test")
}
`
	err = os.WriteFile(testProgramPath, []byte(testProgram), 0644)
	require.NoError(t, err)

	// Build the test program
	binaryPath := filepath.Join(tmpDir, "testprogram")
	cmd := exec.Command("go", "build", "-o", binaryPath, testProgramPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Build output: %s", output)
		require.NoError(t, err, "failed to build test program")
	}

	// Verify the binary has symbols before stripping
	elfFile, err := elf.Open(binaryPath)
	require.NoError(t, err)
	defer elfFile.Close()

	syms, err := elfFile.Symbols()
	assert.NoError(t, err, "unstripped binary should have symbols")
	assert.Greater(t, len(syms), 0, "unstripped binary should have symbols")

	// Test with unstripped binary
	t.Run("UnstrippedBinary", func(t *testing.T) {
		logger := zerolog.New(os.Stdout).Level(zerolog.DebugLevel)
		tracee := NewUserTracee(
			WithTraceeExePath(binaryPath),
			WithTraceeSymPatternInclude("main\\."),
			WithTraceeLogger(logger),
		)

		err := tracee.Init(t.Context())
		require.NoError(t, err, "should load functions from unstripped binary")
		assert.Greater(t, len(tracee.funcs), 0, "should have loaded functions")

		// Check that our test functions are present
		foundHello := false
		foundWorld := false
		foundGreet := false
		for _, fn := range tracee.funcs {
			if fn.name == "main.hello" {
				foundHello = true
			}
			if fn.name == "main.world" {
				foundWorld = true
			}
			if fn.name == "main.greet" {
				foundGreet = true
			}
		}
		assert.True(t, foundHello, "should have found main.hello function")
		assert.True(t, foundWorld, "should have found main.world function")
		assert.True(t, foundGreet, "should have found main.greet function")
	})

	// Create a stripped version of the binary
	strippedPath := filepath.Join(tmpDir, "testprogram-stripped")
	cmd = exec.Command("cp", binaryPath, strippedPath)
	require.NoError(t, cmd.Run())

	cmd = exec.Command("strip", strippedPath)
	require.NoError(t, cmd.Run())

	// Verify the stripped binary has no symbols
	elfFile, err = elf.Open(strippedPath)
	require.NoError(t, err)
	defer elfFile.Close()

	syms, err = elfFile.Symbols()
	assert.Error(t, err, "stripped binary should not have symbol table")

	// Verify .gopclntab section still exists
	pclntabSection := elfFile.Section(".gopclntab")
	assert.NotNil(t, pclntabSection, "stripped binary should still have .gopclntab section")

	// Test with stripped binary - should fall back to .gopclntab
	t.Run("StrippedBinary", func(t *testing.T) {
		logger := zerolog.New(os.Stdout).Level(zerolog.DebugLevel)
		tracee := NewUserTracee(
			WithTraceeExePath(strippedPath),
			WithTraceeSymPatternInclude("main\\."),
			WithTraceeLogger(logger),
		)

		err := tracee.Init(t.Context())
		require.NoError(t, err, "should load functions from stripped binary via .gopclntab")
		assert.Greater(t, len(tracee.funcs), 0, "should have loaded functions from .gopclntab")

		// Check that our test functions are present
		foundHello := false
		foundWorld := false
		foundGreet := false
		for _, fn := range tracee.funcs {
			t.Logf("Found function: %s @ 0x%x", fn.name, fn.offset)
			if fn.name == "main.hello" {
				foundHello = true
			}
			if fn.name == "main.world" {
				foundWorld = true
			}
			if fn.name == "main.greet" {
				foundGreet = true
			}
		}
		assert.True(t, foundHello, "should have found main.hello function in stripped binary")
		assert.True(t, foundWorld, "should have found main.world function in stripped binary")
		assert.True(t, foundGreet, "should have found main.greet function in stripped binary")

		// Verify that offsets are file offsets, not virtual addresses.
		// This validates the fix for the bug where VA was used directly instead of converting to file offset.
		textSection := elfFile.Section(".text")
		require.NotNil(t, textSection, "should have .text section")

		for _, fn := range tracee.funcs {
			if fn.name == "main.hello" || fn.name == "main.world" || fn.name == "main.greet" {
				// File offsets should be much smaller than virtual addresses.
				// For a typical Go binary, .text starts at VA ~0x400000+ but file offset ~0x1000.
				// So a valid file offset should be less than 1MB for our small test binary.
				assert.Less(t, fn.offset, uint64(1024*1024),
					"offset for %s should be a file offset, not a VA (got 0x%x)", fn.name, fn.offset)

				// Also verify offset is within reasonable range (after text section start)
				assert.GreaterOrEqual(t, fn.offset, textSection.Offset,
					"offset for %s should be >= .text section file offset", fn.name)

				t.Logf("VALIDATED: %s has file offset 0x%x (within .text @ 0x%x, VA 0x%x)",
					fn.name, fn.offset, textSection.Offset, textSection.Addr)
			}
		}
	})
}

// TestLoadFunctionsFromGoPclntab_NoGoPclntab verifies that a stripped non-Go binary
// (no symbol table, no .gopclntab) is handled by RecoveryResolver, returning
// synthesized func_0x<addr> entries rather than an error.
func TestLoadFunctionsFromGoPclntab_NoGoPclntab(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xcover-gopclntab-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	testProgramPath := filepath.Join(tmpDir, "testprogram.c")
	err = os.WriteFile(testProgramPath, []byte(`#include <stdio.h>
void hello() { printf("Hello\n"); }
int main() { hello(); return 0; }
`), 0644)
	require.NoError(t, err)

	binaryPath := filepath.Join(tmpDir, "testprogram")
	out, err := exec.Command("gcc", "-o", binaryPath, testProgramPath).CombinedOutput()
	if err != nil {
		t.Skipf("gcc not available: %v: %s", err, out)
	}
	require.NoError(t, exec.Command("strip", binaryPath).Run())

	logger := zerolog.New(os.Stdout).Level(zerolog.DebugLevel)
	tracee := NewUserTracee(
		WithTraceeExePath(binaryPath),
		WithTraceeLogger(logger),
	)

	err = tracee.Init(t.Context())
	require.NoError(t, err, "RecoveryResolver should handle a stripped non-Go binary")
	assert.NotEmpty(t, tracee.funcs)
	for _, fn := range tracee.funcs {
		assert.Regexp(t, `^func_0x[0-9a-f]+$`, fn.name)
	}
}

// TestGoPclntabOffsetCalculation verifies that offsets from .gopclntab are
// correctly converted from virtual addresses to file offsets
func TestGoPclntabOffsetCalculation(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "xcover-gopclntab-offset-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a simple Go program to test with
	testProgramPath := filepath.Join(tmpDir, "testprogram.go")
	testProgram := `package main

import "fmt"

//go:noinline
func testFunc() {
	fmt.Println("Test")
}

func main() {
	testFunc()
}
`
	err = os.WriteFile(testProgramPath, []byte(testProgram), 0644)
	require.NoError(t, err)

	// Build and strip the test program
	binaryPath := filepath.Join(tmpDir, "testprogram")
	cmd := exec.Command("go", "build", "-o", binaryPath, testProgramPath)
	require.NoError(t, cmd.Run())

	cmd = exec.Command("strip", binaryPath)
	require.NoError(t, cmd.Run())

	// Open the ELF file to get actual section information
	elfFile, err := elf.Open(binaryPath)
	require.NoError(t, err)
	defer elfFile.Close()

	textSection := elfFile.Section(".text")
	require.NotNil(t, textSection, "binary should have .text section")

	// Load functions using our implementation
	logger := zerolog.New(os.Stdout).Level(zerolog.DebugLevel)
	tracee := NewUserTracee(
		WithTraceeExePath(binaryPath),
		WithTraceeSymPatternInclude("main\\."),
		WithTraceeLogger(logger),
	)

	err = tracee.Init(t.Context())
	require.NoError(t, err, "should load functions from stripped binary via .gopclntab")
	require.Greater(t, len(tracee.funcs), 0, "should have loaded functions")

	// Verify that offsets are file offsets, not virtual addresses
	// Virtual addresses in .text typically start at 0x400000+ for 64-bit binaries
	// File offsets should be much smaller (usually < 0x100000)
	for _, fn := range tracee.funcs {
		// File offsets should be relative to the file, not huge virtual addresses
		// The .text section virtual address is typically 0x400000 or 0x401000
		// File offsets should be close to .text file offset (usually 0x1000)
		assert.Less(t, fn.offset, textSection.Addr,
			"offset for %s should be file offset (%x), not virtual address (would be >= %x)",
			fn.name, fn.offset, textSection.Addr)

		// File offset should be within reasonable range of .text file offset
		// (within the .text section size)
		if fn.offset >= textSection.Offset {
			relOffset := fn.offset - textSection.Offset
			assert.LessOrEqual(t, relOffset, textSection.Size,
				"offset for %s should be within .text section bounds", fn.name)
		}

		t.Logf("Function %s: offset=0x%x (correct file offset, not virtual address)",
			fn.name, fn.offset)
	}
}

// TestFilterFunctionsFromGoPclntab tests that symbol filtering works with .gopclntab
func TestFilterFunctionsFromGoPclntab(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "xcover-gopclntab-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a simple Go program to test with
	testProgramPath := filepath.Join(tmpDir, "testprogram.go")
	testProgram := `package main

import "fmt"

//go:noinline
func publicFunc() {
	fmt.Println("Public")
}

//go:noinline
func anotherPublicFunc() {
	fmt.Println("Another Public")
}

func main() {
	publicFunc()
	anotherPublicFunc()
}
`
	err = os.WriteFile(testProgramPath, []byte(testProgram), 0644)
	require.NoError(t, err)

	// Build and strip the test program
	binaryPath := filepath.Join(tmpDir, "testprogram")
	cmd := exec.Command("go", "build", "-o", binaryPath, testProgramPath)
	require.NoError(t, cmd.Run())

	cmd = exec.Command("strip", binaryPath)
	require.NoError(t, cmd.Run())

	t.Run("IncludeFilter", func(t *testing.T) {
		logger := zerolog.New(os.Stdout).Level(zerolog.DebugLevel)
		tracee := NewUserTracee(
			WithTraceeExePath(binaryPath),
			WithTraceeSymPatternInclude("main\\.publicFunc$"), // Only match exact publicFunc
			WithTraceeLogger(logger),
		)

		err := tracee.Init(t.Context())
		require.NoError(t, err)

		// Should only have main.publicFunc, not main.anotherPublicFunc
		foundPublic := false
		foundAnotherPublic := false
		for _, fn := range tracee.funcs {
			if fn.name == "main.publicFunc" {
				foundPublic = true
			}
			if fn.name == "main.anotherPublicFunc" {
				foundAnotherPublic = true
			}
		}
		assert.True(t, foundPublic, "should have found main.publicFunc")
		assert.False(t, foundAnotherPublic, "should not have found main.anotherPublicFunc")
	})

	t.Run("ExcludeFilter", func(t *testing.T) {
		logger := zerolog.New(os.Stdout).Level(zerolog.DebugLevel)
		tracee := NewUserTracee(
			WithTraceeExePath(binaryPath),
			WithTraceeSymPatternInclude("main\\."), // Include main package functions
			WithTraceeSymPatternExclude("another"), // Exclude anything with "another"
			WithTraceeLogger(logger),
		)

		err := tracee.Init(t.Context())
		require.NoError(t, err)

		// Should have main.publicFunc but not main.anotherPublicFunc
		foundPublic := false
		foundAnotherPublic := false
		for _, fn := range tracee.funcs {
			if fn.name == "main.publicFunc" {
				foundPublic = true
			}
			if fn.name == "main.anotherPublicFunc" {
				foundAnotherPublic = true
			}
		}
		assert.True(t, foundPublic, "should have found main.publicFunc")
		assert.False(t, foundAnotherPublic, "should not have found main.anotherPublicFunc (excluded)")
	})
}

// TestUserTracee_Init verifies end-to-end function resolution against the
// testdata binary, and that a nonexistent path surfaces os.ErrNotExist.
func TestUserTracee_Init(t *testing.T) {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	tracee := NewUserTracee(
		WithTraceeExePath(integrationTestBinary),
		WithTraceeLogger(logger),
		WithTraceeSymPatternExclude(integrationTestExcludedSyms),
	)
	err := tracee.Init(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, tracee.GetFuncNames())
	require.NotEmpty(t, tracee.GetFuncOffsets())
	require.NotEmpty(t, tracee.GetFuncCookies())

	tracee = NewUserTracee(
		WithTraceeExePath("nonexistent-binary-file"),
		WithTraceeLogger(logger),
		WithTraceeSymPatternExclude(integrationTestExcludedSyms),
	)
	err = tracee.Init(t.Context())
	require.Error(t, err)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestUserTracee_Init_NoMatch verifies that Init returns ErrNoFunctionSymbols
// when the include pattern matches nothing in the binary.
func TestUserTracee_Init_NoMatch(t *testing.T) {
	tracee := NewUserTracee(
		WithTraceeExePath(integrationTestBinary),
		WithTraceeSymPatternInclude("^nonexistentSymbol$"),
	)
	err := tracee.Init(t.Context())
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoFunctionSymbols)
}

// TestGoPclntab_Go126Compat verifies that the .gopclntab resolver produces
// correct file offsets on binaries compiled with Go 1.26+.
//
// Go 1.26 changed pcHeader.textStart to always be zero. This is safe for
// xcover because the resolver reads the text base directly from the ELF
// .text section header, never from pcHeader.
func TestGoPclntab_Go126Compat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xcover-gopclntab-126-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	src := filepath.Join(tmpDir, "main.go")
	err = os.WriteFile(src, []byte(`package main

import "fmt"

//go:noinline
func add(a, b int) int { return a + b }

//go:noinline
func greet(name string) { fmt.Println("hello", name) }

func main() { fmt.Println(add(1, 2)); greet("world") }
`), 0644)
	require.NoError(t, err)

	bin := filepath.Join(tmpDir, "bin")
	require.NoError(t, exec.Command("go", "build", "-o", bin, src).Run())
	require.NoError(t, exec.Command("strip", bin).Run())

	ef, err := elf.Open(bin)
	require.NoError(t, err)
	defer ef.Close()

	textSec := ef.Section(".text")
	require.NotNil(t, textSec)
	require.NotNil(t, ef.Section(".gopclntab"), ".gopclntab must survive strip")

	tracee := NewUserTracee(
		WithTraceeExePath(bin),
		WithTraceeSymPatternInclude(`^main\.`),
		WithTraceeLogger(zerolog.New(os.Stderr)),
	)
	require.NoError(t, tracee.Init(t.Context()))
	require.NotEmpty(t, tracee.funcs)

	for _, fn := range tracee.funcs {
		assert.GreaterOrEqual(t, fn.offset, textSec.Offset,
			"%s: offset should be >= .text file offset", fn.name)
		assert.Less(t, fn.offset, textSec.Offset+textSec.Size,
			"%s: offset should be within .text section", fn.name)
	}
}
