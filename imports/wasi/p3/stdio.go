package p3

import (
	"context"

	"github.com/tetratelabs/wazero/api"
	internalsys "github.com/tetratelabs/wazero/internal/sys"
	"github.com/tetratelabs/wazero/internal/wasm"
)

// instantiateStdio registers the wasi:cli/stdin, stdout, and stderr host modules.
func (b *builder) instantiateStdio(ctx context.Context) (api.Closer, error) {
	// wasi:cli/stdin - provides get-stdin returning an input-stream handle
	_, err := b.r.NewHostModuleBuilder(CliStdin).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(getStdin), nil, []api.ValueType{i32}).
		WithName("get-stdin").
		Export("get-stdin").
		Instantiate(ctx)
	if err != nil {
		return nil, err
	}

	// wasi:cli/stdout - provides get-stdout returning an output-stream handle
	_, err = b.r.NewHostModuleBuilder(CliStdout).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(getStdout), nil, []api.ValueType{i32}).
		WithName("get-stdout").
		Export("get-stdout").
		Instantiate(ctx)
	if err != nil {
		return nil, err
	}

	// wasi:cli/stderr - provides get-stderr returning an output-stream handle
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

// getStdin implements wasi:cli/stdin.get-stdin.
// Returns a handle to the stdin input stream (fd 0).
func getStdin(_ context.Context, _ api.Module, stack []uint64) {
	stack[0] = api.EncodeU32(uint32(internalsys.FdStdin))
}

// getStdout implements wasi:cli/stdout.get-stdout.
// Returns a handle to the stdout output stream (fd 1).
func getStdout(_ context.Context, _ api.Module, stack []uint64) {
	stack[0] = api.EncodeU32(uint32(internalsys.FdStdout))
}

// getStderr implements wasi:cli/stderr.get-stderr.
// Returns a handle to the stderr output stream (fd 2).
func getStderr(_ context.Context, _ api.Module, stack []uint64) {
	stack[0] = api.EncodeU32(uint32(internalsys.FdStderr))
}

// instantiateStreams registers the wasi:io/streams host module.
func (b *builder) instantiateStreams(ctx context.Context) (api.Closer, error) {
	return b.r.NewHostModuleBuilder(IoStreams).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(streamRead), []api.ValueType{i32, i32, i32}, []api.ValueType{}).
		WithName("[method]input-stream.read").
		Export("[method]input-stream.read").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(streamWrite), []api.ValueType{i32, i32, i32, i32}, []api.ValueType{}).
		WithName("[method]output-stream.blocking-write-and-flush").
		Export("[method]output-stream.blocking-write-and-flush").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(streamDrop), []api.ValueType{i32}, []api.ValueType{}).
		WithName("[resource-drop]input-stream").
		Export("[resource-drop]input-stream").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(streamDrop), []api.ValueType{i32}, []api.ValueType{}).
		WithName("[resource-drop]output-stream").
		Export("[resource-drop]output-stream").
		Instantiate(ctx)
}

// streamRead implements wasi:io/streams [method]input-stream.read.
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
		dataPtr := resultPtr + 8
		mem.Write(dataPtr, buf[:n])
		mem.WriteUint32Le(resultPtr, 0)           // ok discriminant
		mem.WriteUint32Le(resultPtr+4, uint32(n)) // bytes read
	} else {
		mem.WriteUint32Le(resultPtr, 1) // closed/error
	}
}

// streamWrite implements wasi:io/streams [method]output-stream.blocking-write-and-flush.
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
		mem.WriteUint32Le(resultPtr, 1) // error
		return
	}

	fsc := mod.(*wasm.ModuleInstance).Sys.FS()
	f, fOk := fsc.LookupFile(int32(handle))
	if !fOk {
		mem.WriteUint32Le(resultPtr, 1) // error
		return
	}

	n, _ := f.File.Write(data)
	mem.WriteUint32Le(resultPtr, 0)           // ok discriminant
	mem.WriteUint32Le(resultPtr+4, uint32(n)) // bytes written
}

// streamDrop implements resource drop for streams.
func streamDrop(_ context.Context, _ api.Module, _ []uint64) {
	// stdio handles are not actually dropped
}
