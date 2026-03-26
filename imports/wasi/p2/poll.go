package p2

import (
	"context"

	"github.com/tetratelabs/wazero/api"
)

// instantiatePoll registers the wasi:io/poll@0.2.0 host module.
// Poll provides the pollable resource type and the poll function used for
// synchronous I/O multiplexing in WASI P2.
func (b *builder) instantiatePoll(ctx context.Context) (api.Closer, error) {
	return b.r.NewHostModuleBuilder(IoPoll).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(pollList), []api.ValueType{i32, i32, i32}, []api.ValueType{}).
		WithName("poll").
		Export("poll").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(pollableDrop), []api.ValueType{i32}, []api.ValueType{}).
		WithName("[resource-drop]pollable").
		Export("[resource-drop]pollable").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(pollableBlock), []api.ValueType{i32}, []api.ValueType{}).
		WithName("[method]pollable.block").
		Export("[method]pollable.block").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(pollableReady), []api.ValueType{i32}, []api.ValueType{i32}).
		WithName("[method]pollable.ready").
		Export("[method]pollable.ready").
		Instantiate(ctx)
}

// pollList implements wasi:io/poll.poll.
// Takes a list<borrow<pollable>> and returns a list<u32> of ready indices.
func pollList(_ context.Context, mod api.Module, stack []uint64) {
	listPtr := api.DecodeU32(stack[0])
	listLen := api.DecodeU32(stack[1])
	resultPtr := api.DecodeU32(stack[2])

	mem := mod.Memory()
	if mem == nil {
		return
	}

	// For the basic implementation, report all pollables as immediately ready.
	// Write result list: pointer to data, then count.
	dataPtr := resultPtr + 8
	for i := uint32(0); i < listLen; i++ {
		mem.WriteUint32Le(dataPtr+i*4, i)
	}
	mem.WriteUint32Le(resultPtr, dataPtr)
	mem.WriteUint32Le(resultPtr+4, listLen)

	_ = listPtr // consumed
}

// pollableDrop drops a pollable resource (no-op for built-in pollables).
func pollableDrop(_ context.Context, _ api.Module, _ []uint64) {}

// pollableBlock implements [method]pollable.block - blocks until ready.
// For our implementation, all pollables are immediately ready.
func pollableBlock(_ context.Context, _ api.Module, _ []uint64) {}

// pollableReady implements [method]pollable.ready - returns whether the pollable is ready.
func pollableReady(_ context.Context, _ api.Module, stack []uint64) {
	stack[0] = 1 // always ready
}
