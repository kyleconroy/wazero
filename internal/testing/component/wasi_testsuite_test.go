package component_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"errors"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/internal/testing/require"
	"github.com/tetratelabs/wazero/internal/wasm/component"
	componentBinary "github.com/tetratelabs/wazero/internal/wasm/component/binary"
	"github.com/tetratelabs/wazero/internal/wasm/component/runtime"
	"github.com/tetratelabs/wazero/sys"
)

// wasiTestSpec represents the WASI test suite JSON specification format.
// Supports both the legacy format (args/dirs/env/exit_code/stdout/stderr)
// and the operation-based format (operations array).
// See: https://github.com/WebAssembly/wasi-testsuite/blob/prod/testsuite-base/doc/specification.md
type wasiTestSpec struct {
	// Metadata (manifest.json only)
	Name string `json:"name,omitempty"`

	// Legacy format fields
	Args     []string          `json:"args,omitempty"`
	Dirs     []string          `json:"dirs,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	ExitCode *int              `json:"exit_code,omitempty"`
	Stdout   string            `json:"stdout,omitempty"`
	Stderr   string            `json:"stderr,omitempty"`

	// Operation-based format fields
	Proposals  []string      `json:"proposals,omitempty"`
	Operations []wasiTestOp  `json:"operations,omitempty"`
}

type wasiTestOp struct {
	Type         string            `json:"type"`
	Args         []string          `json:"args,omitempty"`
	Dirs         []string          `json:"dirs,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	ExitCode     *int              `json:"exit_code,omitempty"`
	ID           string            `json:"id,omitempty"`
	Payload      string            `json:"payload,omitempty"`
	ProtocolType string            `json:"protocol_type,omitempty"`
}

// toLegacy converts the spec to legacy format for simple execution.
// If operations-based, it extracts args/env/dirs from the "run" operation
// and exit_code from the "wait" operation.
func (s *wasiTestSpec) toLegacy() wasiTestSpec {
	if len(s.Operations) == 0 {
		// Already legacy format. Apply defaults.
		out := *s
		if out.ExitCode == nil {
			zero := 0
			out.ExitCode = &zero
		}
		return out
	}

	out := wasiTestSpec{}
	for _, op := range s.Operations {
		switch op.Type {
		case "run":
			out.Args = op.Args
			out.Dirs = op.Dirs
			out.Env = op.Env
		case "wait":
			out.ExitCode = op.ExitCode
		case "read":
			if op.ID == "stdout" || op.ID == "" {
				out.Stdout = op.Payload
			} else if op.ID == "stderr" {
				out.Stderr = op.Payload
			}
		}
	}
	if out.ExitCode == nil {
		zero := 0
		out.ExitCode = &zero
	}
	return out
}

// loadTestSpec loads a JSON test spec from the given directory.
// If no .json file exists, returns a default spec (run with exit_code=0).
func loadTestSpec(dir, baseName string) (wasiTestSpec, error) {
	jsonPath := filepath.Join(dir, baseName+".json")
	data, err := os.ReadFile(jsonPath)
	if os.IsNotExist(err) {
		zero := 0
		return wasiTestSpec{ExitCode: &zero}, nil
	}
	if err != nil {
		return wasiTestSpec{}, err
	}

	var spec wasiTestSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return wasiTestSpec{}, err
	}
	return spec.toLegacy(), nil
}

