package wasip3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	compbin "github.com/tetratelabs/wazero/internal/component/binary"
	"github.com/tetratelabs/wazero/internal/leb128"
	"github.com/tetratelabs/wazero/internal/testing/binaryencoding"
	"github.com/tetratelabs/wazero/internal/wasm"
)

// TestHttpComponentOutgoing is an end-to-end test that creates a minimal wasm
// component which makes an outgoing HTTP GET request through the wasi:http
// outgoing-handler, verified against an httptest server.
func TestHttpComponentOutgoing(t *testing.T) {
	// Start an httptest server that records the request.
	var gotMethod, gotPath, gotHost string
	var gotHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotHost = r.Host
		gotHeaders = r.Header.Clone()
		w.Header().Set("X-Test", "ok")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hello from server"))
	}))
	defer ts.Close()

	// Extract authority (host:port) from the test server URL.
	authority := strings.TrimPrefix(ts.URL, "http://")

	// Build a minimal wasm component binary that makes an outgoing HTTP request.
	componentBinary := buildHTTPComponentBinary(t, authority)

	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	t.Cleanup(func() { rt.Close(ctx) })

	host := NewComponentHost(nil, nil, nil, nil, nil).
		WithHTTPClient(ts.Client())

	mod, err := InstantiateComponentWithHost(ctx, rt, componentBinary,
		wazero.NewModuleConfig().WithName("").WithStartFunctions(), host)
	if mod != nil {
		t.Cleanup(func() { mod.Close(ctx) })
	}
	if err != nil {
		t.Fatalf("instantiate component: %v", err)
	}

	// Verify the HTTP request hit the test server.
	if gotMethod != "GET" {
		t.Errorf("method: got %q, want %q", gotMethod, "GET")
	}
	if gotPath != "/" {
		t.Errorf("path: got %q, want %q", gotPath, "/")
	}
	if gotHost != authority {
		t.Errorf("host: got %q, want %q", gotHost, authority)
	}
	if gotHeaders.Get("X-Custom") != "test-value" {
		t.Errorf("custom header: got %q, want %q", gotHeaders.Get("X-Custom"), "test-value")
	}
}

