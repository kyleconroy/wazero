package wasip2

import (
	"context"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
	"github.com/tetratelabs/wazero/sys"
)

// wasi:cli function names.
const (
	cliGetStdinName       = "get-stdin"
	cliGetStdoutName      = "get-stdout"
	cliGetStderrName      = "get-stderr"
	cliGetEnvironmentName = "get-environment"
	cliExitName           = "exit"
	cliGetArgsName        = "get-arguments"
)

// CLIGetStdin implements wasi:cli/stdin.get-stdin.
var CLIGetStdin = &wasm.HostFunc{
	ExportName:  cliGetStdinName,
	Name:        CLIStdinName + "#" + cliGetStdinName,
	ParamTypes:  []api.ValueType{},
	ParamNames:  []string{},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"stream"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		// Return handle 0 for stdin.
		stack[0] = 0
	})},
}

// CLIGetStdout implements wasi:cli/stdout.get-stdout.
var CLIGetStdout = &wasm.HostFunc{
	ExportName:  cliGetStdoutName,
	Name:        CLIStdoutName + "#" + cliGetStdoutName,
	ParamTypes:  []api.ValueType{},
	ParamNames:  []string{},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"stream"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		// Return handle 1 for stdout.
		stack[0] = 1
	})},
}

// CLIGetStderr implements wasi:cli/stderr.get-stderr.
var CLIGetStderr = &wasm.HostFunc{
	ExportName:  cliGetStderrName,
	Name:        CLIStderrName + "#" + cliGetStderrName,
	ParamTypes:  []api.ValueType{},
	ParamNames:  []string{},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"stream"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		// Return handle 2 for stderr.
		stack[0] = 2
	})},
}

// CLIGetEnvironment implements wasi:cli/environment.get-environment.
var CLIGetEnvironment = &wasm.HostFunc{
	ExportName:  cliGetEnvironmentName,
	Name:        CLIEnvironmentName + "#" + cliGetEnvironmentName,
	ParamTypes:  []api.ValueType{},
	ParamNames:  []string{},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"ptr", "len"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		// Return empty environment.
		stack[0] = 0
		stack[1] = 0
	})},
}

// CLIGetArguments implements wasi:cli/environment.get-arguments.
var CLIGetArguments = &wasm.HostFunc{
	ExportName:  cliGetArgsName,
	Name:        CLIEnvironmentName + "#" + cliGetArgsName,
	ParamTypes:  []api.ValueType{},
	ParamNames:  []string{},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"ptr", "len"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		// Return empty args list.
		stack[0] = 0
		stack[1] = 0
	})},
}

// CLIExit implements wasi:cli/exit.exit.
var CLIExit = &wasm.HostFunc{
	ExportName:  cliExitName,
	Name:        CLIExitName + "#" + cliExitName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"status"},
	ResultTypes: []api.ValueType{},
	ResultNames: []string{},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		exitCode := uint32(stack[0])
		panic(sys.NewExitError(exitCode))
	})},
}