// TestWASITestSuite_P1_Run executes the official WASI preview 1 test suite
// using wazero's WASI snapshot_preview1 implementation.
// Following: https://github.com/WebAssembly/wasi-testsuite/blob/prod/testsuite-base/doc/specification.md
func TestWASITestSuite_P1_Run(t *testing.T) {
	dir := filepath.Join("testdata", "wasi-testsuite-p1")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	// Collect all .wasm test cases.
	var testCases []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".wasm") {
			testCases = append(testCases, strings.TrimSuffix(e.Name(), ".wasm"))
		}
	}
	t.Logf("Found %d P1 test cases", len(testCases))

	for _, tc := range testCases {
		tc := tc
		t.Run(tc, func(t *testing.T) {
			// Load the test spec.
			spec, err := loadTestSpec(dir, tc)
			require.NoError(t, err)

			// Read the wasm binary.
			wasmData, err := os.ReadFile(filepath.Join(dir, tc+".wasm"))
			require.NoError(t, err)

			// Create a fresh temp directory for filesystem tests.
			tmpDir := t.TempDir()

			// Set up preopened directories.
			// The spec uses relative directory names (e.g., "fs-tests.dir").
			// We create them as subdirectories of the temp dir.
			fsConfig := wazero.NewFSConfig()
			for _, d := range spec.Dirs {
				guestDir := filepath.Join(tmpDir, d)
				if err := os.MkdirAll(guestDir, 0o755); err != nil {
					t.Fatalf("failed to create preopen dir %s: %v", d, err)
				}
				// Mount with the same name the test expects.
				fsConfig = fsConfig.WithDirMount(guestDir, d)
			}

			// Build module config following the WASI test suite spec.
			modConfig := wazero.NewModuleConfig().
				WithStartFunctions().
				WithFSConfig(fsConfig)

			// Set args: the program name is the first arg, followed by spec args.
			args := append([]string{tc + ".wasm"}, spec.Args...)
			modConfig = modConfig.WithArgs(args...)

			// Set environment variables.
			for k, v := range spec.Env {
				modConfig = modConfig.WithEnv(k, v)
			}

			// Capture stdout and stderr.
			var stdoutBuf, stderrBuf bytes.Buffer
			modConfig = modConfig.WithStdout(&stdoutBuf).WithStderr(&stderrBuf)

			// Create runtime and instantiate WASI.
			ctx := context.Background()
			r := wazero.NewRuntime(ctx)
			defer r.Close(ctx)

			wasi_snapshot_preview1.MustInstantiate(ctx, r)

			// Compile and run.
			compiled, err := r.CompileModule(ctx, wasmData)
			require.NoError(t, err)

			_, runErr := r.InstantiateModule(ctx, compiled, modConfig)

			// Validate exit code.
			expectedExitCode := uint32(*spec.ExitCode)
			if runErr != nil {
				if exitErr, ok := runErr.(*sys.ExitError); ok {
					require.Equal(t, expectedExitCode, exitErr.ExitCode(),
						"exit code mismatch (stdout: %s, stderr: %s)",
						stdoutBuf.String(), stderrBuf.String())
				} else {
					t.Fatalf("unexpected error: %v (stdout: %s, stderr: %s)",
						runErr, stdoutBuf.String(), stderrBuf.String())
				}
			} else {
				// No error means exit code 0.
				require.Equal(t, expectedExitCode, uint32(0),
					"expected exit code %d but got 0 (stdout: %s, stderr: %s)",
					expectedExitCode, stdoutBuf.String(), stderrBuf.String())
			}

			// Validate stdout if specified.
			if spec.Stdout != "" {
				require.Equal(t, spec.Stdout, stdoutBuf.String(),
					"stdout mismatch")
			}

			// Validate stderr if specified.
			if spec.Stderr != "" {
				require.Equal(t, spec.Stderr, stderrBuf.String(),
					"stderr mismatch")
			}
		})
	}
}

// TestWASITestSuite_P3_Run executes P3 component test suite binaries
// using the component model instantiation engine with WASI host implementations.
func TestWASITestSuite_P3_Run(t *testing.T) {
	dir := filepath.Join("testdata", "wasi-testsuite-p3")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var testCases []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".wasm") {
			testCases = append(testCases, strings.TrimSuffix(e.Name(), ".wasm"))
		}
	}
	t.Logf("Found %d P3 test cases", len(testCases))

	for _, tc := range testCases {
		tc := tc
		t.Run(tc, func(t *testing.T) {
			spec, err := loadTestSpec(dir, tc)
			require.NoError(t, err)

			wasmData, err := os.ReadFile(filepath.Join(dir, tc+".wasm"))
			require.NoError(t, err)

			// Decode the component.
			comp, err := componentBinary.DecodeComponent(wasmData, 0, 65536, false)
			require.NoError(t, err)

			var stdoutBuf, stderrBuf bytes.Buffer
			tmpDir := t.TempDir()

			// Build WASI host imports.
			hostImports := buildWASIP3HostImports(t, comp, spec, &stdoutBuf, &stderrBuf, tmpDir)

			// Create runtime and engine.
			ctx := context.Background()
			r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter())
			defer r.Close(ctx)

			modConfig := wazero.NewModuleConfig().
				WithStdout(&stdoutBuf).
				WithStderr(&stderrBuf)

			args := append([]string{tc + ".wasm"}, spec.Args...)
			modConfig = modConfig.WithArgs(args...)
			for k, v := range spec.Env {
				modConfig = modConfig.WithEnv(k, v)
			}

			eng := runtime.NewEngine(ctx, r, modConfig)
			defer eng.Close()

			// Instantiate the component.
			instErr := eng.Instantiate(comp, hostImports)
			if instErr != nil {
				// Check if it's an exit error from instantiation.
				if exitErr, ok := instErr.(*sys.ExitError); ok {
					validateExitCode(t, spec, exitErr.ExitCode(), &stdoutBuf, &stderrBuf)
					return
				}
				// Check if wrapped.
				if strings.Contains(instErr.Error(), "exit_code") {
					t.Logf("instantiation error (may contain exit): %v", instErr)
				}
				// For tests that expect non-zero exit, look for sys.ExitError in the chain.
				var sysExit *sys.ExitError
				if errors.As(instErr, &sysExit) {
					validateExitCode(t, spec, sysExit.ExitCode(), &stdoutBuf, &stderrBuf)
					return
				}
				t.Fatalf("instantiation error: %v (stdout: %s, stderr: %s)",
					instErr, stdoutBuf.String(), stderrBuf.String())
			}

			// Call the component's entry point.
			runErr := eng.CallStart()
			if runErr != nil {
				var sysExit *sys.ExitError
				if errors.As(runErr, &sysExit) {
					validateExitCode(t, spec, sysExit.ExitCode(), &stdoutBuf, &stderrBuf)
					return
				}
				t.Fatalf("runtime error: %v (stdout: %s, stderr: %s)",
					runErr, stdoutBuf.String(), stderrBuf.String())
			}

			// No error means exit code 0.
			validateExitCode(t, spec, 0, &stdoutBuf, &stderrBuf)
		})
	}
}

