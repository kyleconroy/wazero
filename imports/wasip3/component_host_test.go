package wasip3

import (
	"context"
	"os"
	"path/filepath"
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

func instantiateP3WithPreopens(t *testing.T, wasmFile string, args []string, env [][2]string, preopens [][2]string) (*ComponentHost, error) {
	data, err := os.ReadFile(wasmDir + "/" + wasmFile)
	if err != nil {
		t.Skip(wasmFile + " not found")
	}

	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	t.Cleanup(func() { rt.Close(ctx) })

	host := NewComponentHost(os.Stdin, os.Stdout, os.Stderr, args, env)
	for _, po := range preopens {
		host.AddPreopen(po[0], po[1])
	}
	mod, err := InstantiateComponentWithHost(ctx, rt, data,
		wazero.NewModuleConfig().WithName("").WithStartFunctions(), host)
	if mod != nil {
		t.Cleanup(func() { mod.Close(ctx) })
	}
	return host, err
}

const fsTestDir = "../../wasi-testsuite/tests/rust/wasm32-wasip3/src/bin/fs-tests.dir"

// runFSTest runs a filesystem test wasm module with the given preopen directory.
func runFSTest(t *testing.T, name string, preopen string) {
	t.Helper()
	_, err := instantiateP3WithPreopens(t, name+".wasm",
		[]string{name}, nil,
		[][2]string{{preopen, "fs-tests.dir"}})
	if err != nil {
		t.Errorf("%s failed: %v", name, err)
	}
}

// copyTestDir creates a temporary copy of the fs-tests.dir for tests that write.
func copyTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dest := filepath.Join(dir, "fs-tests.dir")
	if err := copyDir(fsTestDir, dest); err != nil {
		t.Fatal(err)
	}
	return dest
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func TestFilesystemStat(t *testing.T) {
	runFSTest(t, "filesystem-stat", copyTestDir(t))
}

func TestFilesystemFlagsAndType(t *testing.T) {
	runFSTest(t, "filesystem-flags-and-type", copyTestDir(t))
}

func TestFilesystemIO(t *testing.T) {
	runFSTest(t, "filesystem-io", copyTestDir(t))
}

func TestFilesystemIsSameObject(t *testing.T) {
	runFSTest(t, "filesystem-is-same-object", copyTestDir(t))
}

func TestFilesystemOpenErrors(t *testing.T) {
	runFSTest(t, "filesystem-open-errors", fsTestDir)
}

func TestFilesystemReadDirectory(t *testing.T) {
	runFSTest(t, "filesystem-read-directory", fsTestDir)
}

func TestFilesystemMetadataHash(t *testing.T) {
	runFSTest(t, "filesystem-metadata-hash", copyTestDir(t))
}

func TestFilesystemAdvise(t *testing.T) {
	runFSTest(t, "filesystem-advise", fsTestDir)
}

func TestFilesystemMkdirRmdir(t *testing.T) {
	runFSTest(t, "filesystem-mkdir-rmdir", copyTestDir(t))
}

func TestFilesystemRename(t *testing.T) {
	runFSTest(t, "filesystem-rename", copyTestDir(t))
}

func TestFilesystemSetSize(t *testing.T) {
	runFSTest(t, "filesystem-set-size", copyTestDir(t))
}

func TestFilesystemUnlinkErrors(t *testing.T) {
	runFSTest(t, "filesystem-unlink-errors", fsTestDir)
}

func TestFilesystemHardLinks(t *testing.T) {
	runFSTest(t, "filesystem-hard-links", copyTestDir(t))
}

func TestFilesystemDotdot(t *testing.T) {
	runFSTest(t, "filesystem-dotdot", copyTestDir(t))
}

func TestCliStdioRoundtrip(t *testing.T) {
	_, err := instantiateP3(t, "cli-stdio-roundtrip.wasm", []string{"cli-stdio-roundtrip"}, nil)
	if err != nil {
		t.Errorf("cli-stdio-roundtrip failed: %v", err)
	}
}

func TestHttpFields(t *testing.T) {
	_, err := instantiateP3(t, "http-fields.wasm", []string{"http-fields"}, nil)
	if err != nil {
		t.Errorf("http-fields failed: %v", err)
	}
}

func TestHttpRequest(t *testing.T) {
	_, err := instantiateP3(t, "http-request.wasm", []string{"http-request"}, nil)
	if err != nil {
		t.Errorf("http-request failed: %v", err)
	}
}

func TestHttpResponse(t *testing.T) {
	_, err := instantiateP3(t, "http-response.wasm", []string{"http-response"}, nil)
	if err != nil {
		t.Errorf("http-response failed: %v", err)
	}
}

func TestHttpService(t *testing.T) {
	_, err := instantiateP3(t, "http-service.wasm", []string{"http-service"}, nil)
	if err != nil {
		t.Errorf("http-service failed: %v", err)
	}
}

func TestSocketsEcho(t *testing.T) {
	_, err := instantiateP3(t, "sockets-echo.wasm", []string{"sockets-echo"}, nil)
	if err != nil {
		t.Errorf("sockets-echo failed: %v", err)
	}
}

func TestSocketsTcpBind(t *testing.T) {
	_, err := instantiateP3(t, "sockets-tcp-bind.wasm", []string{"sockets-tcp-bind"}, nil)
	if err != nil {
		t.Errorf("sockets-tcp-bind failed: %v", err)
	}
}

func TestSocketsTcpConnect(t *testing.T) {
	_, err := instantiateP3(t, "sockets-tcp-connect.wasm", []string{"sockets-tcp-connect"}, nil)
	if err != nil {
		t.Errorf("sockets-tcp-connect failed: %v", err)
	}
}

func TestSocketsTcpListen(t *testing.T) {
	_, err := instantiateP3(t, "sockets-tcp-listen.wasm", []string{"sockets-tcp-listen"}, nil)
	if err != nil {
		t.Errorf("sockets-tcp-listen failed: %v", err)
	}
}

func TestSocketsTcpReceive(t *testing.T) {
	_, err := instantiateP3(t, "sockets-tcp-receive.wasm", []string{"sockets-tcp-receive"}, nil)
	if err != nil {
		t.Errorf("sockets-tcp-receive failed: %v", err)
	}
}

func TestSocketsTcpSend(t *testing.T) {
	_, err := instantiateP3(t, "sockets-tcp-send.wasm", []string{"sockets-tcp-send"}, nil)
	if err != nil {
		t.Errorf("sockets-tcp-send failed: %v", err)
	}
}

func TestSocketsUdpBind(t *testing.T) {
	_, err := instantiateP3(t, "sockets-udp-bind.wasm", []string{"sockets-udp-bind"}, nil)
	if err != nil {
		t.Errorf("sockets-udp-bind failed: %v", err)
	}
}

func TestSocketsUdpConnect(t *testing.T) {
	_, err := instantiateP3(t, "sockets-udp-connect.wasm", []string{"sockets-udp-connect"}, nil)
	if err != nil {
		t.Errorf("sockets-udp-connect failed: %v", err)
	}
}

func TestSocketsUdpReceive(t *testing.T) {
	_, err := instantiateP3(t, "sockets-udp-receive.wasm", []string{"sockets-udp-receive"}, nil)
	if err != nil {
		t.Errorf("sockets-udp-receive failed: %v", err)
	}
}

func TestSocketsUdpSend(t *testing.T) {
	_, err := instantiateP3(t, "sockets-udp-send.wasm", []string{"sockets-udp-send"}, nil)
	if err != nil {
		t.Errorf("sockets-udp-send failed: %v", err)
	}
}
