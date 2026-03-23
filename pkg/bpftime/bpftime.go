// Package bpftime provides support for running xcover with the bpftime
// userspace BPF runtime (https://github.com/eunomia-bpf/bpftime).
//
// In userspace BPF mode, BPF programs execute entirely in userspace via the
// bpftime runtime, eliminating the kernel trap cost on every traced function
// call. Two shared libraries are involved:
//
//   - bpftime-syscall-server.so: intercepted into the xcover process itself.
//     It intercepts BPF syscalls so that map and program management stays in
//     userspace. Injected at startup via memfd and self re-exec.
//
//   - bpftime-agent.so: injected into the tracee process. It handles uprobe
//     hits in userspace. In the short term, the user must preload it manually
//     via LD_PRELOAD; xcover provides the ExtractAgent helper to obtain the
//     path to the extracted library.
//
// # Build note
//
// The .so files under libs/ are placeholders. Replace them with libraries
// built from the bpftime source tree before use:
//
//	git clone https://github.com/eunomia-bpf/bpftime
//	cd bpftime && cmake -B build -DCMAKE_BUILD_TYPE=Release && cmake --build build
//	cp build/runtime/syscall-server/libbpftime-syscall-server.so \
//	   <xcover>/pkg/bpftime/libs/bpftime-syscall-server.so
//	cp build/runtime/agent/libbpftime-agent.so \
//	   <xcover>/pkg/bpftime/libs/bpftime-agent.so
package bpftime

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

//go:embed libs/bpftime-syscall-server.so
var syscallServerLib []byte

//go:embed libs/bpftime-agent.so
var agentLib []byte

const (
	// envSentinel is set in the environment after a successful re-exec so
	// that the second invocation does not re-exec again indefinitely.
	envSentinel = "XCOVER_BPFTIME_LOADED"

	syscallServerMemfdName = "bpftime-syscall-server"
	agentMemfdName         = "bpftime-agent"
	agentTempPattern       = "bpftime-agent-*.so"
)

// EnsureSyscallServer injects the bpftime syscall-server library into the
// current process if it has not been loaded yet. Injection is achieved by
// writing the embedded library into an anonymous memfd and re-executing the
// current process with LD_PRELOAD set to /proc/self/fd/<n>.
//
// The function is a no-op when the XCOVER_BPFTIME_LOADED sentinel is already
// set in the environment (i.e. we are in the re-exec'd process).
//
// Must be called before any BPF operations are performed.
func EnsureSyscallServer() error {
	if os.Getenv(envSentinel) == "1" {
		return nil
	}

	// Write the library into a memfd. Do NOT set MFD_CLOEXEC so the fd
	// survives the exec below.
	fd, err := unix.MemfdCreate(syscallServerMemfdName, 0)
	if err != nil {
		return fmt.Errorf("bpftime: memfd_create: %w", err)
	}

	if _, err := unix.Write(fd, syscallServerLib); err != nil {
		unix.Close(fd)
		return fmt.Errorf("bpftime: write syscall-server to memfd: %w", err)
	}

	ldPreload := fmt.Sprintf("/proc/self/fd/%d", fd)

	// Build the new environment: prepend our lib to LD_PRELOAD, add sentinel.
	env := os.Environ()
	newEnv := make([]string, 0, len(env)+2)
	for _, e := range env {
		if !strings.HasPrefix(e, "LD_PRELOAD=") {
			newEnv = append(newEnv, e)
		} else {
			// Preserve any existing LD_PRELOAD entries.
			existing := strings.TrimPrefix(e, "LD_PRELOAD=")
			ldPreload = ldPreload + ":" + existing
		}
	}
	newEnv = append(newEnv, fmt.Sprintf("LD_PRELOAD=%s", ldPreload))
	newEnv = append(newEnv, fmt.Sprintf("%s=1", envSentinel))

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("bpftime: resolve executable path: %w", err)
	}

	// Replace the current process image. On success this never returns.
	if err := unix.Exec(self, os.Args, newEnv); err != nil {
		return fmt.Errorf("bpftime: re-exec: %w", err)
	}

	return nil
}

// ExtractAgent writes the embedded bpftime agent library to a temporary file
// and returns its path. The caller is responsible for removing the file when
// it is no longer needed.
//
// The returned path is intended for use as LD_PRELOAD in the tracee
// environment:
//
//	path, err := bpftime.ExtractAgent()
//	fmt.Printf("LD_PRELOAD=%s\n", path)
func ExtractAgent() (string, error) {
	f, err := os.CreateTemp("", agentTempPattern)
	if err != nil {
		return "", fmt.Errorf("bpftime: create temp file for agent: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(agentLib); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("bpftime: write agent library: %w", err)
	}

	// Make the library executable.
	if err := f.Chmod(0755); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("bpftime: chmod agent library: %w", err)
	}

	return f.Name(), nil
}
