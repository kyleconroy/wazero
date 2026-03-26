package p3

import (
	"context"
	"time"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
)

const i64 = wasm.ValueTypeI64

// instantiateClocks registers the wasi:clocks host modules.
func (b *builder) instantiateClocks(ctx context.Context) (api.Closer, error) {
	// wasi:clocks/monotonic-clock
	_, err := b.r.NewHostModuleBuilder(ClocksMono).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(monotonicNow), nil, []api.ValueType{i64}).
		WithName("now").
		Export("now").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(monotonicResolution), nil, []api.ValueType{i64}).
		WithName("resolution").
		Export("resolution").
		Instantiate(ctx)
	if err != nil {
		return nil, err
	}

	// wasi:clocks/wall-clock
	return b.r.NewHostModuleBuilder(ClocksWall).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(wallClockNow), []api.ValueType{i32}, nil).
		WithName("now").
		Export("now").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(wallClockResolution), []api.ValueType{i32}, nil).
		WithName("resolution").
		Export("resolution").
		Instantiate(ctx)
}

// monotonicNow implements wasi:clocks/monotonic-clock.now.
// Returns the current time in nanoseconds as a u64.
func monotonicNow(ctx context.Context, mod api.Module, stack []uint64) {
	sysCtx := mod.(*wasm.ModuleInstance).Sys
	now := sysCtx.Nanotime()
	stack[0] = uint64(now)
}

// monotonicResolution implements wasi:clocks/monotonic-clock.resolution.
// Returns the clock resolution in nanoseconds.
func monotonicResolution(_ context.Context, mod api.Module, stack []uint64) {
	sysCtx := mod.(*wasm.ModuleInstance).Sys
	stack[0] = uint64(sysCtx.NanotimeResolution())
}

// wallClockNow implements wasi:clocks/wall-clock.now.
// Writes a datetime record { seconds: u64, nanoseconds: u32 } to the result pointer.
func wallClockNow(_ context.Context, mod api.Module, stack []uint64) {
	resultPtr := api.DecodeU32(stack[0])

	sysCtx := mod.(*wasm.ModuleInstance).Sys
	sec, nsec := sysCtx.Walltime()

	mem := mod.Memory()
	if mem == nil {
		return
	}

	// Write datetime record: seconds (u64) + nanoseconds (u32)
	mem.WriteUint64Le(resultPtr, uint64(sec))
	mem.WriteUint32Le(resultPtr+8, uint32(nsec))
}

// wallClockResolution implements wasi:clocks/wall-clock.resolution.
// Writes a datetime record representing the clock resolution.
func wallClockResolution(_ context.Context, mod api.Module, stack []uint64) {
	resultPtr := api.DecodeU32(stack[0])

	mem := mod.Memory()
	if mem == nil {
		return
	}

	// Wall clock resolution is typically 1 microsecond.
	resolution := time.Microsecond
	sec := int64(resolution / time.Second)
	nsec := int64((resolution % time.Second) / time.Nanosecond)

	mem.WriteUint64Le(resultPtr, uint64(sec))
	mem.WriteUint32Le(resultPtr+8, uint32(nsec))
}