func validateExitCode(t *testing.T, spec wasiTestSpec, gotExitCode uint32, stdout, stderr *bytes.Buffer) {
	t.Helper()
	expectedExitCode := uint32(*spec.ExitCode)
	require.Equal(t, expectedExitCode, gotExitCode,
		"exit code mismatch (stdout: %s, stderr: %s)",
		stdout.String(), stderr.String())

	if spec.Stdout != "" {
		require.Equal(t, spec.Stdout, stdout.String(), "stdout mismatch")
	}
	if spec.Stderr != "" {
		require.Equal(t, spec.Stderr, stderr.String(), "stderr mismatch")
	}
}

// buildWASIP3HostImports creates host implementations for all WASI interfaces
// that P3 components import.
func buildWASIP3HostImports(
	t *testing.T,
	comp *component.Component,
	spec wasiTestSpec,
	stdout, stderr *bytes.Buffer,
	tmpDir string,
) runtime.HostImports {
	imports := make(runtime.HostImports)

	for _, imp := range comp.Imports {
		name := imp.Name
		switch {
		case strings.HasPrefix(name, "wasi:cli/exit"):
			imports[name] = wasiCLIExit()
		case strings.HasPrefix(name, "wasi:cli/environment"):
			imports[name] = wasiCLIEnvironment(spec.Env)
		case strings.HasPrefix(name, "wasi:cli/stdin"):
			imports[name] = wasiCLIStdin()
		case strings.HasPrefix(name, "wasi:cli/stdout"):
			imports[name] = wasiCLIStdout()
		case strings.HasPrefix(name, "wasi:cli/stderr"):
			imports[name] = wasiCLIStderr()
		case strings.HasPrefix(name, "wasi:cli/terminal-input"):
			imports[name] = wasiCLITerminalResource()
		case strings.HasPrefix(name, "wasi:cli/terminal-output"):
			imports[name] = wasiCLITerminalResource()
		case strings.HasPrefix(name, "wasi:cli/terminal-stdin"):
			imports[name] = wasiCLITerminalGet()
		case strings.HasPrefix(name, "wasi:cli/terminal-stdout"):
			imports[name] = wasiCLITerminalGet()
		case strings.HasPrefix(name, "wasi:cli/terminal-stderr"):
			imports[name] = wasiCLITerminalGet()
		case strings.HasPrefix(name, "wasi:io/poll"):
			imports[name] = wasiIOPoll()
		case strings.HasPrefix(name, "wasi:io/error"):
			imports[name] = wasiIOError()
		case strings.HasPrefix(name, "wasi:io/streams"):
			imports[name] = wasiIOStreams(stdout, stderr)
		case strings.HasPrefix(name, "wasi:clocks/monotonic-clock"):
			imports[name] = wasiClocksMonotonic()
		case strings.HasPrefix(name, "wasi:clocks/system-clock"),
			strings.HasPrefix(name, "wasi:clocks/wall-clock"):
			imports[name] = wasiClocksWall()
		case strings.HasPrefix(name, "wasi:random/random"):
			imports[name] = wasiRandomRandom()
		case strings.HasPrefix(name, "wasi:random/insecure-seed"):
			imports[name] = wasiRandomInsecureSeed()
		case strings.HasPrefix(name, "wasi:random/insecure"):
			imports[name] = wasiRandomInsecure()
		case strings.HasPrefix(name, "wasi:filesystem"):
			imports[name] = wasiFilesystemStub()
		case strings.HasPrefix(name, "wasi:sockets"):
			imports[name] = wasiSocketsStub()
		case strings.HasPrefix(name, "wasi:http"):
			imports[name] = wasiHTTPStub()
		default:
			// Provide an empty instance for unknown imports.
			imports[name] = &runtime.HostInstance{
				Funcs:     map[string]api.GoModuleFunc{},
				Resources: map[string]uint32{},
			}
		}
	}
	return imports
}

