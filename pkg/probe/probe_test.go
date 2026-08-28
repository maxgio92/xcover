package probe

import "testing"

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
