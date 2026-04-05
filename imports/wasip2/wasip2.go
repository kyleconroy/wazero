// Package wasip2 provides Go-defined host functions for WASI Preview 2 (0.2.x).
//
// WASI Preview 2 is based on the WebAssembly Component Model and defines
// interfaces using WIT (WebAssembly Interface Types). This package provides
// host implementations of the standard WASI Preview 2 interfaces:
//
//   - wasi:clocks (monotonic-clock, wall-clock)
//   - wasi:io (error, streams, poll)
//   - wasi:filesystem (types, preopens)
//   - wasi:random (random, insecure, insecure-seed)
//   - wasi:cli (stdin, stdout, stderr, environment, exit)
//   - wasi:sockets (tcp, udp, network, ip-name-lookup)
//   - wasi:http (types, outgoing-handler)
//
// # Usage
//
//	ctx := context.Background()
//	r := wazero.NewRuntime(ctx)
//	defer r.Close(ctx)
//
//	wasip2.MustInstantiate(ctx, r)
//	mod, _ := r.Instantiate(ctx, wasm)
//
// See https://github.com/WebAssembly/WASI/blob/main/wasip2/README.md
package wasip2

import (
	"context"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasip2"
	"github.com/tetratelabs/wazero/internal/wasm"
)

// MustInstantiate calls Instantiate or panics on error.
//
// This is a simpler function for those who know the WASI p2 modules are not
// already instantiated, and don't need to unload them.
func MustInstantiate(ctx context.Context, r wazero.Runtime) {
	if err := Instantiate(ctx, r); err != nil {
		panic(err)
	}
}

// Instantiate instantiates all WASI Preview 2 host modules into the runtime.
//
// Each WASI p2 interface is instantiated as a separate host module following
// the WIT package naming convention (e.g., "wasi:clocks/monotonic-clock@0.2.0").
func Instantiate(ctx context.Context, r wazero.Runtime) error {
	b := NewBuilder(r)
	_, err := b.Instantiate(ctx)
	return err
}

// Builder configures the WASI Preview 2 modules for later use via Compile or Instantiate.
type Builder interface {
	// Compile compiles all WASI p2 host modules.
	Compile(context.Context) ([]wazero.CompiledModule, error)

	// Instantiate instantiates all WASI p2 host modules and returns a function to close them.
	Instantiate(context.Context) (api.Closer, error)
}

// NewBuilder returns a new Builder for configuring WASI Preview 2 modules.
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
			// Clean up any already-instantiated modules.
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
		b.ioErrorModule(),
		b.ioStreamsModule(),
		b.ioPollModule(),
		b.randomModule(),
		b.randomInsecureModule(),
		b.randomInsecureSeedModule(),
		b.filesystemTypesModule(),
		b.filesystemPreopensModule(),
		b.cliStdinModule(),
		b.cliStdoutModule(),
		b.cliStderrModule(),
		b.cliEnvironmentModule(),
		b.cliExitModule(),
		b.socketsTCPModule(),
		b.socketsTCPCreateModule(),
		b.socketsUDPCreateModule(),
		b.socketsInstanceNetworkModule(),
		b.socketsIPNameLookupModule(),
		b.httpTypesModule(),
		b.httpOutgoingHandlerModule(),
	}
}

func (b *builder) monotonicClockModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.ClocksMonotonicClockName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.MonotonicClockResolution)
	exporter.ExportHostFunc(wasip2.MonotonicClockNow)
	exporter.ExportHostFunc(wasip2.MonotonicClockSubscribeDuration)
	exporter.ExportHostFunc(wasip2.MonotonicClockSubscribeInstant)
	return moduleDefinition{name: wasip2.ClocksMonotonicClockName, builder: mod}
}

func (b *builder) wallClockModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.ClocksWallClockName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.WallClockResolution)
	exporter.ExportHostFunc(wasip2.WallClockNow)
	return moduleDefinition{name: wasip2.ClocksWallClockName, builder: mod}
}

func (b *builder) ioErrorModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.IOErrorName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.IOErrorToDebugString)
	return moduleDefinition{name: wasip2.IOErrorName, builder: mod}
}

func (b *builder) ioStreamsModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.IOStreamsName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.StreamsRead)
	exporter.ExportHostFunc(wasip2.StreamsBlockingRead)
	exporter.ExportHostFunc(wasip2.StreamsSubscribe)
	exporter.ExportHostFunc(wasip2.StreamsCheckWrite)
	exporter.ExportHostFunc(wasip2.StreamsWrite)
	exporter.ExportHostFunc(wasip2.StreamsBlockingWriteAndFlush)
	exporter.ExportHostFunc(wasip2.StreamsFlush)
	exporter.ExportHostFunc(wasip2.StreamsBlockingFlush)
	return moduleDefinition{name: wasip2.IOStreamsName, builder: mod}
}

func (b *builder) ioPollModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.IOPollName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.PollPoll)
	return moduleDefinition{name: wasip2.IOPollName, builder: mod}
}

func (b *builder) randomModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.RandomRandomName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.RandomGetRandomBytes)
	exporter.ExportHostFunc(wasip2.RandomGetRandomU64)
	return moduleDefinition{name: wasip2.RandomRandomName, builder: mod}
}

