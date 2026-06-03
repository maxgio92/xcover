package trace

import (
	"testing"
)

func TestParseScope(t *testing.T) {
	tests := []struct {
		input   string
		want    Scope
		wantErr bool
	}{
		{"binary", ScopeBinary, false},
		{"project", ScopeProject, false},
		{"", "", true},
		{"unknown", "", true},
		{"Binary", "", true}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseScope(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseScope(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseScope(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
