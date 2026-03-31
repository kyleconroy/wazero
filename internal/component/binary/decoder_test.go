package binary

import (
	"testing"

	"github.com/tetratelabs/wazero/internal/component"
	"github.com/tetratelabs/wazero/internal/testing/require"
)

func TestIsComponent(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected bool
	}{
		{
			name:     "valid component",
			data:     append(ComponentMagic, ComponentVersion...),
			expected: true,
		},
		{
			name:     "core module (not component)",
			data:     []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00},
			expected: false,
		},
		{
			name:     "too short",
			data:     []byte{0x00, 0x61, 0x73},
			expected: false,
		},
		{
			name:     "empty",
			data:     []byte{},
			expected: false,
		},
		{
			name:     "wrong magic",
			data:     []byte{0x01, 0x02, 0x03, 0x04, 0x0d, 0x00, 0x01, 0x00},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, IsComponent(tt.data))
		})
	}
}

func TestDecodeComponent_Empty(t *testing.T) {
	// An empty component with just the header.
	data := append(ComponentMagic, ComponentVersion...)

	c, err := DecodeComponent(data)
	require.NoError(t, err)
	require.Equal(t, 0, len(c.CoreModules))
	require.Equal(t, 0, len(c.Imports))
	require.Equal(t, 0, len(c.Exports))
}

func TestDecodeComponent_InvalidMagic(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x0d, 0x00, 0x01, 0x00}
	_, err := DecodeComponent(data)
	require.Error(t, err)
	require.Equal(t, ErrInvalidMagicNumber, err)
}

func TestDecodeComponent_InvalidVersion(t *testing.T) {
	// Core module version instead of component version.
	data := []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}
	_, err := DecodeComponent(data)
	require.Error(t, err)
	require.Equal(t, ErrInvalidVersion, err)
}

func TestDecodeComponent_HeaderConstants(t *testing.T) {
	// Verify the header constants match the component model spec.
	require.Equal(t, []byte{0x00, 0x61, 0x73, 0x6D}, ComponentMagic)
	require.Equal(t, []byte{0x0d, 0x00, 0x01, 0x00}, ComponentVersion)
}

func TestPrimitiveValTypeName(t *testing.T) {
	tests := []struct {
		prim component.PrimitiveValType
		name string
	}{
		{component.PrimitiveValTypeBool, "bool"},
		{component.PrimitiveValTypeS8, "s8"},
		{component.PrimitiveValTypeU8, "u8"},
		{component.PrimitiveValTypeS16, "s16"},
		{component.PrimitiveValTypeU16, "u16"},
		{component.PrimitiveValTypeS32, "s32"},
		{component.PrimitiveValTypeU32, "u32"},
		{component.PrimitiveValTypeS64, "s64"},
		{component.PrimitiveValTypeU64, "u64"},
		{component.PrimitiveValTypeF32, "f32"},
		{component.PrimitiveValTypeF64, "f64"},
		{component.PrimitiveValTypeChar, "char"},
		{component.PrimitiveValTypeString, "string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.name, component.PrimitiveValTypeName(tt.prim))
		})
	}
}

func TestIsPrimitiveValType(t *testing.T) {
	// Primitives are in range 0x73-0x7f.
	require.True(t, isPrimitiveValType(0x73)) // string
	require.True(t, isPrimitiveValType(0x7f)) // bool
	require.True(t, isPrimitiveValType(0x76)) // f32

	require.False(t, isPrimitiveValType(0x72)) // record (below range)
	require.False(t, isPrimitiveValType(0x40)) // func type
	require.False(t, isPrimitiveValType(0x00)) // zero
}