func (b *builder) randomInsecureModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.RandomInsecureName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.InsecureGetRandomBytes)
	exporter.ExportHostFunc(wasip2.InsecureGetRandomU64)
	return moduleDefinition{name: wasip2.RandomInsecureName, builder: mod}
}

func (b *builder) randomInsecureSeedModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.RandomInsecureSeedName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.InsecureSeed)
	return moduleDefinition{name: wasip2.RandomInsecureSeedName, builder: mod}
}

func (b *builder) filesystemTypesModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.FilesystemTypesName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.FilesystemGetType)
	exporter.ExportHostFunc(wasip2.FilesystemStat)
	exporter.ExportHostFunc(wasip2.FilesystemStatAt)
	exporter.ExportHostFunc(wasip2.FilesystemOpenAt)
	exporter.ExportHostFunc(wasip2.FilesystemReadDirectory)
	exporter.ExportHostFunc(wasip2.FilesystemCreateDirectoryAt)
	exporter.ExportHostFunc(wasip2.FilesystemRemoveDirectoryAt)
	exporter.ExportHostFunc(wasip2.FilesystemUnlinkFileAt)
	exporter.ExportHostFunc(wasip2.FilesystemRenameAt)
	exporter.ExportHostFunc(wasip2.FilesystemReadViaStream)
	exporter.ExportHostFunc(wasip2.FilesystemWriteViaStream)
	exporter.ExportHostFunc(wasip2.FilesystemAppendViaStream)
	exporter.ExportHostFunc(wasip2.FilesystemGetFlags)
	return moduleDefinition{name: wasip2.FilesystemTypesName, builder: mod}
}

func (b *builder) filesystemPreopensModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.FilesystemPreopensName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.PreopensGetDirectories)
	return moduleDefinition{name: wasip2.FilesystemPreopensName, builder: mod}
}

func (b *builder) cliStdinModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.CLIStdinName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.CLIGetStdin)
	return moduleDefinition{name: wasip2.CLIStdinName, builder: mod}
}

func (b *builder) cliStdoutModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.CLIStdoutName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.CLIGetStdout)
	return moduleDefinition{name: wasip2.CLIStdoutName, builder: mod}
}

func (b *builder) cliStderrModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.CLIStderrName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.CLIGetStderr)
	return moduleDefinition{name: wasip2.CLIStderrName, builder: mod}
}

func (b *builder) cliEnvironmentModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.CLIEnvironmentName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.CLIGetEnvironment)
	exporter.ExportHostFunc(wasip2.CLIGetArguments)
	return moduleDefinition{name: wasip2.CLIEnvironmentName, builder: mod}
}

func (b *builder) cliExitModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.CLIExitName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.CLIExit)
	return moduleDefinition{name: wasip2.CLIExitName, builder: mod}
}

func (b *builder) socketsTCPModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.SocketsTCPName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.SocketsTCPStartBind)
	exporter.ExportHostFunc(wasip2.SocketsTCPFinishBind)
	exporter.ExportHostFunc(wasip2.SocketsTCPStartConnect)
	exporter.ExportHostFunc(wasip2.SocketsTCPFinishConnect)
	exporter.ExportHostFunc(wasip2.SocketsTCPStartListen)
	exporter.ExportHostFunc(wasip2.SocketsTCPFinishListen)
	exporter.ExportHostFunc(wasip2.SocketsTCPAccept)
	return moduleDefinition{name: wasip2.SocketsTCPName, builder: mod}
}

func (b *builder) socketsTCPCreateModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.SocketsTCPCreateSocketName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.SocketsTCPCreateSocket)
	return moduleDefinition{name: wasip2.SocketsTCPCreateSocketName, builder: mod}
}

func (b *builder) socketsUDPCreateModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.SocketsUDPCreateSocketName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.SocketsUDPCreateSocket)
	return moduleDefinition{name: wasip2.SocketsUDPCreateSocketName, builder: mod}
}

func (b *builder) socketsInstanceNetworkModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.SocketsInstanceNetworkName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.SocketsInstanceNetwork)
	return moduleDefinition{name: wasip2.SocketsInstanceNetworkName, builder: mod}
}

func (b *builder) socketsIPNameLookupModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.SocketsIPNameLookupName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.SocketsIPNameLookup)
	return moduleDefinition{name: wasip2.SocketsIPNameLookupName, builder: mod}
}

func (b *builder) httpTypesModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.HTTPTypesName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.HTTPNewFields)
	exporter.ExportHostFunc(wasip2.HTTPNewOutgoingRequest)
	exporter.ExportHostFunc(wasip2.HTTPIncomingResponseStatus)
	exporter.ExportHostFunc(wasip2.HTTPIncomingResponseHeaders)
	exporter.ExportHostFunc(wasip2.HTTPIncomingResponseConsume)
	exporter.ExportHostFunc(wasip2.HTTPFutureResponseGet)
	exporter.ExportHostFunc(wasip2.HTTPFutureResponseSubscribe)
	return moduleDefinition{name: wasip2.HTTPTypesName, builder: mod}
}

func (b *builder) httpOutgoingHandlerModule() moduleDefinition {
	mod := b.r.NewHostModuleBuilder(wasip2.HTTPOutgoingHandlerName)
	exporter := mod.(wasm.HostFuncExporter)
	exporter.ExportHostFunc(wasip2.HTTPOutgoingHandler)
	return moduleDefinition{name: wasip2.HTTPOutgoingHandlerName, builder: mod}
}
