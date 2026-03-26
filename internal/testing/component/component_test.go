package component_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tetratelabs/wazero/internal/testing/require"
	componentBinary "github.com/tetratelabs/wazero/internal/wasm/component/binary"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return data
}

// TestDecodeMinimalComponent verifies that we can parse a minimal component
// that has no imports, one core module, and one export.
func TestDecodeMinimalComponent(t *testing.T) {
	data := readTestdata(t, "minimal-component.wasm")

	require.True(t, componentBinary.IsComponent(data))

	comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
	require.NoError(t, err)
	require.NotNil(t, comp)

	// Minimal component has one core module.
	require.Equal(t, 1, len(comp.CoreModules))

	// One core instance (instantiate).
	require.Equal(t, 1, len(comp.CoreInstances))

	// One type (func returning u32).
	require.Equal(t, 1, len(comp.Types))

	// One alias (core export "hello").
	require.Equal(t, 1, len(comp.Aliases))

	// One canon lift.
	require.Equal(t, 1, len(comp.Canons))

	// One export "hello".
	require.Equal(t, 1, len(comp.Exports))
	require.Equal(t, "hello", comp.Exports[0].Name)

	// No imports.
	require.Equal(t, 0, len(comp.Imports))
}

// TestDecodeHelloStdoutComponent verifies parsing a component with two
// exports: "run" (no return) and "get-message" (returns string).
func TestDecodeHelloStdoutComponent(t *testing.T) {
	data := readTestdata(t, "hello-stdout-component.wasm")

	require.True(t, componentBinary.IsComponent(data))

	comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
	require.NoError(t, err)
	require.NotNil(t, comp)

	// One core module.
	require.Equal(t, 1, len(comp.CoreModules))

	// Verify the core module has "memory", "run", "get-message" exports.
	coreMod := comp.CoreModules[0]
	exportNames := make(map[string]bool)
	for _, exp := range coreMod.ExportSection {
		exportNames[exp.Name] = true
	}
	require.True(t, exportNames["memory"])
	require.True(t, exportNames["run"])
	require.True(t, exportNames["get-message"])

	// Two component-level exports: "run" and "get-message".
	require.Equal(t, 2, len(comp.Exports))

	foundRun := false
	foundGetMessage := false
	for _, exp := range comp.Exports {
		switch exp.Name {
		case "run":
			foundRun = true
		case "get-message":
			foundGetMessage = true
		}
	}
	require.True(t, foundRun)
	require.True(t, foundGetMessage)

	// No imports.
	require.Equal(t, 0, len(comp.Imports))
}

// TestDecodeRealP2Component verifies that we can parse a real Rust-compiled
// WASI P2 component. This component was built with `cargo build --target wasm32-wasip2`.
func TestDecodeRealP2Component(t *testing.T) {
	data := readTestdata(t, "hello-p2.wasm")

	require.True(t, componentBinary.IsComponent(data))

	comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
	require.NoError(t, err)
	require.NotNil(t, comp)

	// Real P2 component should have core modules (main module + adapter).
	require.True(t, len(comp.CoreModules) > 0)

	// Should have imports (wasi:io/error, wasi:io/streams, wasi:cli/*, etc.).
	require.True(t, len(comp.Imports) > 0)

	// Verify some expected WASI P2 imports are present.
	importNames := make(map[string]bool)
	for _, imp := range comp.Imports {
		importNames[imp.Name] = true
	}

	// Real Rust components use @0.2.6 versions.
	require.True(t, importNames["wasi:io/error@0.2.6"])
	require.True(t, importNames["wasi:io/streams@0.2.6"])
	require.True(t, importNames["wasi:cli/environment@0.2.6"])
	require.True(t, importNames["wasi:cli/exit@0.2.6"])
	require.True(t, importNames["wasi:cli/stdout@0.2.6"])
	require.True(t, importNames["wasi:cli/stderr@0.2.6"])

	// Should have types, aliases, canons, core instances.
	require.True(t, len(comp.Types) > 0)
	require.True(t, len(comp.Aliases) > 0)
	require.True(t, len(comp.Canons) > 0)
	require.True(t, len(comp.CoreInstances) > 0)
}

// TestDecodeMinimalComponentCoreModule verifies the embedded core module structure.
func TestDecodeMinimalComponentCoreModule(t *testing.T) {
	data := readTestdata(t, "minimal-component.wasm")

	comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
	require.NoError(t, err)

	coreMod := comp.CoreModules[0]

	// Core module should have a memory export.
	var hasMemory bool
	for _, exp := range coreMod.ExportSection {
		if exp.Name == "memory" {
			hasMemory = true
			break
		}
	}
	require.True(t, hasMemory)

	// Core module should have a "hello" function export.
	var hasHello bool
	for _, exp := range coreMod.ExportSection {
		if exp.Name == "hello" {
			hasHello = true
			break
		}
	}
	require.True(t, hasHello)
}

// TestDecodeRealP2ComponentStructure verifies the structural elements of a
// real WASI P2 component in more detail.
func TestDecodeRealP2ComponentStructure(t *testing.T) {
	data := readTestdata(t, "hello-p2.wasm")

	comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
	require.NoError(t, err)

	// The Rust-compiled P2 component has multiple core modules
	// (the main module and the wasi_snapshot_preview1 adapter).
	t.Logf("Core modules: %d", len(comp.CoreModules))
	t.Logf("Core instances: %d", len(comp.CoreInstances))
	t.Logf("Aliases: %d", len(comp.Aliases))
	t.Logf("Types: %d", len(comp.Types))
	t.Logf("Canons: %d", len(comp.Canons))
	t.Logf("Imports: %d", len(comp.Imports))
	t.Logf("Exports: %d", len(comp.Exports))

	// Should have at least 2 core modules (main + adapter).
	require.True(t, len(comp.CoreModules) >= 2)

	// Verify canon operations exist (lift/lower for bridging core <-> component).
	hasLift := false
	hasLower := false
	for _, c := range comp.Canons {
		switch c.Kind {
		case 0x00: // lift
			hasLift = true
		case 0x01: // lower
			hasLower = true
		}
	}
	require.True(t, hasLift || hasLower, "expected at least one canon lift or lower")
}
