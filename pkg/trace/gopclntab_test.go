package trace

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadFunctionsFromGoPclntab tests that we can load function symbols
// from the .gopclntab section of a stripped Go binary
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

		err := tracee.Init()
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

		err := tracee.Init()
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
	})
}

// TestLoadFunctionsFromGoPclntab_NoGoPclntab tests that we handle non-Go binaries gracefully
func TestLoadFunctionsFromGoPclntab_NoGoPclntab(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "xcover-gopclntab-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a simple C program to test with
	testProgramPath := filepath.Join(tmpDir, "testprogram.c")
	testProgram := `#include <stdio.h>

void hello() {
    printf("Hello\\n");
}

int main() {
    hello();
    return 0;
}
`
	err = os.WriteFile(testProgramPath, []byte(testProgram), 0644)
	require.NoError(t, err)

	// Build the test program with gcc
	binaryPath := filepath.Join(tmpDir, "testprogram")
	cmd := exec.Command("gcc", "-o", binaryPath, testProgramPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("gcc not available or failed: %v, output: %s", err, output)
	}

	// Strip it
	cmd = exec.Command("strip", binaryPath)
	require.NoError(t, cmd.Run())

	// Try to load functions - should fail gracefully
	logger := zerolog.New(os.Stdout).Level(zerolog.DebugLevel)
	tracee := NewUserTracee(
		WithTraceeExePath(binaryPath),
		WithTraceeLogger(logger),
	)

	err = tracee.Init()
	assert.Error(t, err, "should fail to load functions from non-Go stripped binary")
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

	err = tracee.Init()
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

		err := tracee.Init()
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

		err := tracee.Init()
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
