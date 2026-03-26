package component_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

	require.Equal(t, 1, len(comp.CoreModules))
	require.Equal(t, 1, len(comp.CoreInstances))
	require.Equal(t, 1, len(comp.Types))
	require.Equal(t, 1, len(comp.Aliases))
	require.Equal(t, 1, len(comp.Canons))
	require.Equal(t, 1, len(comp.Exports))
	require.Equal(t, "hello", comp.Exports[0].Name)
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

	require.Equal(t, 1, len(comp.CoreModules))

	coreMod := comp.CoreModules[0]
	exportNames := make(map[string]bool)
	for _, exp := range coreMod.ExportSection {
		exportNames[exp.Name] = true
	}
	require.True(t, exportNames["memory"])
	require.True(t, exportNames["run"])
	require.True(t, exportNames["get-message"])

	require.Equal(t, 2, len(comp.Exports))
	require.Equal(t, 0, len(comp.Imports))
}

// TestDecodeRealP2Component verifies that we can parse a real Rust-compiled
// WASI P2 component built with `cargo build --target wasm32-wasip2`.
func TestDecodeRealP2Component(t *testing.T) {
	data := readTestdata(t, "hello-p2.wasm")

	require.True(t, componentBinary.IsComponent(data))

	comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
	require.NoError(t, err)
	require.NotNil(t, comp)

	require.True(t, len(comp.CoreModules) > 0)
	require.True(t, len(comp.Imports) > 0)

	importNames := make(map[string]bool)
	for _, imp := range comp.Imports {
		importNames[imp.Name] = true
	}
	require.True(t, importNames["wasi:io/error@0.2.6"])
	require.True(t, importNames["wasi:io/streams@0.2.6"])
	require.True(t, importNames["wasi:cli/environment@0.2.6"])
	require.True(t, importNames["wasi:cli/exit@0.2.6"])
	require.True(t, importNames["wasi:cli/stdout@0.2.6"])
	require.True(t, importNames["wasi:cli/stderr@0.2.6"])

	require.True(t, len(comp.Types) > 0)
	require.True(t, len(comp.Aliases) > 0)
	require.True(t, len(comp.Canons) > 0)
	require.True(t, len(comp.CoreInstances) > 0)
}

func TestDecodeMinimalComponentCoreModule(t *testing.T) {
	data := readTestdata(t, "minimal-component.wasm")

	comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
	require.NoError(t, err)

	coreMod := comp.CoreModules[0]
	var hasMemory, hasHello bool
	for _, exp := range coreMod.ExportSection {
		switch exp.Name {
		case "memory":
			hasMemory = true
		case "hello":
			hasHello = true
		}
	}
	require.True(t, hasMemory)
	require.True(t, hasHello)
}

func TestDecodeRealP2ComponentStructure(t *testing.T) {
	data := readTestdata(t, "hello-p2.wasm")

	comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
	require.NoError(t, err)

	t.Logf("Core modules: %d", len(comp.CoreModules))
	t.Logf("Core instances: %d", len(comp.CoreInstances))
	t.Logf("Aliases: %d", len(comp.Aliases))
	t.Logf("Types: %d", len(comp.Types))
	t.Logf("Canons: %d", len(comp.Canons))
	t.Logf("Imports: %d", len(comp.Imports))
	t.Logf("Exports: %d", len(comp.Exports))

	require.True(t, len(comp.CoreModules) >= 2)

	hasLift := false
	hasLower := false
	for _, c := range comp.Canons {
		switch c.Kind {
		case 0x00:
			hasLift = true
		case 0x01:
			hasLower = true
		}
	}
	require.True(t, hasLift || hasLower, "expected at least one canon lift or lower")
}

// ---------------------------------------------------------------------------
// Official WASI Test Suite (WebAssembly/wasi-testsuite) - P3 Components
// ---------------------------------------------------------------------------

// TestWASITestSuite_P3_DecodeAll verifies that every precompiled WASI P3
// component from the official test suite can be decoded without error.
// These are real Rust-compiled component binaries from
// https://github.com/WebAssembly/wasi-testsuite (prod/testsuite-base branch).
func TestWASITestSuite_P3_DecodeAll(t *testing.T) {
	dir := filepath.Join("testdata", "wasi-testsuite-p3")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var wasmFiles []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".wasm") {
			wasmFiles = append(wasmFiles, e.Name())
		}
	}
	require.True(t, len(wasmFiles) > 0, "no .wasm files found in wasi-testsuite-p3")
	t.Logf("Found %d P3 test components", len(wasmFiles))

	for _, name := range wasmFiles {
		t.Run(strings.TrimSuffix(name, ".wasm"), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, name))
			require.NoError(t, err)
			require.True(t, componentBinary.IsComponent(data), "expected component binary")

			comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
			require.NoError(t, err)
			require.NotNil(t, comp)

			// Every test suite component should have at least one core module.
			require.True(t, len(comp.CoreModules) > 0,
				"expected at least one core module in %s", name)

			// Every test suite component exports wasi:cli/run.
			require.True(t, len(comp.Exports) > 0,
				"expected at least one export in %s", name)

			t.Logf("modules=%d instances=%d types=%d aliases=%d canons=%d imports=%d exports=%d",
				len(comp.CoreModules), len(comp.CoreInstances), len(comp.Types),
				len(comp.Aliases), len(comp.Canons), len(comp.Imports), len(comp.Exports))
		})
	}
}

