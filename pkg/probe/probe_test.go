package probe

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsNoisyAttachFailure(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{
			name: "multi-uprobe attach failure",
			msg:  "libbpf: prog 'handle_user_function': failed to attach multi-uprobe: Invalid argument",
			want: true,
		},
		{
			name: "legacy uprobe event registration failure",
			msg:  "libbpf: failed to add legacy uprobe event for /bin/foo:0x1000: -2",
			want: true,
		},
		{
			name: "legacy uprobe event id lookup failure",
			msg:  "libbpf: failed to determine legacy uprobe event id for /bin/foo:0x1000: -2",
			want: true,
		},
		{
			name: "legacy uprobe perf_event_open failure",
			msg:  "libbpf: legacy uprobe perf_event_open() failed: -1",
			want: true,
		},
		{
			name: "unrelated warning is not filtered",
			msg:  "libbpf: elf: failed to open /bin/foo as ELF file: invalid data",
			want: false,
		},
		{
			name: "empty message is not filtered",
			msg:  "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNoisyAttachFailure(tt.msg); got != tt.want {
				t.Errorf("isNoisyAttachFailure(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

// TestEmbeddedObjectHasNoPrintk pins that the default BPF object keeps
// bpf_printk off the hot path: no `call 6` (BPF_FUNC_trace_printk) in the
// uprobe program. Build with BPF_DEBUG=1 to opt in.
func TestEmbeddedObjectHasNoPrintk(t *testing.T) {
	data, err := probeFS.ReadFile(filepath.Join(outputPath, ProbePath))
	require.NoError(t, err)

	f, err := elf.NewFile(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	sec := f.Section("uprobe/" + ProgName)
	require.NotNil(t, sec, "program section not found")
	code, err := sec.Data()
	require.NoError(t, err)

	const bpfCall, tracePrintk = 0x85, 6
	for i := 0; i+8 <= len(code); i += 8 {
		if code[i] == bpfCall && binary.LittleEndian.Uint32(code[i+4:i+8]) == tracePrintk {
			t.Fatalf("default BPF object calls trace_printk at insn %d", i/8)
		}
	}
}
