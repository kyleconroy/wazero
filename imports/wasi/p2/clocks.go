package p2

import (
	"context"
	"time"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
)

const i64 = wasm.ValueTypeI64

// instantiateClocks registers the wasi:clocks host modules.
func (b *builder) instantiateClocks(ctx context.Context) (api.Closer, error) {
	// wasi:clocks/monotonic-clock@0.2.0
	_, err := b.r.NewHostModuleBuilder(ClocksMono).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(monotonicNow), nil, []api.ValueType{i64}).
		WithName("now").
		Export("now").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(monotonicResolution), nil, []api.ValueType{i64}).
		WithName("resolution").
		Export("resolution").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(monotonicSubscribeInstant), []api.ValueType{i64}, []api.ValueType{i32}).
		WithName("subscribe-instant").
		Export("subscribe-instant").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(monotonicSubscribeDuration), []api.ValueType{i64}, []api.ValueType{i32}).
		WithName("subscribe-duration").
		Export("subscribe-duration").
		Instantiate(ctx)
	if err != nil {
		return nil, err
	}

	// wasi:clocks/wall-clock@0.2.0
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

func monotonicNow(_ context.Context, mod api.Module, stack []uint64) {
	sysCtx := mod.(*wasm.ModuleInstance).Sys
	stack[0] = uint64(sysCtx.Nanotime())
}

func monotonicResolution(_ context.Context, mod api.Module, stack []uint64) {
	sysCtx := mod.(*wasm.ModuleInstance).Sys
	stack[0] = uint64(sysCtx.NanotimeResolution())
}

// monotonicSubscribeInstant returns a pollable that resolves at the given instant.
func monotonicSubscribeInstant(_ context.Context, _ api.Module, stack []uint64) {
	// Return a pollable handle (simplified: always immediately ready).
	stack[0] = api.EncodeU32(0xFFFF_FF00) // synthetic pollable handle
}

// monotonicSubscribeDuration returns a pollable that resolves after the given duration.
func monotonicSubscribeDuration(_ context.Context, _ api.Module, stack []uint64) {
	stack[0] = api.EncodeU32(0xFFFF_FF01) // synthetic pollable handle
}

func wallClockNow(_ context.Context, mod api.Module, stack []uint64) {
	resultPtr := api.DecodeU32(stack[0])
	sysCtx := mod.(*wasm.ModuleInstance).Sys
	sec, nsec := sysCtx.Walltime()

	mem := mod.Memory()
	if mem == nil {
		return
	}
	mem.WriteUint64Le(resultPtr, uint64(sec))
	mem.WriteUint32Le(resultPtr+8, uint32(nsec))
}

func wallClockResolution(_ context.Context, mod api.Module, stack []uint64) {
	resultPtr := api.DecodeU32(stack[0])
	mem := mod.Memory()
	if mem == nil {
		return
	}
	resolution := time.Microsecond
	sec := int64(resolution / time.Second)
	nsec := int64((resolution % time.Second) / time.Nanosecond)
	mem.WriteUint64Le(resultPtr, uint64(sec))
	mem.WriteUint32Le(resultPtr+8, uint32(nsec))
}