// TestWASITestSuite_P3_CLITests verifies structure of CLI-related test components.
func TestWASITestSuite_P3_CLITests(t *testing.T) {
	cliTests := []struct {
		name           string
		expectImports  []string
		expectExport   string
	}{
		{
			name: "cli-env",
			expectImports: []string{
				"wasi:cli/environment@0.3.0-rc-2026-02-09",
			},
			expectExport: "wasi:cli/run@0.3.0-rc-2026-02-09",
		},
		{
			name: "cli-exit",
			expectImports: []string{
				"wasi:cli/exit@0.3.0-rc-2026-02-09",
			},
			expectExport: "wasi:cli/run@0.3.0-rc-2026-02-09",
		},
		{
			name: "cli-stdio",
			expectImports: []string{
				"wasi:cli/stdin@0.3.0-rc-2026-02-09",
				"wasi:cli/stdout@0.3.0-rc-2026-02-09",
				"wasi:cli/stderr@0.3.0-rc-2026-02-09",
			},
			expectExport: "wasi:cli/run@0.3.0-rc-2026-02-09",
		},
	}

	for _, tc := range cliTests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "wasi-testsuite-p3", tc.name+".wasm"))
			require.NoError(t, err)

			comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
			require.NoError(t, err)

			importNames := make(map[string]bool)
			for _, imp := range comp.Imports {
				importNames[imp.Name] = true
			}
			for _, expected := range tc.expectImports {
				require.True(t, importNames[expected],
					"expected import %q not found in %s", expected, tc.name)
			}

			// Verify the run export exists.
			var hasRunExport bool
			for _, exp := range comp.Exports {
				if exp.Name == tc.expectExport {
					hasRunExport = true
					break
				}
			}
			require.True(t, hasRunExport, "expected export %q in %s", tc.expectExport, tc.name)
		})
	}
}

// TestWASITestSuite_P3_RandomTest verifies the random test component structure.
func TestWASITestSuite_P3_RandomTest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "wasi-testsuite-p3", "random.wasm"))
	require.NoError(t, err)

	comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
	require.NoError(t, err)

	importNames := make(map[string]bool)
	for _, imp := range comp.Imports {
		importNames[imp.Name] = true
	}

	// P3 random test imports all three random interfaces.
	require.True(t, importNames["wasi:random/random@0.3.0-rc-2026-02-09"])
	require.True(t, importNames["wasi:random/insecure@0.3.0-rc-2026-02-09"])
	require.True(t, importNames["wasi:random/insecure-seed@0.3.0-rc-2026-02-09"])
}

// TestWASITestSuite_P3_ClockTests verifies clock test component structure.
func TestWASITestSuite_P3_ClockTests(t *testing.T) {
	clockTests := []struct {
		name          string
		expectImport  string
	}{
		{"monotonic-clock", "wasi:clocks/monotonic-clock@0.3.0-rc-2026-02-09"},
		{"wall-clock", "wasi:clocks/system-clock@0.3.0-rc-2026-02-09"},
	}

	for _, tc := range clockTests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "wasi-testsuite-p3", tc.name+".wasm"))
			require.NoError(t, err)

			comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
			require.NoError(t, err)

			var found bool
			for _, imp := range comp.Imports {
				if imp.Name == tc.expectImport {
					found = true
					break
				}
			}
			require.True(t, found, "expected import %q", tc.expectImport)
		})
	}
}

// TestWASITestSuite_P3_FilesystemTests verifies filesystem test components parse.
func TestWASITestSuite_P3_FilesystemTests(t *testing.T) {
	fsTests := []string{
		"filesystem-advise",
		"filesystem-dotdot",
		"filesystem-flags-and-type",
		"filesystem-hard-links",
		"filesystem-io",
		"filesystem-is-same-object",
		"filesystem-metadata-hash",
		"filesystem-mkdir-rmdir",
		"filesystem-open-errors",
		"filesystem-read-directory",
		"filesystem-rename",
		"filesystem-set-size",
		"filesystem-stat",
		"filesystem-unlink-errors",
	}

	for _, name := range fsTests {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "wasi-testsuite-p3", name+".wasm"))
			require.NoError(t, err)

			comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
			require.NoError(t, err)
			require.True(t, len(comp.CoreModules) > 0)
			require.True(t, len(comp.Imports) > 0)
		})
	}
}

// TestWASITestSuite_P3_HTTPTests verifies HTTP test components parse.
func TestWASITestSuite_P3_HTTPTests(t *testing.T) {
	httpTests := []string{
		"http-fields",
		"http-request",
		"http-response",
		"http-service",
	}

	for _, name := range httpTests {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "wasi-testsuite-p3", name+".wasm"))
			require.NoError(t, err)

			comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
			require.NoError(t, err)
			require.True(t, len(comp.CoreModules) > 0)
			require.True(t, len(comp.Imports) > 0)

			// HTTP tests should import HTTP interfaces.
			var hasHTTPImport bool
			for _, imp := range comp.Imports {
				if strings.Contains(imp.Name, "wasi:http") {
					hasHTTPImport = true
					break
				}
			}
			require.True(t, hasHTTPImport, "expected wasi:http import in %s", name)
		})
	}
}