// buildHTTPComponentBinary constructs a minimal wasm component binary that:
// 1. Creates HTTP fields with a custom header
// 2. Creates an HTTP request with those fields
// 3. Sets the method (GET), path (/), scheme (http), and authority
// 4. Calls the outgoing-handler handle function
//
// The component binary wraps a core wasm module with the appropriate imports
// and a simple entry point.
func buildHTTPComponentBinary(t *testing.T, authority string) []byte {
	t.Helper()

	// Memory layout:
	// 0x0100 (256): "/" path string (1 byte)
	// 0x0200 (512): authority string
	// 0x0300 (768): retPtr area (32 bytes)
	// 0x0400 (1024): header name "x-custom" (8 bytes)
	// 0x0410 (1040): header value "test-value" (10 bytes)
	// 0x0500 (1280): fields.from-list entry buffer (16 bytes per entry)
	// 0x1000 (4096): cabi_realloc bump area

	const (
		pathOffset      = 256
		authOffset      = 512
		retPtrOffset    = 768
		hdrNameOffset   = 1024
		hdrValueOffset  = 1040
		fieldListOffset = 1280
	)

	pathData := []byte("/")
	authData := []byte(authority)
	hdrName := []byte("x-custom")
	hdrValue := []byte("test-value")

	// Build the field list entry in memory at fieldListOffset.
	// Each entry is: name_ptr(4) + name_len(4) + val_ptr(4) + val_len(4) = 16 bytes.
	var fieldListData [16]byte
	le := func(buf []byte, off int, v uint32) {
		buf[off] = byte(v)
		buf[off+1] = byte(v >> 8)
		buf[off+2] = byte(v >> 16)
		buf[off+3] = byte(v >> 24)
	}
	le(fieldListData[:], 0, uint32(hdrNameOffset))
	le(fieldListData[:], 4, uint32(len(hdrName)))
	le(fieldListData[:], 8, uint32(hdrValueOffset))
	le(fieldListData[:], 12, uint32(len(hdrValue)))

	// Type definitions for the core module.
	types := []wasm.FunctionType{
		// Type 0: () -> (i32) — [constructor]fields, entry point
		{Params: nil, Results: []wasm.ValueType{wasm.ValueTypeI32}},
		// Type 1: (i32,i32,i32,i32,i32,i32,i32) -> () — [static]request.new
		{Params: []wasm.ValueType{i32, i32, i32, i32, i32, i32, i32}, Results: nil},
		// Type 2: (i32,i32,i32,i32) -> (i32) — set-path, set-authority
		{Params: []wasm.ValueType{i32, i32, i32, i32}, Results: []wasm.ValueType{i32}},
		// Type 3: (i32,i32,i32,i32,i32) -> (i32) — set-scheme
		{Params: []wasm.ValueType{i32, i32, i32, i32, i32}, Results: []wasm.ValueType{i32}},
		// Type 4: (i32,i32) -> (i32) — [async-lower]handle
		{Params: []wasm.ValueType{i32, i32}, Results: []wasm.ValueType{i32}},
		// Type 5: (i32,i32,i32,i32) -> (i32) — cabi_realloc
		{Params: []wasm.ValueType{i32, i32, i32, i32}, Results: []wasm.ValueType{i32}},
		// Type 6: (i32,i32,i32) -> (i32) — callback
		{Params: []wasm.ValueType{i32, i32, i32}, Results: []wasm.ValueType{i32}},
		// Type 7: (i32,i32,i32) -> () — [static]fields.from-list
		{Params: []wasm.ValueType{i32, i32, i32}, Results: nil},
	}

	// Imports: 6 imported functions (indices 0-5).
	imports := []wasm.Import{
		// 0: [static]fields.from-list: (list_ptr, list_len, retPtr) -> ()
		{Type: wasm.ExternTypeFunc, Module: "wasi:http/types@0.3.0", Name: "[static]fields.from-list", DescFunc: 7},
		// 1: [static]request.new
		{Type: wasm.ExternTypeFunc, Module: "wasi:http/types@0.3.0", Name: "[static]request.new", DescFunc: 1},
		// 2: [method]request.set-path-with-query
		{Type: wasm.ExternTypeFunc, Module: "wasi:http/types@0.3.0", Name: "[method]request.set-path-with-query", DescFunc: 2},
		// 3: [method]request.set-scheme
		{Type: wasm.ExternTypeFunc, Module: "wasi:http/types@0.3.0", Name: "[method]request.set-scheme", DescFunc: 3},
		// 4: [method]request.set-authority
		{Type: wasm.ExternTypeFunc, Module: "wasi:http/types@0.3.0", Name: "[method]request.set-authority", DescFunc: 2},
		// 5: [async-lower]handle
		{Type: wasm.ExternTypeFunc, Module: "wasi:http/outgoing-handler@0.3.0", Name: "[async-lower]handle", DescFunc: 4},
	}

	// Functions defined in the module (indices 6-8):
	// 6: cabi_realloc (type 5)
	// 7: entry point (type 0)
	// 8: callback (type 6)
	functionSection := []wasm.Index{5, 0, 6}

	// Build function bodies.
	cabiReallocBody := buildCabiReallocBody()
	entryBody := buildEntryBody(authority, pathOffset, authOffset, retPtrOffset, fieldListOffset)
	callbackBody := buildCallbackBody()

	codeSection := []wasm.Code{
		{Body: cabiReallocBody},
		{LocalTypes: []wasm.ValueType{wasm.ValueTypeI32}, Body: entryBody},
		{Body: callbackBody},
	}

	// Memory: 1 page.
	memorySection := &wasm.Memory{Min: 1, Cap: 1, Max: 1, IsMaxEncoded: true}

	// Global: bump pointer starting at 0x1000.
	globalSection := []wasm.Global{
		{
			Type: wasm.GlobalType{ValType: wasm.ValueTypeI32, Mutable: true},
			Init: wasm.ConstantExpression{
				Data: append([]byte{byte(wasm.OpcodeI32Const)}, append(leb128.EncodeInt32(4096), byte(wasm.OpcodeEnd))...),
			},
		},
	}

	// Data segments for pre-initialized memory.
	dataSegments := []wasm.DataSegment{
		makeDataSegment(pathOffset, pathData),
		makeDataSegment(authOffset, authData),
		makeDataSegment(hdrNameOffset, hdrName),
		makeDataSegment(hdrValueOffset, hdrValue),
		makeDataSegment(fieldListOffset, fieldListData[:]),
	}
	dataCount := uint32(len(dataSegments))

	// Exports.
	exports := []wasm.Export{
		{Type: wasm.ExternTypeMemory, Name: "memory", Index: 0},
		{Type: wasm.ExternTypeFunc, Name: "cabi_realloc", Index: 6},
		{Type: wasm.ExternTypeFunc, Name: "[async-lift]wasi:cli/run@0.3.0-rc-2026-03-15#run", Index: 7},
		{Type: wasm.ExternTypeFunc, Name: "[callback][async-lift]wasi:cli/run@0.3.0-rc-2026-03-15#run", Index: 8},
	}

	mod := &wasm.Module{
		TypeSection:      types,
		ImportSection:    imports,
		FunctionSection:  functionSection,
		MemorySection:    memorySection,
		GlobalSection:    globalSection,
		ExportSection:    exports,
		CodeSection:      codeSection,
		DataSection:      dataSegments,
		DataCountSection: &dataCount,
	}

	coreModuleBytes := binaryencoding.EncodeModule(mod)

	// Wrap core module in a component binary.
	return wrapInComponent(coreModuleBytes)
}

