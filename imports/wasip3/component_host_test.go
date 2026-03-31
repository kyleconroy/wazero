package wasip3

import (
	"context"
	"os"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/sys"
)

const wasmDir = "../../wasi-testsuite/tests/rust/testsuite/wasm32-wasip3"

func instantiateP3(t *testing.T, wasmFile string, args []string, env [][2]string) (*ComponentHost, error) {
	data, err := os.ReadFile(wasmDir + "/" + wasmFile)
	if err != nil {
		t.Skip(wasmFile + " not found")
	}

	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	t.Cleanup(func() { rt.Close(ctx) })

	host := NewComponentHost(os.Stdin, os.Stdout, os.Stderr, args, env)
	mod, err := InstantiateComponentWithHost(ctx, rt, data,
		wazero.NewModuleConfig().WithName("").WithStartFunctions(), host)
	if mod != nil {
		t.Cleanup(func() { mod.Close(ctx) })
	}
	return host, err
}

func TestCliExit(t *testing.T) {
	host, err := instantiateP3(t, "cli-exit.wasm", []string{"cli-exit"}, nil)

	exitCode, exited := host.ExitCode()
	t.Logf("exited=%v exitCode=%d err=%v", exited, exitCode, err)

	if !exited {
		t.Error("expected exit to be called")
	}

	if exitErr, ok := err.(*sys.ExitError); ok {
		t.Logf("exit error: code=%d", exitErr.ExitCode())
		if exitErr.ExitCode() != 1 {
			t.Errorf("expected exit code 1, got %d", exitErr.ExitCode())
		}
	} else if err != nil {
		t.Errorf("unexpected error type: %T: %v", err, err)
	}
}

func TestCliEnv(t *testing.T) {
	_, err := instantiateP3(t, "cli-env.wasm",
		[]string{"cli-env.wasm", "a", "b", "42"},
		[][2]string{{"foo", "bar"}, {"baz", "42"}})
	if err != nil {
		t.Errorf("cli-env failed: %v", err)
	}
}

func TestCliEnvDefault(t *testing.T) {
	data, err := os.ReadFile(wasmDir + "/cli-env.wasm")
	if err != nil {
		t.Skip("cli-env.wasm not found")
	}

	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	host := NewComponentHost(os.Stdin, os.Stdout, os.Stderr,
		[]string{"cli-env.wasm", "a", "b", "42"},
		[][2]string{{"foo", "bar"}, {"baz", "42"}})
	mod, err := InstantiateComponentWithHost(ctx, rt, data, wazero.NewModuleConfig().WithName(""), host)
	if mod != nil {
		defer mod.Close(ctx)
	}
	t.Logf("err=%v", err)
}

func TestRandom(t *testing.T) {
	_, err := instantiateP3(t, "random.wasm", []string{"random"}, nil)
	if err != nil {
		t.Errorf("random failed: %v", err)
	}
}

func TestWallClock(t *testing.T) {
	_, err := instantiateP3(t, "wall-clock.wasm", []string{"wall-clock"}, nil)
	if err != nil {
		t.Errorf("wall-clock failed: %v", err)
	}
}

func TestMonotonicClock(t *testing.T) {
	_, err := instantiateP3(t, "monotonic-clock.wasm", []string{"monotonic-clock"}, nil)
	if err != nil {
		t.Errorf("monotonic-clock failed: %v", err)
	}
}

func TestRunWithErr(t *testing.T) {
	host, err := instantiateP3(t, "run-with-err.wasm", []string{"run-with-err"}, nil)

	exitCode, exited := host.ExitCode()
	t.Logf("exited=%v exitCode=%d err=%v", exited, exitCode, err)
}

func TestCliStdio(t *testing.T) {
	_, err := instantiateP3(t, "cli-stdio.wasm", []string{"cli-stdio"}, nil)
	t.Logf("err=%v", err)
}