// TestWASITestSuite_P3_SocketTests verifies socket test components parse.
func TestWASITestSuite_P3_SocketTests(t *testing.T) {
	socketTests := []string{
		"sockets-echo",
		"sockets-tcp-bind",
		"sockets-tcp-connect",
		"sockets-tcp-listen",
		"sockets-tcp-receive",
		"sockets-tcp-send",
		"sockets-udp-bind",
		"sockets-udp-connect",
		"sockets-udp-receive",
		"sockets-udp-send",
	}

	for _, name := range socketTests {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "wasi-testsuite-p3", name+".wasm"))
			require.NoError(t, err)

			comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
			require.NoError(t, err)
			require.True(t, len(comp.CoreModules) > 0)
			require.True(t, len(comp.Imports) > 0)
		})
	}
}

// TestWASITestSuite_P3_TestSpecs verifies that JSON test specifications
// can be loaded alongside each component binary.
func TestWASITestSuite_P3_TestSpecs(t *testing.T) {
	dir := filepath.Join("testdata", "wasi-testsuite-p3")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	type testOp struct {
		Type     string            `json:"type"`
		Env      map[string]string `json:"env,omitempty"`
		Args     []string          `json:"args,omitempty"`
		ExitCode *int              `json:"exit_code,omitempty"`
	}
	type testSpec struct {
		Name       string   `json:"name,omitempty"`       // manifest.json
		Operations []testOp `json:"operations,omitempty"` // run specs
		Dirs       []string `json:"dirs,omitempty"`       // filesystem specs
	}

	var jsonCount int
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		jsonCount++
		name := e.Name()
		t.Run(strings.TrimSuffix(name, ".json"), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, name))
			require.NoError(t, err)

			var spec testSpec
			err = json.Unmarshal(data, &spec)
			require.NoError(t, err)

			// manifest.json is metadata only, skip validation.
			if name == "manifest.json" {
				require.True(t, spec.Name != "", "manifest should have a name")
				return
			}

			// Specs can have operations (run/wait/read/write) and/or dirs (filesystem fixtures).
			hasOperations := len(spec.Operations) > 0
			hasDirs := len(spec.Dirs) > 0
			require.True(t, hasOperations || hasDirs,
				"test spec %s should have operations or dirs", name)

			if hasOperations {
				// First operation should be "run".
				require.Equal(t, "run", spec.Operations[0].Type,
					"first operation in %s should be 'run'", name)
			}

			// Verify the corresponding .wasm file exists.
			wasmName := strings.TrimSuffix(name, ".json") + ".wasm"
			_, err = os.Stat(filepath.Join(dir, wasmName))
			require.NoError(t, err, "missing .wasm for spec %s", name)
		})
	}
	t.Logf("Validated %d test specs", jsonCount)
}

// TestWASITestSuite_P3_InterfaceCoverage reports which WASI interfaces are
// imported across all test suite components.
func TestWASITestSuite_P3_InterfaceCoverage(t *testing.T) {
	dir := filepath.Join("testdata", "wasi-testsuite-p3")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	// Collect all unique top-level imports across all test components.
	allImports := make(map[string]int)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".wasm") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)

		comp, err := componentBinary.DecodeComponent(data, 0, 65536, false)
		require.NoError(t, err)

		for _, imp := range comp.Imports {
			allImports[imp.Name]++
		}
	}

	// Group by WASI package.
	packages := make(map[string][]string)
	for name := range allImports {
		// Extract package like "wasi:cli" from "wasi:cli/exit@0.3.0-rc-..."
		parts := strings.SplitN(name, "/", 2)
		if len(parts) == 2 {
			packages[parts[0]] = append(packages[parts[0]], name)
		}
	}

	t.Logf("WASI interfaces required by official test suite:")
	for pkg, interfaces := range packages {
		t.Logf("  %s: %d interfaces", pkg, len(interfaces))
		for _, iface := range interfaces {
			t.Logf("    - %s (used by %d tests)", iface, allImports[iface])
		}
	}

	// Verify the test suite covers the major WASI packages.
	require.True(t, len(packages["wasi:cli"]) > 0, "expected wasi:cli interfaces")
	require.True(t, len(packages["wasi:io"]) > 0, "expected wasi:io interfaces")
	require.True(t, len(packages["wasi:clocks"]) > 0, "expected wasi:clocks interfaces")
	require.True(t, len(packages["wasi:random"]) > 0, "expected wasi:random interfaces")
	require.True(t, len(packages["wasi:filesystem"]) > 0, "expected wasi:filesystem interfaces")
	require.True(t, len(packages["wasi:sockets"]) > 0, "expected wasi:sockets interfaces")
	require.True(t, len(packages["wasi:http"]) > 0, "expected wasi:http interfaces")
}