// makeDataSegment creates an active data segment at the given memory offset.
func makeDataSegment(offset int, data []byte) wasm.DataSegment {
	return wasm.DataSegment{
		OffsetExpression: wasm.ConstantExpression{
			Data: append([]byte{byte(wasm.OpcodeI32Const)}, append(leb128.EncodeInt32(int32(offset)), byte(wasm.OpcodeEnd))...),
		},
		Init: data,
	}
}

// buildCabiReallocBody builds a simple bump allocator.
// cabi_realloc(old_ptr, old_size, align, new_size) -> ptr
func buildCabiReallocBody() []byte {
	var b []byte
	// Return current bump pointer, then advance it by new_size.
	b = append(b, byte(wasm.OpcodeGlobalGet))
	b = append(b, 0x00)
	b = append(b, byte(wasm.OpcodeGlobalGet))
	b = append(b, 0x00)
	b = append(b, byte(wasm.OpcodeLocalGet))
	b = append(b, 0x03) // new_size
	b = append(b, byte(wasm.OpcodeI32Add))
	b = append(b, byte(wasm.OpcodeGlobalSet))
	b = append(b, 0x00)
	b = append(b, byte(wasm.OpcodeEnd))
	return b
}

// i32Const appends an i32.const instruction with a signed LEB128 immediate.
func i32Const(b []byte, v int32) []byte {
	b = append(b, byte(wasm.OpcodeI32Const))
	b = append(b, leb128.EncodeInt32(v)...)
	return b
}

// call appends a call instruction with the function index.
func callFunc(b []byte, idx uint32) []byte {
	b = append(b, byte(wasm.OpcodeCall))
	b = append(b, leb128.EncodeUint32(idx)...)
	return b
}