// --- WASI host implementations ---

// hf is shorthand for creating ordered HostFunc entries.
func hf(name string, fn api.GoModuleFunc) runtime.HostFunc {
	return runtime.HostFunc{Name: name, Func: fn}
}

// newHostInst creates a HostInstance with ordered function list and optional resources.
func newHostInst(funcs []runtime.HostFunc, resources map[string]uint32) *runtime.HostInstance {
	fm := make(map[string]api.GoModuleFunc, len(funcs))
	for _, f := range funcs {
		fm[f.Name] = f.Func
	}
	if resources == nil {
		resources = map[string]uint32{}
	}
	return &runtime.HostInstance{FuncList: funcs, Funcs: fm, Resources: resources}
}

func wasiCLIExit() *runtime.HostInstance {
	return newHostInst([]runtime.HostFunc{
		hf("exit", func(_ context.Context, _ api.Module, stack []uint64) {
			status := api.DecodeI32(stack[0])
			if status != 0 {
				panic(sys.NewExitError(1))
			}
			panic(sys.NewExitError(0))
		}),
	}, nil)
}

func wasiCLIEnvironment(env map[string]string) *runtime.HostInstance {
	return newHostInst([]runtime.HostFunc{
		hf("get-environment", func(ctx context.Context, mod api.Module, stack []uint64) {
			retptr := api.DecodeU32(stack[0])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			if len(env) == 0 {
				mem.WriteUint32Le(retptr, 0)
				mem.WriteUint32Le(retptr+4, 0)
				return
			}
			type envPair struct{ k, v string }
			var pairs []envPair
			for k, v := range env {
				pairs = append(pairs, envPair{k, v})
			}
			realloc := mod.ExportedFunction("cabi_realloc")
			if realloc == nil {
				mem.WriteUint32Le(retptr, 0)
				mem.WriteUint32Le(retptr+4, 0)
				return
			}
			tupleSize := uint32(len(pairs)) * 16
			tupleResults, err := realloc.Call(ctx, 0, 0, 4, uint64(tupleSize))
			if err != nil || len(tupleResults) == 0 {
				mem.WriteUint32Le(retptr, 0)
				mem.WriteUint32Le(retptr+4, 0)
				return
			}
			tupleBase := uint32(tupleResults[0])
			for i, p := range pairs {
				offset := tupleBase + uint32(i)*16
				keyResults, _ := realloc.Call(ctx, 0, 0, 1, uint64(len(p.k)))
				keyPtr := uint32(0)
				if len(keyResults) > 0 {
					keyPtr = uint32(keyResults[0])
					mem.Write(keyPtr, []byte(p.k))
				}
				valResults, _ := realloc.Call(ctx, 0, 0, 1, uint64(len(p.v)))
				valPtr := uint32(0)
				if len(valResults) > 0 {
					valPtr = uint32(valResults[0])
					mem.Write(valPtr, []byte(p.v))
				}
				mem.WriteUint32Le(offset, keyPtr)
				mem.WriteUint32Le(offset+4, uint32(len(p.k)))
				mem.WriteUint32Le(offset+8, valPtr)
				mem.WriteUint32Le(offset+12, uint32(len(p.v)))
			}
			mem.WriteUint32Le(retptr, tupleBase)
			mem.WriteUint32Le(retptr+4, uint32(len(pairs)))
		}),
	}, nil)
}

