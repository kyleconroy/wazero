package wasip2

import (
	"context"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
)

// wasi:sockets function names.
const (
	sockTCPStartBindName    = "start-bind"
	sockTCPFinishBindName   = "finish-bind"
	sockTCPStartConnectName = "start-connect"
	sockTCPFinishConnectName = "finish-connect"
	sockTCPStartListenName  = "start-listen"
	sockTCPFinishListenName = "finish-listen"
	sockTCPAcceptName       = "accept"
	sockTCPLocalAddressName = "local-address"
	sockTCPRemoteAddressName = "remote-address"
	sockTCPShutdownName     = "shutdown"
	sockTCPSetKeepAliveName = "set-keep-alive-enabled"
	sockTCPDropName         = "drop-tcp-socket"

	sockTCPCreateSocketName     = "create-tcp-socket"
	sockUDPCreateSocketName     = "create-udp-socket"
	sockInstanceNetworkName     = "instance-network"
	sockNetworkDropName         = "drop-network"

	sockIPNameLookupName        = "resolve-addresses"
	sockIPNameLookupNextName    = "resolve-next-address"
	sockIPNameLookupDropName    = "drop-resolve-address-stream"
)

// SocketsTCPStartBind implements wasi:sockets/tcp.tcp-socket.start-bind.
var SocketsTCPStartBind = &wasm.HostFunc{
	ExportName:  sockTCPStartBindName,
	Name:        SocketsTCPName + "#" + sockTCPStartBindName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"self", "network", "local_address_ptr", "local_address_len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"tag"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 1 // error: not supported yet
	})},
}

// SocketsTCPFinishBind implements wasi:sockets/tcp.tcp-socket.finish-bind.
var SocketsTCPFinishBind = &wasm.HostFunc{
	ExportName:  sockTCPFinishBindName,
	Name:        SocketsTCPName + "#" + sockTCPFinishBindName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"self"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"tag"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 1
	})},
}

// SocketsTCPStartConnect implements wasi:sockets/tcp.tcp-socket.start-connect.
var SocketsTCPStartConnect = &wasm.HostFunc{
	ExportName:  sockTCPStartConnectName,
	Name:        SocketsTCPName + "#" + sockTCPStartConnectName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"self", "network", "remote_address_ptr", "remote_address_len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"tag"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 1
	})},
}

// SocketsTCPFinishConnect implements wasi:sockets/tcp.tcp-socket.finish-connect.
var SocketsTCPFinishConnect = &wasm.HostFunc{
	ExportName:  sockTCPFinishConnectName,
	Name:        SocketsTCPName + "#" + sockTCPFinishConnectName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"self"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "input_stream", "output_stream"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 1
		stack[1] = 0
		stack[2] = 0
	})},
}

// SocketsTCPStartListen implements wasi:sockets/tcp.tcp-socket.start-listen.
var SocketsTCPStartListen = &wasm.HostFunc{
	ExportName:  sockTCPStartListenName,
	Name:        SocketsTCPName + "#" + sockTCPStartListenName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"self"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"tag"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 1
	})},
}

// SocketsTCPFinishListen implements wasi:sockets/tcp.tcp-socket.finish-listen.
var SocketsTCPFinishListen = &wasm.HostFunc{
	ExportName:  sockTCPFinishListenName,
	Name:        SocketsTCPName + "#" + sockTCPFinishListenName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"self"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"tag"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 1
	})},
}

// SocketsTCPAccept implements wasi:sockets/tcp.tcp-socket.accept.
var SocketsTCPAccept = &wasm.HostFunc{
	ExportName:  sockTCPAcceptName,
	Name:        SocketsTCPName + "#" + sockTCPAcceptName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"self"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "client", "input_stream", "output_stream"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 1
		stack[1] = 0
		stack[2] = 0
		stack[3] = 0
	})},
}

// SocketsTCPCreateSocket implements wasi:sockets/tcp-create-socket.create-tcp-socket.
var SocketsTCPCreateSocket = &wasm.HostFunc{
	ExportName:  sockTCPCreateSocketName,
	Name:        SocketsTCPCreateSocketName + "#" + sockTCPCreateSocketName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"address_family"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "socket"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 1 // error
		stack[1] = 0
	})},
}

// SocketsUDPCreateSocket implements wasi:sockets/udp-create-socket.create-udp-socket.
var SocketsUDPCreateSocket = &wasm.HostFunc{
	ExportName:  sockUDPCreateSocketName,
	Name:        SocketsUDPCreateSocketName + "#" + sockUDPCreateSocketName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"address_family"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "socket"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 1 // error
		stack[1] = 0
	})},
}

// SocketsInstanceNetwork implements wasi:sockets/instance-network.instance-network.
var SocketsInstanceNetwork = &wasm.HostFunc{
	ExportName:  sockInstanceNetworkName,
	Name:        SocketsInstanceNetworkName + "#" + sockInstanceNetworkName,
	ParamTypes:  []api.ValueType{},
	ParamNames:  []string{},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"network"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 0 // default network handle
	})},
}

// SocketsIPNameLookup implements wasi:sockets/ip-name-lookup.resolve-addresses.
var SocketsIPNameLookup = &wasm.HostFunc{
	ExportName:  sockIPNameLookupName,
	Name:        SocketsIPNameLookupName + "#" + sockIPNameLookupName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"network", "name_ptr", "name_len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "stream"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 1 // error
		stack[1] = 0
	})},
}
