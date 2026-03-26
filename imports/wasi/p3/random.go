package p3

import (
	"context"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
)

// instantiateRandom registers the wasi:random/random host module.
func (b *builder) instantiateRandom(ctx context.Context) (api.Closer, error) {
	return b.r.NewHostModuleBuilder(RandomRandom).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(getRandomBytes), []api.ValueType{i32, i32}, nil).
		WithName("get-random-bytes").
		Export("get-random-bytes").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(getRandomU64), nil, []api.ValueType{i64}).
		WithName("get-random-u64").
		Export("get-random-u64").
		Instantiate(ctx)
}

// getRandomBytes implements wasi:random/random.get-random-bytes.
// Writes random bytes to the result pointer in canonical ABI list format.
func getRandomBytes(ctx context.Context, mod api.Module, stack []uint64) {
	length := api.DecodeU32(stack[0])
	resultPtr := api.DecodeU32(stack[1])

	mem := mod.Memory()
	if mem == nil {
		return
	}

	sysCtx := mod.(*wasm.ModuleInstance).Sys
	randSource := sysCtx.RandSource()

	buf := make([]byte, length)
	randSource.Read(buf)

	// Write list result: the data directly at a known location.
	// In a full implementation, this would use realloc.
	// For now, write at resultPtr + 8 and set the list header.
	dataPtr := resultPtr + 8
	mem.Write(dataPtr, buf)
	mem.WriteUint32Le(resultPtr, dataPtr) // data pointer
	mem.WriteUint32Le(resultPtr+4, length) // count
}

// getRandomU64 implements wasi:random/random.get-random-u64.
func getRandomU64(_ context.Context, mod api.Module, stack []uint64) {
	sysCtx := mod.(*wasm.ModuleInstance).Sys
	randSource := sysCtx.RandSource()

	var buf [8]byte
	randSource.Read(buf[:])

	stack[0] = uint64(buf[0]) | uint64(buf[1])<<8 | uint64(buf[2])<<16 | uint64(buf[3])<<24 |
		uint64(buf[4])<<32 | uint64(buf[5])<<40 | uint64(buf[6])<<48 | uint64(buf[7])<<56
}
