// Package wasip3 provides Go-defined host functions for WASI Preview 3 (0.3.x).
//
// WASI Preview 3 extends Preview 2 with native async support in the Component Model,
// enabling composable concurrency. This package provides host implementations that
// build on the wasip2 package, adding async-aware wrappers for the standard interfaces.
//
// Key differences from Preview 2:
//   - Async function ABI: functions can be imported/exported using sync or async ABIs.
//   - Built-in stream and future types replace the resource-based streams of p2.
//   - Seamless composition of sync and async components.
//
// # Usage
//
//	ctx := context.Background()
//	r := wazero.NewRuntime(ctx)
//	defer r.Close(ctx)
//
//	wasip3.MustInstantiate(ctx, r)
//	mod, _ := r.Instantiate(ctx, wasm)
//
// See https://github.com/WebAssembly/WASI/blob/main/wasip3/README.md
package wasip3

import (
	"context"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasip3"
	"github.com/tetratelabs/wazero/internal/wasm"
)

// MustInstantiate calls Instantiate or panics on error.
func MustInstantiate(ctx context.Context, r wazero.Runtime) {
	if err := Instantiate(ctx, r); err != nil {
		panic(err)
	}
}

// Instantiate instantiates all WASI Preview 3 host modules into the runtime.
func Instantiate(ctx context.Context, r wazero.Runtime) error {
	b := NewBuilder(r)
	_, err := b.Instantiate(ctx)
	return err
}

// Builder configures the WASI Preview 3 modules for later use via Compile or Instantiate.
type Builder interface {
	// Compile compiles all WASI p3 host modules.
	Compile(context.Context) ([]wazero.CompiledModule, error)

	// Instantiate instantiates all WASI p3 host modules.
	Instantiate(context.Context) (api.Closer, error)
}

// NewBuilder returns a new Builder for configuring WASI Preview 3 modules.
func NewBuilder(r wazero.Runtime) Builder {
	return &builder{r: r}
}

type builder struct {
	r wazero.Runtime
}

type multiCloser struct {
	closers []api.Closer
}

func (mc *multiCloser) Close(ctx context.Context) error {
	var firstErr error
	for _, c := range mc.closers {
		if err := c.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Compile implements Builder.Compile
func (b *builder) Compile(ctx context.Context) ([]wazero.CompiledModule, error) {
	var compiled []wazero.CompiledModule
	for _, modDef := range b.moduleDefinitions() {
		cm, err := modDef.builder.Compile(ctx)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, cm)
	}
	return compiled, nil
}

// Instantiate implements Builder.Instantiate
func (b *builder) Instantiate(ctx context.Context) (api.Closer, error) {
	mc := &multiCloser{}
	for _, modDef := range b.moduleDefinitions() {
		closer, err := modDef.builder.Instantiate(ctx)
		if err != nil {
			mc.Close(ctx) //nolint
			return nil, err
		}
		mc.closers = append(mc.closers, closer)
	}
	return mc, nil
}

type moduleDefinition struct {
	name    string
	builder wazero.HostModuleBuilder
}

func (b *builder) moduleDefinitions() []moduleDefinition {
	return []moduleDefinition{
		b.monotonicClockModule(),
		b.wallClockModule(),
	}
}

// monotonicClockModule creates the wasi:clocks/monotonic-clock@0.3.0 module.
// In p3, subscribe-duration and subscribe-instant return built-in futures
// instead of pollable resources.
func (b *builder) monotonicClockModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip3.ClocksMonotonicClockName)
	exporter := mod.(wasm.HostFuncExporter)

	// resolution returns the clock resolution in nanoseconds.
	exporter.ExportHostFunc(&wasm.HostFunc{
		ExportName:  "resolution",
		Name:        wasip3.ClocksMonotonicClockName + "#resolution",
		ParamTypes:  []api.ValueType{},
		ParamNames:  []string{},
		ResultTypes: []api.ValueType{wasm.ValueTypeI64},
		ResultNames: []string{"duration"},
		Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = 1 // 1ns resolution
		})},
	})

	// now returns the current monotonic clock value.
	exporter.ExportHostFunc(&wasm.HostFunc{
		ExportName:  "now",
		Name:        wasip3.ClocksMonotonicClockName + "#now",
		ParamTypes:  []api.ValueType{},
		ParamNames:  []string{},
		ResultTypes: []api.ValueType{wasm.ValueTypeI64},
		ResultNames: []string{"instant"},
		Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = uint64(time.Now().UnixNano())
		})},
	})

	// subscribe-duration: in p3, returns a future instead of a pollable.
	exporter.ExportHostFunc(&wasm.HostFunc{
		ExportName:  "subscribe-duration",
		Name:        wasip3.ClocksMonotonicClockName + "#subscribe-duration",
		ParamTypes:  []api.ValueType{wasm.ValueTypeI64},
		ParamNames:  []string{"duration"},
		ResultTypes: []api.ValueType{wasm.ValueTypeI32},
		ResultNames: []string{"future"},
		Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = 0
		})},
	})

	// subscribe-instant: in p3, returns a future instead of a pollable.
	exporter.ExportHostFunc(&wasm.HostFunc{
		ExportName:  "subscribe-instant",
		Name:        wasip3.ClocksMonotonicClockName + "#subscribe-instant",
		ParamTypes:  []api.ValueType{wasm.ValueTypeI64},
		ParamNames:  []string{"instant"},
		ResultTypes: []api.ValueType{wasm.ValueTypeI32},
		ResultNames: []string{"future"},
		Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = 0
		})},
	})

	return moduleDefinition{name: wasip3.ClocksMonotonicClockName, builder: mod}
}

// wallClockModule creates the wasi:clocks/wall-clock@0.3.0 module.
func (b *builder) wallClockModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip3.ClocksWallClockName)
	exporter := mod.(wasm.HostFuncExporter)

	exporter.ExportHostFunc(&wasm.HostFunc{
		ExportName:  "resolution",
		Name:        wasip3.ClocksWallClockName + "#resolution",
		ParamTypes:  []api.ValueType{},
		ParamNames:  []string{},
		ResultTypes: []api.ValueType{wasm.ValueTypeI64, wasm.ValueTypeI32},
		ResultNames: []string{"seconds", "nanoseconds"},
		Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = 0
			stack[1] = 1
		})},
	})

	exporter.ExportHostFunc(&wasm.HostFunc{
		ExportName:  "now",
		Name:        wasip3.ClocksWallClockName + "#now",
		ParamTypes:  []api.ValueType{},
		ParamNames:  []string{},
		ResultTypes: []api.ValueType{wasm.ValueTypeI64, wasm.ValueTypeI32},
		ResultNames: []string{"seconds", "nanoseconds"},
		Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			now := time.Now()
			stack[0] = uint64(now.Unix())
			stack[1] = uint64(now.Nanosecond())
		})},
	})

	return moduleDefinition{name: wasip3.ClocksWallClockName, builder: mod}
}
