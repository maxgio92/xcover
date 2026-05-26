//go:build linux

package benchmark

import (
	"os"
	"os/exec"
)

const (
	hitBinary  = "./target/hit/hit"
	idleBinary = "./target/idle/idle"
	missBinary = "./target/miss/miss"
)

// buildTargets runs make in the benchmark directory to compile all C targets.
func buildTargets() error {
	cmd := exec.Command("make", "all")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
