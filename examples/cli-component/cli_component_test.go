package main

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasip3"
	"github.com/tetratelabs/wazero/internal/testing/require"
)

const wasmDir = "../../wasi-testsuite/tests/rust/testsuite/wasm32-wasip3"

// TestCliComponent demonstrates running a WASI P3 CLI component that
// validates command-line arguments and environment variables.
//
// This test requires the wasi-testsuite. See the project CLAUDE.md or
// README.md for setup instructions.
func TestCliComponent(t *testing.T) {
	wasmBytes, err := os.ReadFile(wasmDir + "/cli-env.wasm")
	if err != nil {
		t.Skip("cli-env.wasm not found; clone wasi-testsuite first (see README.md)")
	}

	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	stdout := &bytes.Buffer{}

	host := wasip3.NewComponentHost(
		os.Stdin,
		stdout,
		os.Stderr,
		[]string{"cli-env.wasm", "a", "b", "42"},
		[][2]string{{"foo", "bar"}, {"baz", "42"}},
	)

	mod, err := wasip3.InstantiateComponentWithHost(ctx, rt, wasmBytes,
		wazero.NewModuleConfig().WithName("").WithStartFunctions(),
		host,
	)
	if mod != nil {
		defer mod.Close(ctx)
	}

	require.NoError(t, err)
	t.Logf("stdout: %s", stdout.String())
}
