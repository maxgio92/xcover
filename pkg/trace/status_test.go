package trace

import "testing"

func TestBufferUtilisation(t *testing.T) {
	tests := []struct {
		name     string
		n        int
		capacity int
		want     int
	}{
		{"empty", 0, 4096, 0},
		{"one event", 1, 4096, 0},
		{"half full", 2048, 4096, 50},
		{"one short of full", 4095, 4096, 99},
		{"full", 4096, 4096, 100},
		{"zero capacity", 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bufferUtilisation(tt.n, tt.capacity)
			if got != tt.want {
				t.Errorf("bufferUtilisation(%d, %d) = %d, want %d", tt.n, tt.capacity, got, tt.want)
			}
		})
	}
}
