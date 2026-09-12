package probe

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"path/filepath"
	"testing"

	bpf "github.com/aquasecurity/libbpfgo"
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

func TestWithFuncCount(t *testing.T) {
	p := NewProbe(WithFuncCount(50000))
	if p.funcCount != 50000 {
		t.Errorf("funcCount = %d, want 50000", p.funcCount)
	}
	if p := NewProbe(); p.funcCount != 0 {
		t.Errorf("default funcCount = %d, want 0", p.funcCount)
	}
}

// openEmbeddedModule opens the embedded BPF object without loading it. Opening
// only parses the ELF, so it needs no privileges; map attributes can still be
// changed at this stage.
func openEmbeddedModule(t *testing.T) *bpf.Module {
	t.Helper()
	data, err := probeFS.ReadFile(filepath.Join(outputPath, ProbePath))
	require.NoError(t, err)
	mod, err := bpf.NewModuleFromBufferArgs(bpf.NewModuleArgs{
		BPFObjBuff:      data,
		BPFObjName:      ProgName,
		SkipMemlockBump: true,
	})
	require.NoError(t, err)
	t.Cleanup(mod.Close)
	return mod
}

// TestResizeSeenFuncs asserts the pre-load resize Init applies to seen_funcs:
// the capacity equals the traced function count exactly, and a non-positive
// count keeps the max_entries compiled into the object.
func TestResizeSeenFuncs(t *testing.T) {
	tests := []struct {
		name      string
		funcCount int
		want      func(compiled uint32) uint32
	}{
		{name: "zero keeps the object default", funcCount: 0, want: func(c uint32) uint32 { return c }},
		{name: "negative keeps the object default", funcCount: -1, want: func(c uint32) uint32 { return c }},
		{name: "exactly the function count", funcCount: 1, want: func(uint32) uint32 { return 1 }},
		{name: "above the object default", funcCount: 50000, want: func(uint32) uint32 { return 50000 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := openEmbeddedModule(t)
			seenFuncs, err := mod.GetMap(seenFuncsBPFMapName)
			require.NoError(t, err)
			compiled := seenFuncs.MaxEntries()
			require.NotZero(t, compiled)

			require.NoError(t, resizeSeenFuncs(seenFuncs, tt.funcCount))
			require.Equal(t, tt.want(compiled), seenFuncs.MaxEntries())
		})
	}
}

// TestEmbeddedObjectHasDropsMap pins the drops counter layout Probe.Drops
// reads: a one-slot array of u64.
func TestEmbeddedObjectHasDropsMap(t *testing.T) {
	mod := openEmbeddedModule(t)
	drops, err := mod.GetMap(dropsBPFMapName)
	require.NoError(t, err)
	require.Equal(t, bpf.MapTypeArray, drops.Type())
	require.Equal(t, uint32(1), drops.MaxEntries())
	require.Equal(t, 8, drops.ValueSize())
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

func TestNewProbePID(t *testing.T) {
	require.Equal(t, -1, NewProbe().pid, "default must trace every process")
	require.Equal(t, 1234, NewProbe(WithPID(1234)).pid)
}
