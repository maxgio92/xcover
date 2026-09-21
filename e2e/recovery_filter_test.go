//go:build e2e

package e2e

import (
	"debug/elf"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	recoveryFilterFunc    = "main"
	filterNeedsSymbolsErr = "symbol filters need a symbol table"
	initTracerErrText     = "failed to init tracer"
)

// TestRecoveryRefusesFiltersBeforeReady runs xcover with --include on a
// binary that has neither .symtab nor .gopclntab and checks that the daemon
// never reports ready, logs the ErrFilterNeedsSymbols refusal and leaves no
// state files behind.
//
// The fixture is the C++ demangle program stripped with strip(1), which
// removes .symtab; a C++ binary carries no .gopclntab. Without the refusal
// xcover would fall back to function recovery, which names functions
// func_0x<offset> so the include pattern would silently match nothing.
func TestRecoveryRefusesFiltersBeforeReady(t *testing.T) {
	xcover := xcoverBinary(t)
	workDir := t.TempDir()
	bin := stripFixture(t, buildCppFixture(t, workDir, demangleCppScenario))

	// A run that fails before readiness removes its own state files. If it
	// did not, stop a daemon that is still alive, then remove the files, so
	// later tests do not skip on stale state.
	t.Cleanup(func() {
		if _, err := os.Stat(pidFile); err == nil {
			_, _ = commandOutput(workDir, 10*time.Second, xcover, "stop", "--timeout=5s")
		}
		for _, path := range []string{pidFile, socketFile} {
			_ = os.Remove(path)
		}
	})

	logOffset := fileSize(t, logFile)
	args := []string{
		"--log-level=debug", "run", "--detach", "--status=false",
		"--path", bin, "--include=^" + recoveryFilterFunc + "$",
	}
	t.Logf("starting xcover daemon: %s", commandLine(xcover, args...))
	if out, err := commandOutput(workDir, 10*time.Second, xcover, args...); err != nil {
		t.Fatalf("%s failed: %v\n%s", commandLine(xcover, args...), err, out)
	}

	out, err := commandOutput(workDir, 20*time.Second, xcover, "wait", "--timeout=15s")
	if err == nil {
		_, _ = commandOutput(workDir, 10*time.Second, xcover, "stop", "--timeout=5s")
		t.Fatalf("xcover wait reported ready for a run that must refuse its filter:\n%s\n%s", out, readSince(t, logFile, logOffset))
	}
	t.Logf("xcover wait failed as expected: %v", err)

	assertLogContains(t, logOffset, filterNeedsSymbolsErr)
	assertLogContains(t, logOffset, initTracerErrText)

	// The daemon exits right after the wait failure; give its cleanup a
	// moment before checking the state files.
	deadline := time.Now().Add(5 * time.Second)
	for _, path := range []string{pidFile, socketFile} {
		for {
			_, err := os.Stat(path)
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			if err != nil {
				t.Fatalf("failed to inspect %s: %v", path, err)
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s still exists after the failed run", path)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// stripFixture strips bin in place with strip(1) and checks the result has
// neither .symtab nor .gopclntab, so the resolver returns ErrNoSymbolTable.
// Skips when strip is missing, like buildCppFixture does for g++.
func stripFixture(t *testing.T, bin string) string {
	t.Helper()
	if _, err := exec.LookPath("strip"); err != nil {
		skipOrFail(t, "strip is not installed: %v", err)
	}
	runCommand(t, filepath.Dir(bin), 10*time.Second, "strip", bin)

	f, err := elf.Open(bin)
	if err != nil {
		t.Fatalf("stripped fixture %s does not parse: %v", bin, err)
	}
	for _, name := range []string{".symtab", ".gopclntab"} {
		if f.Section(name) != nil {
			t.Fatalf("stripped fixture %s still has a %s section", bin, name)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("failed to close %s: %v", bin, err)
	}
	return bin
}
