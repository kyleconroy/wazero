package binary

import (
	"testing"
)

func TestIsComponent(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected bool
	}{
		{
			name:     "nil data",
			data:     nil,
			expected: false,
		},
		{
			name:     "too short",
			data:     []byte{0x00, 0x61, 0x73},
			expected: false,
		},
		{
			name:     "core wasm module",
			data:     []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00},
			expected: false,
		},
		{
			name:     "component (version 13 layer 1)",
			data:     []byte{0x00, 0x61, 0x73, 0x6D, 0x0d, 0x00, 0x01, 0x00},
			expected: true,
		},
		{
			name:     "component with trailing data",
			data:     []byte{0x00, 0x61, 0x73, 0x6D, 0x0d, 0x00, 0x01, 0x00, 0xFF, 0xFF},
			expected: true,
		},
		{
			name:     "wrong magic",
			data:     []byte{0xFF, 0x61, 0x73, 0x6D, 0x0d, 0x00, 0x01, 0x00},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsComponent(tt.data); got != tt.expected {
				t.Errorf("IsComponent() = %v, want %v", got, tt.expected)
			}
		})
	}
}
