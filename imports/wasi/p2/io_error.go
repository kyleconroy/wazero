package p2

import (
	"context"

	"github.com/tetratelabs/wazero/api"
)

// instantiateIoError registers the wasi:io/error@0.2.0 host module.
// This module provides the error resource type used by streams.
func (b *builder) instantiateIoError(ctx context.Context) (api.Closer, error) {
	return b.r.NewHostModuleBuilder(IoError).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(errorDrop), []api.ValueType{i32}, []api.ValueType{}).
		WithName("[resource-drop]error").
		Export("[resource-drop]error").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(errorToDebugString), []api.ValueType{i32, i32}, []api.ValueType{}).
		WithName("[method]error.to-debug-string").
		Export("[method]error.to-debug-string").
		Instantiate(ctx)
}

// errorDrop drops an error resource (no-op).
func errorDrop(_ context.Context, _ api.Module, _ []uint64) {}

// errorToDebugString writes a debug string for an error to the result pointer.
func errorToDebugString(_ context.Context, mod api.Module, stack []uint64) {
	resultPtr := api.DecodeU32(stack[1])
	mem := mod.Memory()
	if mem == nil {
		return
	}
	// Write empty string (ptr=0, len=0).
	mem.WriteUint32Le(resultPtr, 0)
	mem.WriteUint32Le(resultPtr+4, 0)
}
