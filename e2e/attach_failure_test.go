//go:build e2e

package e2e

import (
	"debug/elf"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	attachFailureFunc = "main.main"
	attachErrorText   = "error attaching probe"
	runTracerErrText  = "failed to run tracer"
	// truncatedExeSize keeps the ELF header and the program headers of the
	// fixture (well under 4 KiB for a Go binary) and drops everything else.
	truncatedExeSize = 4096
)

// TestAttachFailureExitsBeforeReady makes the uprobe_multi attach fail and
// checks that the daemon never reports ready, logs the attach error and
// leaves no state files behind.
//
// The setup is a --debug-path pair where the executable is a copy of the Go
// fixture with its section header table dropped and the file truncated to
// its ELF and program headers, and the debug file is the intact fixture.
// The resolver reads main.main from the debug file's .symtab and maps its
// address through the executable's PT_LOAD headers, which the truncation
// leaves untouched, so it computes the original file offset. That offset
// lies past the end of the truncated file and the kernel rejects it with
// EINVAL in uprobe_register on every supported release, so attach fails
// before readiness. Section headers must go because debug/elf reads them
// from the end of the file, where the truncation would cut them.
func TestAttachFailureExitsBeforeReady(t *testing.T) {
	xcover := xcoverBinary(t)
	workDir := t.TempDir()
	debug := buildGoFixture(t, workDir, projectScopeGoScenario)
	exe := truncateExecutable(t, debug, filepath.Join(workDir, "truncated"))

	offset := fileOffsetOf(t, debug, attachFailureFunc)
	if offset <= truncatedExeSize {
		t.Fatalf("%s is at file offset %d, inside the truncated executable of %d bytes; the attach would not fail", attachFailureFunc, offset, truncatedExeSize)
	}
	t.Logf("%s file offset %d is past the %d byte executable", attachFailureFunc, offset, truncatedExeSize)

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
		"--path", exe, "--debug-path", debug, "--no-build-id-check",
		"--include=^" + attachFailureFunc + "$",
	}
	t.Logf("starting xcover daemon: %s", commandLine(xcover, args...))
	if out, err := commandOutput(workDir, 10*time.Second, xcover, args...); err != nil {
		t.Fatalf("%s failed: %v\n%s", commandLine(xcover, args...), err, out)
	}

	out, err := commandOutput(workDir, 20*time.Second, xcover, "wait", "--timeout=15s")
	if err == nil {
		_, _ = commandOutput(workDir, 10*time.Second, xcover, "stop", "--timeout=5s")
		t.Fatalf("xcover wait reported ready for a run whose attach must fail:\n%s\n%s", out, readSince(t, logFile, logOffset))
	}
	t.Logf("xcover wait failed as expected: %v", err)

	assertLogContains(t, logOffset, attachErrorText)
	assertLogContains(t, logOffset, runTracerErrText)

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

// truncateExecutable writes the first truncatedExeSize bytes of src to dst
// with the section header fields of the ELF header zeroed, so debug/elf
// opens the result and still exposes the program headers.
func truncateExecutable(t *testing.T, src, dst string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("failed to read %s: %v", src, err)
	}
	if len(data) <= truncatedExeSize || data[elf.EI_CLASS] != byte(elf.ELFCLASS64) {
		t.Fatalf("%s is not a 64-bit ELF larger than %d bytes", src, truncatedExeSize)
	}
	// ELF64 header: e_shoff at 0x28, e_shnum at 0x3c, e_shstrndx at 0x3e.
	binary.LittleEndian.PutUint64(data[0x28:], 0)
	binary.LittleEndian.PutUint16(data[0x3c:], 0)
	binary.LittleEndian.PutUint16(data[0x3e:], 0)
	if err := os.WriteFile(dst, data[:truncatedExeSize], 0o755); err != nil {
		t.Fatalf("failed to write %s: %v", dst, err)
	}
	f, err := elf.Open(dst)
	if err != nil {
		t.Fatalf("truncated executable %s does not parse: %v", dst, err)
	}
	f.Close()
	return dst
}

// fileOffsetOf returns the file offset of the named function in the ELF at
// path, mapped through its PT_LOAD headers the way the resolver does.
func fileOffsetOf(t *testing.T, path, name string) uint64 {
	t.Helper()
	f, err := elf.Open(path)
	if err != nil {
		t.Fatalf("failed to open %s: %v", path, err)
	}
	defer f.Close()
	syms, err := f.Symbols()
	if err != nil {
		t.Fatalf("failed to read symbols of %s: %v", path, err)
	}
	for _, sym := range syms {
		if sym.Name != name {
			continue
		}
		for _, prog := range f.Progs {
			if prog.Type == elf.PT_LOAD && sym.Value >= prog.Vaddr && sym.Value < prog.Vaddr+prog.Filesz {
				return sym.Value - prog.Vaddr + prog.Off
			}
		}
		t.Fatalf("%s at 0x%x is not covered by a PT_LOAD segment of %s", name, sym.Value, path)
	}
	t.Fatalf("%s has no symbol %s", path, name)
	return 0
}
