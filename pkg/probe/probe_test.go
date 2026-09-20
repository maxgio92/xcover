package probe

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	bpf "github.com/aquasecurity/libbpfgo"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
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
// bpf_printk off the hot path: no helper call to BPF_FUNC_trace_printk (6) or
// BPF_FUNC_trace_vprintk (177) in the uprobe program. Only insns with
// src_reg 0 are helper calls; src_reg 1 marks a BPF-to-BPF pseudo call.
// Build with BPF_DEBUG=1 to opt in.
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

	const bpfCall, tracePrintk, traceVprintk = 0x85, 6, 177
	for i := 0; i+8 <= len(code); i += 8 {
		if code[i] != bpfCall || code[i+1]>>4 != 0 {
			continue
		}
		switch helper := binary.LittleEndian.Uint32(code[i+4 : i+8]); helper {
		case tracePrintk, traceVprintk:
			t.Fatalf("default BPF object calls printk helper %d at insn %d", helper, i/8)
		}
	}
}

func TestNewProbePID(t *testing.T) {
	require.Equal(t, -1, NewProbe().pid, "default must trace every process")
	require.Equal(t, 1234, NewProbe(WithPID(1234)).pid)
}

func TestWithRingBufSize(t *testing.T) {
	require.Equal(t, uint32(1<<20), NewProbe(WithRingBufSize(1<<20)).RingBufSize())
	require.Equal(t, uint32(0), NewProbe().RingBufSize(), "default must keep the compiled size")
}

// TestValidateRingBufSize pins the rule the parent enforces before the daemon
// forks: the kernel rejects a ring buffer that is not a power-of-two multiple
// of the page size with EINVAL, and libbpf would otherwise round the value up
// silently. Every refusal names the rule.
func TestValidateRingBufSize(t *testing.T) {
	pageSize := os.Getpagesize()
	rule := fmt.Sprintf("must be a power of two, a multiple of the %d byte page size, greater than zero and at most %d", pageSize, uint64(1)<<31)
	tests := []struct {
		name    string
		size    uint64
		wantErr bool
	}{
		{name: "zero is refused", size: 0, wantErr: true},
		{name: "not a power of two is refused", size: 102400, wantErr: true},
		{name: "below the page size is refused", size: 2048, wantErr: true},
		{name: "one page is accepted", size: uint64(pageSize)},
		{name: "the 2 GiB bound is accepted", size: 1 << 31},
		{name: "above the bound is refused", size: 1 << 32, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRingBufSize(tt.size)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, fmt.Sprintf("invalid ring buffer size %d: %s", tt.size, rule))
		})
	}
}

// TestResizeEventsRingBuf asserts the pre-load resize Init applies to the
// events ring buffer: zero keeps the max_entries compiled into the object and
// a valid size replaces it unchanged.
func TestResizeEventsRingBuf(t *testing.T) {
	tests := []struct {
		name string
		size uint32
		want func(compiled uint32) uint32
	}{
		{name: "zero keeps the object default", size: 0, want: func(c uint32) uint32 { return c }},
		{name: "a valid size replaces the default", size: 1 << 20, want: func(uint32) uint32 { return 1 << 20 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := openEmbeddedModule(t)
			events, err := mod.GetMap(evtRingBufBPFMapName)
			require.NoError(t, err)
			compiled := events.MaxEntries()
			require.NotZero(t, compiled)

			require.NoError(t, resizeEventsRingBuf(events, tt.size))
			require.Equal(t, tt.want(compiled), events.MaxEntries())
		})
	}
}

// TestEmbeddedObjectRingBufDefault pins the events map the object ships with:
// a 16 MiB BPF ring buffer, which the default --ringbuf-size mirrors.
func TestEmbeddedObjectRingBufDefault(t *testing.T) {
	mod := openEmbeddedModule(t)
	events, err := mod.GetMap(evtRingBufBPFMapName)
	require.NoError(t, err)
	require.Equal(t, bpf.MapTypeRingbuf, events.Type())
	require.Equal(t, uint32(1<<24), events.MaxEntries())
}

// TestLoadError pins the load failure text: the size the kernel was asked to
// allocate is always named, the --ringbuf-size hint follows ENOMEM only, and
// the errno stays reachable through errors.Is. The cause mirrors libbpfgo's
// BPFLoadObject wrapping ("failed to load BPF object: %w").
func TestLoadError(t *testing.T) {
	tests := []struct {
		name  string
		errno unix.Errno
		want  string
	}{
		{
			name:  "ENOMEM names the size and hints at the flag",
			errno: unix.ENOMEM,
			want:  "failed to load bpf module handle_user_function with a 16777216 byte events ring buffer: failed to load BPF object: cannot allocate memory; lower --ringbuf-size",
		},
		{
			name:  "EPERM names the size without the hint",
			errno: unix.EPERM,
			want:  "failed to load bpf module handle_user_function with a 16777216 byte events ring buffer: failed to load BPF object: operation not permitted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cause := fmt.Errorf("failed to load BPF object: %w", tt.errno)
			err := loadError(ProgName, 1<<24, cause)
			require.EqualError(t, err, tt.want)
			require.ErrorIs(t, err, tt.errno)
		})
	}
}
