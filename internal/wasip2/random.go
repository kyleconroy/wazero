package wasip2

import (
	"context"
	"crypto/rand"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
)

const (
	randomGetRandomBytesName   = "get-random-bytes"
	randomGetRandomU64Name     = "get-random-u64"
	insecureGetRandomBytesName = "get-insecure-random-bytes"
	insecureGetRandomU64Name   = "get-insecure-random-u64"
	insecureSeedName           = "insecure-seed"
)

// RandomGetRandomBytes implements wasi:random/random.get-random-bytes.
// Writes cryptographically secure random bytes to the guest's memory.
var RandomGetRandomBytes = &wasm.HostFunc{
	ExportName:  randomGetRandomBytesName,
	Name:        RandomRandomName + "#" + randomGetRandomBytesName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI64},
	ParamNames:  []string{"len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"ptr"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		length := uint32(stack[0])

		buf := make([]byte, length)
		_, _ = rand.Read(buf)

		// In a canonical ABI implementation, the guest provides a realloc function
		// and we write into their memory. For now, write directly if possible.
		if mem := mod.Memory(); mem != nil && length > 0 {
			// The caller is expected to pass a pointer to write to via
			// the canonical ABI. For now, return 0 to indicate the start.
			mem.Write(0, buf)
		}
		stack[0] = 0
	})},
}

// RandomGetRandomU64 implements wasi:random/random.get-random-u64.
var RandomGetRandomU64 = &wasm.HostFunc{
	ExportName:  randomGetRandomU64Name,
	Name:        RandomRandomName + "#" + randomGetRandomU64Name,
	ParamTypes:  []api.ValueType{},
	ParamNames:  []string{},
	ResultTypes: []api.ValueType{wasm.ValueTypeI64},
	ResultNames: []string{"value"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		var buf [8]byte
		_, _ = rand.Read(buf[:])
		stack[0] = uint64(buf[0]) | uint64(buf[1])<<8 | uint64(buf[2])<<16 | uint64(buf[3])<<24 |
			uint64(buf[4])<<32 | uint64(buf[5])<<40 | uint64(buf[6])<<48 | uint64(buf[7])<<56
	})},
}

// InsecureGetRandomBytes implements wasi:random/insecure.get-insecure-random-bytes.
var InsecureGetRandomBytes = &wasm.HostFunc{
	ExportName:  insecureGetRandomBytesName,
	Name:        RandomInsecureName + "#" + insecureGetRandomBytesName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI64},
	ParamNames:  []string{"len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"ptr"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		length := uint32(stack[0])
		buf := make([]byte, length)
		_, _ = rand.Read(buf)
		if mem := mod.Memory(); mem != nil && length > 0 {
			mem.Write(0, buf)
		}
		stack[0] = 0
	})},
}

// InsecureGetRandomU64 implements wasi:random/insecure.get-insecure-random-u64.
var InsecureGetRandomU64 = &wasm.HostFunc{
	ExportName:  insecureGetRandomU64Name,
	Name:        RandomInsecureName + "#" + insecureGetRandomU64Name,
	ParamTypes:  []api.ValueType{},
	ParamNames:  []string{},
	ResultTypes: []api.ValueType{wasm.ValueTypeI64},
	ResultNames: []string{"value"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		var buf [8]byte
		_, _ = rand.Read(buf[:])
		stack[0] = uint64(buf[0]) | uint64(buf[1])<<8 | uint64(buf[2])<<16 | uint64(buf[3])<<24 |
			uint64(buf[4])<<32 | uint64(buf[5])<<40 | uint64(buf[6])<<48 | uint64(buf[7])<<56
	})},
}

// InsecureSeed implements wasi:random/insecure-seed.insecure-seed.
var InsecureSeed = &wasm.HostFunc{
	ExportName:  insecureSeedName,
	Name:        RandomInsecureSeedName + "#" + insecureSeedName,
	ParamTypes:  []api.ValueType{},
	ParamNames:  []string{},
	ResultTypes: []api.ValueType{wasm.ValueTypeI64, wasm.ValueTypeI64},
	ResultNames: []string{"high", "low"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		var buf [16]byte
		_, _ = rand.Read(buf[:])
		stack[0] = uint64(buf[0]) | uint64(buf[1])<<8 | uint64(buf[2])<<16 | uint64(buf[3])<<24 |
			uint64(buf[4])<<32 | uint64(buf[5])<<40 | uint64(buf[6])<<48 | uint64(buf[7])<<56
		stack[1] = uint64(buf[8]) | uint64(buf[9])<<8 | uint64(buf[10])<<16 | uint64(buf[11])<<24 |
			uint64(buf[12])<<32 | uint64(buf[13])<<40 | uint64(buf[14])<<48 | uint64(buf[15])<<56
	})},
}
