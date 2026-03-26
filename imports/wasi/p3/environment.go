package p3

import (
	"context"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
)

const i32 = wasm.ValueTypeI32

// instantiateEnvironment registers the wasi:cli/environment host module.
// This provides access to command-line arguments and environment variables.
func (b *builder) instantiateEnvironment(ctx context.Context) (api.Closer, error) {
	return b.r.NewHostModuleBuilder(CliArgs).
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
// In the canonical ABI, this writes a list<string> to linear memory.
// The caller provides a pointer to where the result should be written.
func getArguments(ctx context.Context, mod api.Module, stack []uint64) {
	resultPtr := api.DecodeU32(stack[0])

	// Get the arguments from the module's system context.
	sysCtx := mod.(*wasm.ModuleInstance).Sys
	args := sysCtx.Args()

	mem := mod.Memory()
	if mem == nil {
		return
	}

	// Write the list header: pointer to data, then count.
	// For now, we write an empty list if we can't allocate.
	writeStringList(mem, resultPtr, args)
}

// getEnvironment implements wasi:cli/environment.get-environment.
// Returns a list<tuple<string, string>> of environment variables.
func getEnvironment(ctx context.Context, mod api.Module, stack []uint64) {
	resultPtr := api.DecodeU32(stack[0])

	sysCtx := mod.(*wasm.ModuleInstance).Sys
	environ := sysCtx.Environ()

	mem := mod.Memory()
	if mem == nil {
		return
	}

	// Write list of key-value pairs.
	// For the initial implementation, write empty list (ptr=0, len=0).
	_ = environ
	mem.WriteUint32Le(resultPtr, 0)   // data pointer
	mem.WriteUint32Le(resultPtr+4, 0) // count
}

// writeStringList writes a list<string> to linear memory in canonical ABI format.
func writeStringList(mem api.Memory, ptr uint32, items [][]byte) {
	count := uint32(len(items))

	// Write list header: data pointer and count.
	// In a full implementation, we would use realloc to allocate memory
	// for the string data. For now, write count=0 as a safe default
	// unless we have data.
	if count == 0 {
		mem.WriteUint32Le(ptr, 0)   // data pointer
		mem.WriteUint32Le(ptr+4, 0) // count
		return
	}

	// Write count but data pointer as 0 (would need realloc for proper implementation).
	mem.WriteUint32Le(ptr, 0)
	mem.WriteUint32Le(ptr+4, count)
}
