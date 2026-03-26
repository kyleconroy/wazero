package wazero

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero/internal/testing/require"
	binaryformat "github.com/tetratelabs/wazero/internal/wasm/binary"
)

func TestIsComponent(t *testing.T) {
	// Core wasm module
	coreModule := []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}
	require.False(t, binaryformat.IsComponent(coreModule))

	// Component
	component := []byte{0x00, 0x61, 0x73, 0x6D, 0x0d, 0x00, 0x01, 0x00}
	require.True(t, binaryformat.IsComponent(component))
}

func TestCompileComponent_Empty(t *testing.T) {
	ctx := context.Background()
	r := NewRuntime(ctx)
	defer r.Close(ctx)

	// Minimal empty component
	data := []byte{0x00, 0x61, 0x73, 0x6D, 0x0d, 0x00, 0x01, 0x00}

	rt := r.(*runtime)
	compiled, err := rt.CompileComponent(ctx, data)
	require.NoError(t, err)
	require.NotNil(t, compiled)

	require.Equal(t, 0, len(compiled.Imports()))
	require.Equal(t, 0, len(compiled.Exports()))
}

func TestCompileComponent_InvalidMagic(t *testing.T) {
	ctx := context.Background()
	r := NewRuntime(ctx)
	defer r.Close(ctx)

	data := []byte{0xFF, 0x00, 0x00, 0x00, 0x0d, 0x00, 0x01, 0x00}

	rt := r.(*runtime)
	_, err := rt.CompileComponent(ctx, data)
	require.Error(t, err)
}

func TestCompileComponent_CoreModuleVersion(t *testing.T) {
	ctx := context.Background()
	r := NewRuntime(ctx)
	defer r.Close(ctx)

	// Core module is not a component
	data := []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}

	rt := r.(*runtime)
	_, err := rt.CompileComponent(ctx, data)
	require.Error(t, err)
}