func wasiCLIStdin() *runtime.HostInstance {
	return newHostInst([]runtime.HostFunc{
		hf("get-stdin", func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = api.EncodeI32(1)
		}),
	}, nil)
}

func wasiCLIStdout() *runtime.HostInstance {
	return newHostInst([]runtime.HostFunc{
		hf("get-stdout", func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = api.EncodeI32(2)
		}),
	}, nil)
}

func wasiCLIStderr() *runtime.HostInstance {
	return newHostInst([]runtime.HostFunc{
		hf("get-stderr", func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = api.EncodeI32(3)
		}),
	}, nil)
}

func wasiCLITerminalResource() *runtime.HostInstance {
	return newHostInst(nil, nil)
}

func wasiCLITerminalGet() *runtime.HostInstance {
	// Lowered: (retptr: i32) -> () -- option<own<terminal-*>> written to memory
	// option layout: discriminant u8 at retptr, value i32 at retptr+4
	// None = discriminant 0
	termGet := func(_ context.Context, mod api.Module, stack []uint64) {
		retptr := api.DecodeU32(stack[0])
		mem := mod.Memory()
		if mem != nil {
			mem.WriteByte(retptr, 0) // None
		}
	}
	return newHostInst([]runtime.HostFunc{
		hf("get-terminal-stdin", termGet),
		hf("get-terminal-stdout", termGet),
		hf("get-terminal-stderr", termGet),
	}, nil)
}

func wasiIOPoll() *runtime.HostInstance {
	return newHostInst([]runtime.HostFunc{
		hf("[method]pollable.block", func(_ context.Context, _ api.Module, _ []uint64) {}),
	}, map[string]uint32{"pollable": 0})
}

func wasiIOError() *runtime.HostInstance {
	return newHostInst(nil, map[string]uint32{"error": 0})
}

func wasiIOStreams(stdout, stderr *bytes.Buffer) *runtime.HostInstance {
	// Note: The lowered ABI uses retptr for complex return types (result<T,E>).
	// Functions with results=[] and an extra i32 param use that param as retptr.
	// Functions with results=[i32] return directly on the stack.
	return newHostInst([]runtime.HostFunc{
		hf("[method]output-stream.check-write", func(_ context.Context, mod api.Module, stack []uint64) {
			// Lowered: (self: i32, retptr: i32) -> ()
			// Result<u64, stream-error> written to memory at retptr:
			//   offset 0: u8 discriminant (0=ok, 1=err)
			//   offset 8: u64 value (for ok case, alignment 8)
			retptr := api.DecodeU32(stack[1])
			mem := mod.Memory()
			if mem != nil {
				mem.WriteByte(retptr, 0)                         // ok discriminant
				mem.WriteUint64Le(retptr+8, 1048576)             // 1MB available
			}
		}),
		hf("[method]output-stream.write", func(_ context.Context, mod api.Module, stack []uint64) {
			// Lowered: (self: i32, ptr: i32, len: i32, retptr: i32) -> ()
			// Result<_, stream-error> written to memory at retptr:
			//   offset 0: u8 discriminant (0=ok, 1=err)
			self := api.DecodeU32(stack[0])
			ptr := api.DecodeU32(stack[1])
			length := api.DecodeU32(stack[2])
			retptr := api.DecodeU32(stack[3])
			mem := mod.Memory()
			if mem != nil {
				data, ok := mem.Read(ptr, length)
				if ok {
					// Route to correct buffer based on stream handle.
					if self == 3 {
						stderr.Write(data)
					} else {
						stdout.Write(data)
					}
				}
				mem.WriteByte(retptr, 0) // ok discriminant
			}
		}),
		hf("[method]output-stream.blocking-flush", func(_ context.Context, mod api.Module, stack []uint64) {
			// Lowered: (self: i32, retptr: i32) -> ()
			retptr := api.DecodeU32(stack[1])
			mem := mod.Memory()
			if mem != nil {
				mem.WriteByte(retptr, 0) // ok
			}
		}),
		hf("[method]output-stream.subscribe", func(_ context.Context, _ api.Module, stack []uint64) {
			// Lowered: (self: i32) -> (i32) -- direct stack return
			stack[0] = api.EncodeI32(100) // pollable handle
		}),
	}, map[string]uint32{"input-stream": 0, "output-stream": 0})
}

func wasiClocksMonotonic() *runtime.HostInstance {
	start := time.Now()
	return newHostInst([]runtime.HostFunc{
		hf("now", func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = uint64(time.Since(start).Nanoseconds())
		}),
		hf("resolution", func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = 1
		}),
		hf("subscribe-instant", func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = api.EncodeI32(101)
		}),
		hf("subscribe-duration", func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = api.EncodeI32(102)
		}),
	}, nil)
}