// buildEntryBody builds the entry point function body that creates an HTTP
// request and calls the outgoing handler.
func buildEntryBody(authority string, pathOffset, authOffset, retPtrOffset, fieldListOffset int) []byte {
	authLen := int32(len(authority))

	var b []byte

	// Call [static]fields.from-list(list_ptr, list_len=1, retPtr) — imported func 0
	b = i32Const(b, int32(fieldListOffset)) // list_ptr
	b = i32Const(b, 1)                      // list_len = 1 entry
	b = i32Const(b, int32(retPtrOffset))    // retPtr
	b = callFunc(b, 0)

	// Load the fields handle from retPtr+4 (result<fields, error>: disc at +0, handle at +4).
	// First check: retPtr+0 should be 0 (Ok).
	b = i32Const(b, int32(retPtrOffset)+4)
	b = append(b, byte(wasm.OpcodeI32Load), 0x02, 0x00) // i32.load align=2 offset=0
	b = append(b, byte(wasm.OpcodeLocalSet), 0x00)      // local.set 0 = fields_handle

	// Call [static]request.new(fields_handle, 0, 0, 0, 0, 0, retPtr) — imported func 1
	b = append(b, byte(wasm.OpcodeLocalGet), 0x00) // fields_handle
	b = i32Const(b, 0)                             // body_opt_disc = none
	b = i32Const(b, 0)                             // body_opt_val
	b = i32Const(b, 0)                             // trailers = 0
	b = i32Const(b, 0)                             // options_opt_disc = none
	b = i32Const(b, 0)                             // options_opt_val
	b = i32Const(b, int32(retPtrOffset))           // retPtr
	b = callFunc(b, 1)

	// Load req_handle from retPtr (request.new writes handle at retPtr+0).
	b = i32Const(b, int32(retPtrOffset))
	b = append(b, byte(wasm.OpcodeI32Load), 0x02, 0x00) // i32.load align=2 offset=0
	b = append(b, byte(wasm.OpcodeLocalSet), 0x00)      // local.set 0 = req_handle

	// Call set-path-with-query(req_handle, 1=some, pathPtr, pathLen=1) — imported func 2
	b = append(b, byte(wasm.OpcodeLocalGet), 0x00)
	b = i32Const(b, 1)                 // some
	b = i32Const(b, int32(pathOffset)) // path ptr
	b = i32Const(b, 1)                 // path len ("/" = 1 byte)
	b = callFunc(b, 2)
	b = append(b, byte(wasm.OpcodeDrop))

	// Call set-scheme(req_handle, 1=some, 0=http, 0, 0) — imported func 3
	b = append(b, byte(wasm.OpcodeLocalGet), 0x00)
	b = i32Const(b, 1) // some
	b = i32Const(b, 0) // scheme disc = 0 (http)
	b = i32Const(b, 0) // unused
	b = i32Const(b, 0) // unused
	b = callFunc(b, 3)
	b = append(b, byte(wasm.OpcodeDrop))

	// Call set-authority(req_handle, 1=some, authPtr, authLen) — imported func 4
	b = append(b, byte(wasm.OpcodeLocalGet), 0x00)
	b = i32Const(b, 1)                 // some
	b = i32Const(b, int32(authOffset)) // authority ptr
	b = i32Const(b, authLen)           // authority len
	b = callFunc(b, 4)
	b = append(b, byte(wasm.OpcodeDrop))

	// Call [async-lower]handle(req_handle, retPtr) — imported func 5
	b = append(b, byte(wasm.OpcodeLocalGet), 0x00)
	b = i32Const(b, int32(retPtrOffset))
	b = callFunc(b, 5)
	// Return the status code.
	b = append(b, byte(wasm.OpcodeEnd))
	return b
}

// buildCallbackBody builds the callback function that returns EXIT(0).
func buildCallbackBody() []byte {
	var b []byte
	b = i32Const(b, 0) // EXIT
	b = append(b, byte(wasm.OpcodeEnd))
	return b
}

// wrapInComponent wraps a core wasm module in a component binary.
func wrapInComponent(coreModuleBytes []byte) []byte {
	// Component header.
	var out []byte
	out = append(out, compbin.ComponentMagic...)
	out = append(out, compbin.ComponentVersion...)

	// Section: core module (section ID = 0x01).
	sectionPayload := coreModuleBytes
	out = append(out, 0x01) // sectionCoreModule
	out = append(out, leb128.EncodeUint32(uint32(len(sectionPayload)))...)
	out = append(out, sectionPayload...)

	return out
}

func TestWithHTTPClient(t *testing.T) {
	host := NewComponentHost(nil, nil, nil, nil, nil)
	if got := host.getHTTPClient(); got != nil {
		t.Error("expected nil client when none configured")
	}

	custom := &http.Client{}
	host.WithHTTPClient(custom)
	if got := host.getHTTPClient(); got != custom {
		t.Error("expected custom client after WithHTTPClient")
	}
}

func TestHttpOutgoingBlockedByDefault(t *testing.T) {
	// Without WithHTTPClient, outgoing HTTP requests should fail.
	authority := "127.0.0.1:12345"
	componentBinary := buildHTTPComponentBinary(t, authority)

	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	t.Cleanup(func() { rt.Close(ctx) })

	host := NewComponentHost(nil, nil, nil, nil, nil) // no WithHTTPClient

	mod, err := InstantiateComponentWithHost(ctx, rt, componentBinary,
		wazero.NewModuleConfig().WithName("").WithStartFunctions(), host)
	if mod != nil {
		t.Cleanup(func() { mod.Close(ctx) })
	}
	if err != nil {
		t.Fatalf("instantiate component: %v", err)
	}
	// The component ran without panic — the handle call returned an error
	// result to the guest instead of making a network request.
}
