//go:build e2e

package e2e

import (
	"testing"

	"github.com/pkg/errors"

	"github.com/maxgio92/xcover/internal/preflight"
)

func TestShouldSkipForRuntimeEnvironment(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "EPERM from the kernel", output: "libbpf: Operation not permitted", want: true},
		{name: "EACCES on a bpffs path", output: "open /sys/fs/bpf: Permission denied", want: true},
		{name: "libbpf load failure", output: "Error: failed to load BPF object", want: true},
		{name: "probe init failure", output: "error initializing BPF probe", want: true},
		{name: "preflight capability check", output: "Error: " + errors.Wrap(preflight.ErrMissingCapabilities, "CAP_BPF not in the effective set").Error(), want: true},
		{name: "unrelated failure", output: "coverage 0.00% is below the expected 42%", want: false},
		{name: "empty output", output: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSkipForRuntimeEnvironment(tt.output); got != tt.want {
				t.Fatalf("shouldSkipForRuntimeEnvironment(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}
