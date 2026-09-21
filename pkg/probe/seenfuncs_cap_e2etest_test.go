//go:build e2etest

package probe

import "testing"

func TestSeenFuncsCapOverride(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{name: "unset", value: "", want: 0},
		{name: "positive", value: "1", want: 1},
		{name: "zero", value: "0", want: 0},
		{name: "negative", value: "-3", want: 0},
		{name: "garbage", value: "many", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(seenFuncsCapEnv, tt.value)
			if got := seenFuncsCapOverride(); got != tt.want {
				t.Fatalf("seenFuncsCapOverride() = %d, want %d", got, tt.want)
			}
		})
	}
}
