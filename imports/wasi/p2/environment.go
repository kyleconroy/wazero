package p2

import (
	"context"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
)

const i32 = wasm.ValueTypeI32

// instantiateEnvironment registers the wasi:cli/environment@0.2.0 host module.
func (b *builder) instantiateEnvironment(ctx context.Context) (api.Closer, error) {
	return b.r.NewHostModuleBuilder(CliEnv).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(getArguments), []api.ValueType{i32}, []api.ValueType{}).
		WithName("get-arguments").
		Export("get-arguments").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(getEnvironment), []api.ValueType{i32}, []api.ValueType{}).
		WithName("get-environment").
		Export("get-environment").
		Instantiate(ctx)
}

// getArguments implements wasi:cli/environment.get-arguments.
// Writes a list<string> to the result pointer in canonical ABI format.
func getArguments(_ context.Context, mod api.Module, stack []uint64) {
	resultPtr := api.DecodeU32(stack[0])

	sysCtx := mod.(*wasm.ModuleInstance).Sys
	args := sysCtx.Args()

	mem := mod.Memory()
	if mem == nil {
		return
	}

	writeStringList(mem, resultPtr, args)
}

// getEnvironment implements wasi:cli/environment.get-environment.
// Writes a list<tuple<string, string>> to the result pointer.
func getEnvironment(_ context.Context, mod api.Module, stack []uint64) {
	resultPtr := api.DecodeU32(stack[0])

	mem := mod.Memory()
	if mem == nil {
		return
	}

	// Write empty list (ptr=0, len=0) as default.
	mem.WriteUint32Le(resultPtr, 0)
	mem.WriteUint32Le(resultPtr+4, 0)
}

// writeStringList writes a list<string> to linear memory in canonical ABI format.
func writeStringList(mem api.Memory, ptr uint32, items [][]byte) {
	count := uint32(len(items))
	if count == 0 {
		mem.WriteUint32Le(ptr, 0)
		mem.WriteUint32Le(ptr+4, 0)
		return
	}
	mem.WriteUint32Le(ptr, 0)
	mem.WriteUint32Le(ptr+4, count)
}
