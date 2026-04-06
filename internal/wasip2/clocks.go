package wasip2

import (
	"context"
	"time"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
)

const (
	clockResGetName            = "resolution"
	clockTimeGetName           = "now"
	clockSubscribeDurationName = "subscribe-duration"
	clockSubscribeInstantName  = "subscribe-instant"

	wallClockResGetName = "resolution"
	wallClockNowName    = "now"
)

// MonotonicClockResolution implements wasi:clocks/monotonic-clock.resolution.
// Returns the resolution of the monotonic clock in nanoseconds.
var MonotonicClockResolution = &wasm.HostFunc{
	ExportName:  clockResGetName,
	Name:        ClocksMonotonicClockName + "#" + clockResGetName,
	ParamTypes:  []api.ValueType{},
	ParamNames:  []string{},
	ResultTypes: []api.ValueType{wasm.ValueTypeI64},
	ResultNames: []string{"duration"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		// Return nanosecond resolution (1ns).
		stack[0] = 1
	})},
}

// MonotonicClockNow implements wasi:clocks/monotonic-clock.now.
// Returns the current value of the monotonic clock in nanoseconds.
var MonotonicClockNow = &wasm.HostFunc{
	ExportName:  clockTimeGetName,
	Name:        ClocksMonotonicClockName + "#" + clockTimeGetName,
	ParamTypes:  []api.ValueType{},
	ParamNames:  []string{},
	ResultTypes: []api.ValueType{wasm.ValueTypeI64},
	ResultNames: []string{"instant"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = uint64(time.Now().UnixNano())
	})},
}

// MonotonicClockSubscribeDuration implements wasi:clocks/monotonic-clock.subscribe-duration.
// Creates a pollable that resolves after the given duration in nanoseconds.
var MonotonicClockSubscribeDuration = &wasm.HostFunc{
	ExportName:  clockSubscribeDurationName,
	Name:        ClocksMonotonicClockName + "#" + clockSubscribeDurationName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI64},
	ParamNames:  []string{"duration"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"pollable"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		// Return a pollable handle. In a full implementation, this would create
		// a timer-based pollable. For now, return a stub handle.
		stack[0] = 0
	})},
}

// MonotonicClockSubscribeInstant implements wasi:clocks/monotonic-clock.subscribe-instant.
// Creates a pollable that resolves at the given instant in nanoseconds.
var MonotonicClockSubscribeInstant = &wasm.HostFunc{
	ExportName:  clockSubscribeInstantName,
	Name:        ClocksMonotonicClockName + "#" + clockSubscribeInstantName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI64},
	ParamNames:  []string{"instant"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"pollable"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 0
	})},
}

// WallClockResolution implements wasi:clocks/wall-clock.resolution.
var WallClockResolution = &wasm.HostFunc{
	ExportName:  wallClockResGetName,
	Name:        ClocksWallClockName + "#" + wallClockResGetName,
	ParamTypes:  []api.ValueType{},
	ParamNames:  []string{},
	ResultTypes: []api.ValueType{wasm.ValueTypeI64, wasm.ValueTypeI32},
	ResultNames: []string{"seconds", "nanoseconds"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		// 1 nanosecond resolution.
		stack[0] = 0 // seconds
		stack[1] = 1 // nanoseconds
	})},
}

// WallClockNow implements wasi:clocks/wall-clock.now.
var WallClockNow = &wasm.HostFunc{
	ExportName:  wallClockNowName,
	Name:        ClocksWallClockName + "#" + wallClockNowName,
	ParamTypes:  []api.ValueType{},
	ParamNames:  []string{},
	ResultTypes: []api.ValueType{wasm.ValueTypeI64, wasm.ValueTypeI32},
	ResultNames: []string{"seconds", "nanoseconds"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		now := time.Now()
		stack[0] = uint64(now.Unix())
		stack[1] = uint64(now.Nanosecond())
	})},
}
