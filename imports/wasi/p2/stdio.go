package p2

import (
	"context"

	"github.com/tetratelabs/wazero/api"
	internalsys "github.com/tetratelabs/wazero/internal/sys"
	"github.com/tetratelabs/wazero/internal/wasm"
)

// instantiateStdio registers the wasi:cli/stdin, stdout, and stderr host modules.
func (b *builder) instantiateStdio(ctx context.Context) (api.Closer, error) {
	_, err := b.r.NewHostModuleBuilder(CliStdin).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(getStdin), nil, []api.ValueType{i32}).
		WithName("get-stdin").
		Export("get-stdin").
		Instantiate(ctx)
	if err != nil {
		return nil, err
	}

	_, err = b.r.NewHostModuleBuilder(CliStdout).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(getStdout), nil, []api.ValueType{i32}).
		WithName("get-stdout").
		Export("get-stdout").
		Instantiate(ctx)
	if err != nil {
		return nil, err
	}

	closer, err := b.r.NewHostModuleBuilder(CliStderr).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(getStderr), nil, []api.ValueType{i32}).
		WithName("get-stderr").
		Export("get-stderr").
		Instantiate(ctx)
	if err != nil {
		return nil, err
	}

	return closer, nil
}

func getStdin(_ context.Context, _ api.Module, stack []uint64) {
	stack[0] = api.EncodeU32(uint32(internalsys.FdStdin))
}

func getStdout(_ context.Context, _ api.Module, stack []uint64) {
	stack[0] = api.EncodeU32(uint32(internalsys.FdStdout))
}

func getStderr(_ context.Context, _ api.Module, stack []uint64) {
	stack[0] = api.EncodeU32(uint32(internalsys.FdStderr))
}

// instantiateStreams registers the wasi:io/streams@0.2.0 host module.
func (b *builder) instantiateStreams(ctx context.Context) (api.Closer, error) {
	return b.r.NewHostModuleBuilder(IoStreams).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(streamRead), []api.ValueType{i32, i32, i32}, []api.ValueType{}).
		WithName("[method]input-stream.read").
		Export("[method]input-stream.read").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(streamBlockingRead), []api.ValueType{i32, i32, i32}, []api.ValueType{}).
		WithName("[method]input-stream.blocking-read").
		Export("[method]input-stream.blocking-read").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(streamWrite), []api.ValueType{i32, i32, i32, i32}, []api.ValueType{}).
		WithName("[method]output-stream.write").
		Export("[method]output-stream.write").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(streamBlockingWriteAndFlush), []api.ValueType{i32, i32, i32, i32}, []api.ValueType{}).
		WithName("[method]output-stream.blocking-write-and-flush").
		Export("[method]output-stream.blocking-write-and-flush").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(streamCheckWrite), []api.ValueType{i32, i32}, []api.ValueType{}).
		WithName("[method]output-stream.check-write").
		Export("[method]output-stream.check-write").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(streamFlush), []api.ValueType{i32, i32}, []api.ValueType{}).
		WithName("[method]output-stream.blocking-flush").
		Export("[method]output-stream.blocking-flush").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(streamDrop), []api.ValueType{i32}, []api.ValueType{}).
		WithName("[resource-drop]input-stream").
		Export("[resource-drop]input-stream").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(streamDrop), []api.ValueType{i32}, []api.ValueType{}).
		WithName("[resource-drop]output-stream").
		Export("[resource-drop]output-stream").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(streamSubscribe), []api.ValueType{i32}, []api.ValueType{i32}).
		WithName("[method]input-stream.subscribe").
		Export("[method]input-stream.subscribe").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(streamSubscribe), []api.ValueType{i32}, []api.ValueType{i32}).
		WithName("[method]output-stream.subscribe").
		Export("[method]output-stream.subscribe").
		Instantiate(ctx)
}

// streamRead implements [method]input-stream.read.
func streamRead(_ context.Context, mod api.Module, stack []uint64) {
	handle := api.DecodeU32(stack[0])
	length := api.DecodeU32(stack[1])
	resultPtr := api.DecodeU32(stack[2])

	mem := mod.Memory()
	if mem == nil {
		return
	}

	fsc := mod.(*wasm.ModuleInstance).Sys.FS()
	f, ok := fsc.LookupFile(int32(handle))
	if !ok {
		mem.WriteUint32Le(resultPtr, 1) // error discriminant
		return
	}

	buf := make([]byte, length)
	n, _ := f.File.Read(buf)
	if n > 0 {
		dataPtr := resultPtr + 12
		mem.Write(dataPtr, buf[:n])
		mem.WriteUint32Le(resultPtr, 0)           // ok discriminant
		mem.WriteUint32Le(resultPtr+4, dataPtr)    // data pointer
		mem.WriteUint32Le(resultPtr+8, uint32(n))  // data length
	} else {
		mem.WriteUint32Le(resultPtr, 1) // error/closed
	}
}

// streamBlockingRead implements [method]input-stream.blocking-read (same as read for now).
func streamBlockingRead(ctx context.Context, mod api.Module, stack []uint64) {
	streamRead(ctx, mod, stack)
}

// streamWrite implements [method]output-stream.write.
func streamWrite(_ context.Context, mod api.Module, stack []uint64) {
	handle := api.DecodeU32(stack[0])
	dataPtr := api.DecodeU32(stack[1])
	dataLen := api.DecodeU32(stack[2])
	resultPtr := api.DecodeU32(stack[3])

	mem := mod.Memory()
	if mem == nil {
		return
	}

	data, ok := mem.Read(dataPtr, dataLen)
	if !ok {
		mem.WriteUint32Le(resultPtr, 1)
		return
	}

	fsc := mod.(*wasm.ModuleInstance).Sys.FS()
	f, fOk := fsc.LookupFile(int32(handle))
	if !fOk {
		mem.WriteUint32Le(resultPtr, 1)
		return
	}

	n, _ := f.File.Write(data)
	mem.WriteUint32Le(resultPtr, 0)
	mem.WriteUint32Le(resultPtr+4, uint32(n))
}

// streamBlockingWriteAndFlush implements [method]output-stream.blocking-write-and-flush.
func streamBlockingWriteAndFlush(ctx context.Context, mod api.Module, stack []uint64) {
	streamWrite(ctx, mod, stack)
}

// streamCheckWrite implements [method]output-stream.check-write.
// Returns the number of bytes that can be written without blocking.
func streamCheckWrite(_ context.Context, mod api.Module, stack []uint64) {
	resultPtr := api.DecodeU32(stack[1])
	mem := mod.Memory()
	if mem == nil {
		return
	}
	// Always report ready to write up to 64KB.
	mem.WriteUint32Le(resultPtr, 0)         // ok discriminant
	mem.WriteUint64Le(resultPtr+4, 65536)   // writable bytes
}

// streamFlush implements [method]output-stream.blocking-flush.
func streamFlush(_ context.Context, mod api.Module, stack []uint64) {
	resultPtr := api.DecodeU32(stack[1])
	mem := mod.Memory()
	if mem == nil {
		return
	}
	mem.WriteUint32Le(resultPtr, 0) // ok
}

// streamSubscribe implements [method]input-stream.subscribe / output-stream.subscribe.
// Returns a pollable handle.
func streamSubscribe(_ context.Context, _ api.Module, stack []uint64) {
	handle := api.DecodeU32(stack[0])
	// Return a pollable handle derived from the stream handle.
	stack[0] = api.EncodeU32(handle)
}

// streamDrop is a no-op for stdio handles.
func streamDrop(_ context.Context, _ api.Module, _ []uint64) {}
