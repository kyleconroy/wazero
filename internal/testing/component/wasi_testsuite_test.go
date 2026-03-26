package component_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/internal/testing/require"
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
