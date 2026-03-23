// Package inject provides bpftime-based agent injection into a live tracee
// process. It extracts the embedded bpftime CLI and agent library to a user
// cache directory and invokes 'bpftime attach <pid>' to perform the injection
// via Frida Core (abstracted by bpftime's CLI).
package inject

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/maxgio92/xcover/pkg/bpftime"
)

const (
	agentLibName  = "libbpftime-agent.so"
	bptimeBinName = "bpftime"
)

// InjectAgent injects the bpftime agent into the process identified by pid
// using the bpftime CLI's 'attach' subcommand. The embedded bpftime binary
// and agent library are extracted to the user cache directory on first use
// and reused on subsequent calls.
//
// The target process must have dlopen available in its address space. See
// docs/xcover_userspace_bpf.md for the binary support matrix.
func InjectAgent(pid int) error {
	dir, err := prepareInstallDir()
	if err != nil {
		return err
	}

	bptimePath := filepath.Join(dir, bptimeBinName)
	cmd := exec.Command(bptimePath, "-i", dir, "attach", strconv.Itoa(pid))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("inject: bpftime attach pid %d: %w", pid, err)
	}

	return nil
}

// prepareInstallDir ensures the bpftime binary and agent library are extracted
// to the user cache directory and returns the directory path.
func prepareInstallDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("inject: resolve user cache dir: %w", err)
	}

	dir := filepath.Join(cacheDir, "xcover", "bpftime")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("inject: create install dir: %w", err)
	}

	if err := extractIfMissing(filepath.Join(dir, bptimeBinName), bpftime.BptimeBinBytes(), 0755); err != nil {
		return "", fmt.Errorf("inject: extract bpftime binary: %w", err)
	}

	if err := extractIfMissing(filepath.Join(dir, agentLibName), bpftime.AgentLibBytes(), 0755); err != nil {
		return "", fmt.Errorf("inject: extract agent library: %w", err)
	}

	return dir, nil
}

// extractIfMissing writes data to path with the given mode only if the file
// does not already exist.
func extractIfMissing(path string, data []byte, mode os.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		return nil // already extracted
	}

	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}
