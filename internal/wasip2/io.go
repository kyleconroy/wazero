package wasip2

import (
	"context"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
)

// wasi:io/error function names.
const (
	ioErrorToDebugStringName = "to-debug-string"
)

// wasi:io/streams function names.
const (
	streamsReadName              = "read"
	streamsBlockingReadName      = "blocking-read"
	streamsSkipName              = "skip"
	streamsBlockingSkipName      = "blocking-skip"
	streamsSubscribeName         = "subscribe"
	streamsDropInputStreamName   = "drop-input-stream"
	streamsCheckWriteName        = "check-write"
	streamsWriteName             = "write"
	streamsBlockingWriteAndFlush = "blocking-write-and-flush"
	streamsFlushName             = "flush"
	streamsBlockingFlushName     = "blocking-flush"
	streamsWriteZeroesName       = "write-zeroes"
	streamsSpliceName            = "splice"
	streamsBlockingSpliceName    = "blocking-splice"
	streamsSubscribeOutputName   = "subscribe-output"
	streamsDropOutputStreamName  = "drop-output-stream"
)

// wasi:io/poll function names.
const (
	pollPollName = "poll"
)

// IOErrorToDebugString implements wasi:io/error.to-debug-string.
var IOErrorToDebugString = &wasm.HostFunc{
	ExportName:  ioErrorToDebugStringName,
	Name:        IOErrorName + "#" + ioErrorToDebugStringName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"error"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"ptr", "len"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
		// Return an empty string for now.
		stack[0] = 0
		stack[1] = 0
	})},
}

// StreamsRead implements wasi:io/streams.input-stream.read.
var StreamsRead = &wasm.HostFunc{
	ExportName:  streamsReadName,
	Name:        IOStreamsName + "#" + streamsReadName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI64},
	ParamNames:  []string{"self", "len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "ptr", "len"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		// tag=0 means ok, return empty data.
		stack[0] = 0
		stack[1] = 0
		stack[2] = 0
	})},
}

// StreamsBlockingRead implements wasi:io/streams.input-stream.blocking-read.
var StreamsBlockingRead = &wasm.HostFunc{
	ExportName:  streamsBlockingReadName,
	Name:        IOStreamsName + "#" + streamsBlockingReadName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI64},
	ParamNames:  []string{"self", "len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "ptr", "len"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 0
		stack[1] = 0
		stack[2] = 0
	})},
}

// StreamsSubscribe implements wasi:io/streams.input-stream.subscribe.
var StreamsSubscribe = &wasm.HostFunc{
	ExportName:  streamsSubscribeName,
	Name:        IOStreamsName + "#" + streamsSubscribeName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"self"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"pollable"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 0
	})},
}

// StreamsCheckWrite implements wasi:io/streams.output-stream.check-write.
var StreamsCheckWrite = &wasm.HostFunc{
	ExportName:  streamsCheckWriteName,
	Name:        IOStreamsName + "#" + streamsCheckWriteName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"self"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI64},
	ResultNames: []string{"tag", "value"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		// tag=0 means ok, return a large writable size.
		stack[0] = 0
		stack[1] = 65536
	})},
}

// StreamsWrite implements wasi:io/streams.output-stream.write.
var StreamsWrite = &wasm.HostFunc{
	ExportName:  streamsWriteName,
	Name:        IOStreamsName + "#" + streamsWriteName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"self", "ptr", "len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"tag"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 0 // ok
	})},
}

// StreamsBlockingWriteAndFlush implements wasi:io/streams.output-stream.blocking-write-and-flush.
var StreamsBlockingWriteAndFlush = &wasm.HostFunc{
	ExportName:  streamsBlockingWriteAndFlush,
	Name:        IOStreamsName + "#" + streamsBlockingWriteAndFlush,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"self", "ptr", "len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"tag"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 0 // ok
	})},
}

// StreamsFlush implements wasi:io/streams.output-stream.flush.
var StreamsFlush = &wasm.HostFunc{
	ExportName:  streamsFlushName,
	Name:        IOStreamsName + "#" + streamsFlushName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"self"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"tag"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 0
	})},
}

// StreamsBlockingFlush implements wasi:io/streams.output-stream.blocking-flush.
var StreamsBlockingFlush = &wasm.HostFunc{
	ExportName:  streamsBlockingFlushName,
	Name:        IOStreamsName + "#" + streamsBlockingFlushName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"self"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"tag"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 0
	})},
}

// PollPoll implements wasi:io/poll.poll.
var PollPoll = &wasm.HostFunc{
	ExportName:  pollPollName,
	Name:        IOPollName + "#" + pollPollName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"in_ptr", "in_len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"ptr", "len"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		// Return an empty list of ready pollables.
		stack[0] = 0
		stack[1] = 0
	})},
}
