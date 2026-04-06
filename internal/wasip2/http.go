package wasip2

import (
	"context"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
)

// wasi:http function names.
const (
	httpNewFieldsName     = "new-fields"
	httpFieldsGetName     = "get"
	httpFieldsSetName     = "set"
	httpFieldsDeleteName  = "delete"
	httpFieldsAppendName  = "append"
	httpFieldsEntriesName = "entries"
	httpFieldsCloneName   = "clone"
	httpFieldsDropName    = "drop-fields"

	httpNewOutgoingRequestName       = "new-outgoing-request"
	httpOutgoingRequestBodyName      = "outgoing-request-body"
	httpOutgoingRequestMethodName    = "outgoing-request-method"
	httpOutgoingRequestPathName      = "outgoing-request-path-with-query"
	httpOutgoingRequestSchemeName    = "outgoing-request-scheme"
	httpOutgoingRequestAuthorityName = "outgoing-request-authority"
	httpOutgoingRequestHeadersName   = "outgoing-request-headers"
	httpDropOutgoingRequestName      = "drop-outgoing-request"

	httpOutgoingHandlerHandleName = "handle"

	httpIncomingResponseStatusName  = "incoming-response-status"
	httpIncomingResponseHeadersName = "incoming-response-headers"
	httpIncomingResponseConsumeName = "incoming-response-consume"
	httpDropIncomingResponseName    = "drop-incoming-response"

	httpFutureResponseGetName       = "future-incoming-response-get"
	httpFutureResponseSubscribeName = "future-incoming-response-subscribe"
	httpDropFutureResponseName      = "drop-future-incoming-response"
)

// HTTPOutgoingHandler implements wasi:http/outgoing-handler.handle.
var HTTPOutgoingHandler = &wasm.HostFunc{
	ExportName:  httpOutgoingHandlerHandleName,
	Name:        HTTPOutgoingHandlerName + "#" + httpOutgoingHandlerHandleName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"request", "options"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "future_response"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		// Return error: not supported yet.
		stack[0] = 1
		stack[1] = 0
	})},
}

// HTTPNewFields implements wasi:http/types.new-fields.
var HTTPNewFields = &wasm.HostFunc{
	ExportName:  httpNewFieldsName,
	Name:        HTTPTypesName + "#" + httpNewFieldsName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"entries_ptr", "entries_len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "fields"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 0
		stack[1] = 0
	})},
}

// HTTPNewOutgoingRequest implements wasi:http/types.new-outgoing-request.
var HTTPNewOutgoingRequest = &wasm.HostFunc{
	ExportName:  httpNewOutgoingRequestName,
	Name:        HTTPTypesName + "#" + httpNewOutgoingRequestName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"headers"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"request"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 0
	})},
}

// HTTPIncomingResponseStatus implements wasi:http/types.incoming-response-status.
var HTTPIncomingResponseStatus = &wasm.HostFunc{
	ExportName:  httpIncomingResponseStatusName,
	Name:        HTTPTypesName + "#" + httpIncomingResponseStatusName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"response"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"status"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 200
	})},
}

// HTTPIncomingResponseHeaders implements wasi:http/types.incoming-response-headers.
var HTTPIncomingResponseHeaders = &wasm.HostFunc{
	ExportName:  httpIncomingResponseHeadersName,
	Name:        HTTPTypesName + "#" + httpIncomingResponseHeadersName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"response"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"headers"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 0
	})},
}

// HTTPIncomingResponseConsume implements wasi:http/types.incoming-response-consume.
var HTTPIncomingResponseConsume = &wasm.HostFunc{
	ExportName:  httpIncomingResponseConsumeName,
	Name:        HTTPTypesName + "#" + httpIncomingResponseConsumeName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"response"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "body"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 0
		stack[1] = 0
	})},
}

// HTTPFutureResponseGet implements wasi:http/types.future-incoming-response-get.
var HTTPFutureResponseGet = &wasm.HostFunc{
	ExportName:  httpFutureResponseGetName,
	Name:        HTTPTypesName + "#" + httpFutureResponseGetName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"future"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "result"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 0
		stack[1] = 0
	})},
}

// HTTPFutureResponseSubscribe implements wasi:http/types.future-incoming-response-subscribe.
var HTTPFutureResponseSubscribe = &wasm.HostFunc{
	ExportName:  httpFutureResponseSubscribeName,
	Name:        HTTPTypesName + "#" + httpFutureResponseSubscribeName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"future"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"pollable"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		stack[0] = 0
	})},
}