func wasiClocksWall() *runtime.HostInstance {
	return newHostInst([]runtime.HostFunc{
		hf("now", func(_ context.Context, mod api.Module, stack []uint64) {
			// Lowered: (retptr: i32) -> ()
			// Record<seconds: u64, nanoseconds: u32> written to memory:
			//   offset 0: u64 seconds
			//   offset 8: u32 nanoseconds
			retptr := api.DecodeU32(stack[0])
			mem := mod.Memory()
			if mem != nil {
				now := time.Now()
				mem.WriteUint64Le(retptr, uint64(now.Unix()))
				mem.WriteUint32Le(retptr+8, uint32(now.Nanosecond()))
			}
		}),
		hf("resolution", func(_ context.Context, mod api.Module, stack []uint64) {
			// Lowered: (retptr: i32) -> ()
			retptr := api.DecodeU32(stack[0])
			mem := mod.Memory()
			if mem != nil {
				mem.WriteUint64Le(retptr, 0)
				mem.WriteUint32Le(retptr+8, 1)
			}
		}),
	}, nil)
}

func wasiRandomRandom() *runtime.HostInstance {
	return newHostInst([]runtime.HostFunc{
		hf("get-random-bytes", func(ctx context.Context, mod api.Module, stack []uint64) {
			length := stack[0]
			retptr := api.DecodeU32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			realloc := mod.ExportedFunction("cabi_realloc")
			if realloc == nil {
				mem.WriteUint32Le(retptr, 0)
				mem.WriteUint32Le(retptr+4, 0)
				return
			}
			results, err := realloc.Call(ctx, 0, 0, 1, length)
			if err != nil || len(results) == 0 {
				mem.WriteUint32Le(retptr, 0)
				mem.WriteUint32Le(retptr+4, 0)
				return
			}
			ptr := uint32(results[0])
			buf := make([]byte, length)
			rand.Read(buf)
			mem.Write(ptr, buf)
			mem.WriteUint32Le(retptr, ptr)
			mem.WriteUint32Le(retptr+4, uint32(length))
		}),
		hf("get-random-u64", func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = rand.Uint64()
		}),
	}, nil)
}

func wasiRandomInsecure() *runtime.HostInstance {
	return newHostInst([]runtime.HostFunc{
		hf("get-insecure-random-bytes", func(ctx context.Context, mod api.Module, stack []uint64) {
			length := stack[0]
			retptr := api.DecodeU32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			realloc := mod.ExportedFunction("cabi_realloc")
			if realloc == nil {
				mem.WriteUint32Le(retptr, 0)
				mem.WriteUint32Le(retptr+4, 0)
				return
			}
			results, err := realloc.Call(ctx, 0, 0, 1, length)
			if err != nil || len(results) == 0 {
				mem.WriteUint32Le(retptr, 0)
				mem.WriteUint32Le(retptr+4, 0)
				return
			}
			ptr := uint32(results[0])
			buf := make([]byte, length)
			rand.Read(buf)
			mem.Write(ptr, buf)
			mem.WriteUint32Le(retptr, ptr)
			mem.WriteUint32Le(retptr+4, uint32(length))
		}),
		hf("get-insecure-random-u64", func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = rand.Uint64()
		}),
	}, nil)
}

func wasiRandomInsecureSeed() *runtime.HostInstance {
	return newHostInst([]runtime.HostFunc{
		hf("insecure-seed", func(_ context.Context, mod api.Module, stack []uint64) {
			// Lowered: (retptr: i32) -> ()
			// Tuple<u64, u64> written to memory at retptr.
			retptr := api.DecodeU32(stack[0])
			mem := mod.Memory()
			if mem != nil {
				mem.WriteUint64Le(retptr, rand.Uint64())
				mem.WriteUint64Le(retptr+8, rand.Uint64())
			}
		}),
	}, nil)
}

func wasiFilesystemStub() *runtime.HostInstance {
	return newHostInst(nil, nil)
}

func wasiSocketsStub() *runtime.HostInstance {
	return newHostInst(nil, nil)
}

func wasiHTTPStub() *runtime.HostInstance {
	return newHostInst(nil, nil)
}

// Ensure imports are used.
var (
	_ = binary.LittleEndian
	_ = math.MaxUint32
)
