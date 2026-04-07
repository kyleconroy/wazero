package wasip3

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/sys"
)

// i32, i64 shorthand for wasm value types.
const (
	i32 = api.ValueTypeI32
	i64 = api.ValueTypeI64
)

// ResourceTable manages resource handles for the component model.
// Resources are identified by integer handles (i32) and can be created, looked up, and dropped.
type ResourceTable struct {
	mu      sync.Mutex
	nextID  uint32
	entries map[uint32]interface{}
}

func newResourceTable() *ResourceTable {
	return &ResourceTable{
		nextID:  1,
		entries: make(map[uint32]interface{}),
	}
}

func (rt *ResourceTable) New(val interface{}) uint32 {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	id := rt.nextID
	rt.nextID++
	rt.entries[id] = val
	return id
}

func (rt *ResourceTable) Get(id uint32) (interface{}, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	v, ok := rt.entries[id]
	return v, ok
}

func (rt *ResourceTable) Set(id uint32, val interface{}) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.entries[id] = val
}

func (rt *ResourceTable) Drop(id uint32) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.entries, id)
}

// ComponentHost provides the WASI host function implementations for component model instantiation.
// It manages resources (streams, pollables, etc.) and provides the canonical ABI lowered functions.
type ComponentHost struct {
	resources *ResourceTable

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	args   []string
	env    [][2]string

	// randSource is the random number source.
	randSource io.Reader

	// exitCode is set when exit() is called.
	exitCode atomic.Int32
	exited   atomic.Bool

	// For filesystem preopens (if any).
	preopens []preopen

	// taskReturnValue stores the result of task-return.
	taskReturnValue atomic.Int32

	// savedContext0 is the last non-zero value of context slot 0,
	// used to restore context before callback invocations.
	savedContext0 atomic.Int64

	// contextRestore restores context slot 0 before callback calls.
	contextRestore func()

	// insecureSeed caches the insecure seed (must be same on every call).
	insecureSeed     [16]byte
	insecureSeedOnce sync.Once

	// subtasks tracks pending async subtasks for the component model protocol.
	subtasks subtaskTable

	// asyncEvents receives events from background goroutines (accept, etc.)
	// for delivery through pollEvent in the callback loop.
	asyncEvents chan asyncEvent

	// pendingOps tracks the number of pending background operations.
	pendingOps atomic.Int32

	// ctx is the context passed during setup, used for cabiRealloc calls in pollEvent.
	ctx context.Context
}

// asyncEvent represents a waitable-set-poll event.
type asyncEvent struct {
	eventType uint32 // STREAM_READ=2, STREAM_WRITE=3, FUTURE_READ=4, etc.
	p1        uint32 // waitable index / stream handle
	p2        uint32 // result code (e.g., COMPLETED(1))
}

// subtask represents a pending async-lower subtask.
type subtask struct {
	retPtr    uint32                            // where to write results in guest memory
	results   []byte                            // the serialized results to write
	resultsFn func(context.Context, api.Module) // called to write results (for list allocation)
	ready     bool                              // whether the subtask has completed
}

// subtaskTable manages subtask indices and pending results.
type subtaskTable struct {
	mu      sync.Mutex
	entries map[uint32]*subtask
	nextIdx uint32
}

func (st *subtaskTable) New(retPtr uint32, results []byte) uint32 {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.entries == nil {
		st.entries = make(map[uint32]*subtask)
	}
	st.nextIdx++
	idx := st.nextIdx
	st.entries[idx] = &subtask{retPtr: retPtr, results: results, ready: true}
	return idx
}

// NewPending creates a subtask that is not yet ready.
// Call Complete() to mark it ready and provide results.
func (st *subtaskTable) NewPending(retPtr uint32) uint32 {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.entries == nil {
		st.entries = make(map[uint32]*subtask)
	}
	st.nextIdx++
	idx := st.nextIdx
	st.entries[idx] = &subtask{retPtr: retPtr, ready: false}
	return idx
}

// Complete marks a pending subtask as ready with its results.
func (st *subtaskTable) Complete(idx uint32, results []byte, resultsFn func(context.Context, api.Module)) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if s, ok := st.entries[idx]; ok {
		s.results = results
		s.resultsFn = resultsFn
		s.ready = true
	}
}

func (st *subtaskTable) PopReady() (uint32, *subtask) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for idx, s := range st.entries {
		if s.ready {
			delete(st.entries, idx)
			return idx, s
		}
	}
	return 0, nil
}

func (st *subtaskTable) Drop(idx uint32) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.entries, idx)
}

type preopen struct {
	fs        preopenFS // filesystem operations
	guestPath string    // guest-visible path
}

// streamResource represents one end of a stream. Both ends of a pair
// point to the same shared resource so that data written to one end
// can be read from the other.
type streamResource struct {
	reader      io.Reader
	writer      io.Writer
	pendingData []byte // cached data from background read
	pendingEOF  bool   // background read hit EOF
}

// futureResource represents a future (single-value async result) resource.
// Both ends (readable, writable) of a future pair share the same instance.
type futureResource struct {
	result []byte // the serialized result to deliver when read
	ready  bool   // whether the future has a value
}

// pollableResource represents a pollable resource.
type pollableResource struct {
	ready bool
}

// NewComponentHost creates a new ComponentHost with the given configuration.
func NewComponentHost(stdin io.Reader, stdout, stderr io.Writer, args []string, env [][2]string) *ComponentHost {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	return &ComponentHost{
		resources:   newResourceTable(),
		stdin:       stdin,
		stdout:      stdout,
		stderr:      stderr,
		args:        args,
		env:         env,
		randSource:  rand.Reader,
		asyncEvents: make(chan asyncEvent, 64),
		ctx:         context.Background(),
	}
}

// cabiRealloc calls the guest module's cabi_realloc to allocate memory.
// Signature: cabi_realloc(old_ptr, old_size, align, new_size) -> ptr
func cabiRealloc(ctx context.Context, mod api.Module, size uint32) (uint32, error) {
	realloc := mod.ExportedFunction("cabi_realloc")
	if realloc == nil {
		return 0, fmt.Errorf("cabi_realloc not exported")
	}
	results, err := realloc.Call(ctx, 0, 0, 1, uint64(size))
	if err != nil {
		return 0, err
	}
	return uint32(results[0]), nil
}

// writeListToMemory allocates memory for data via cabi_realloc, writes the data,
// and writes the list header (ptr, len) at retPtr.
func writeListToMemory(ctx context.Context, mod api.Module, retPtr uint32, data []byte) error {
	mem := mod.Memory()
	if mem == nil {
		return fmt.Errorf("no memory")
	}
	if len(data) == 0 {
		mem.WriteUint32Le(retPtr, 0)
		mem.WriteUint32Le(retPtr+4, 0)
		return nil
	}
	ptr, err := cabiRealloc(ctx, mod, uint32(len(data)))
	if err != nil {
		return err
	}
	mem.Write(ptr, data)
	mem.WriteUint32Le(retPtr, ptr)
	mem.WriteUint32Le(retPtr+4, uint32(len(data)))
	return nil
}

// writeStringListToMemory writes a list<string> to guest memory at retPtr.
func writeStringListToMemory(ctx context.Context, mod api.Module, retPtr uint32, strs []string) error {
	mem := mod.Memory()
	if mem == nil {
		return fmt.Errorf("no memory")
	}
	if len(strs) == 0 {
		mem.WriteUint32Le(retPtr, 0)
		mem.WriteUint32Le(retPtr+4, 0)
		return nil
	}

	// Allocate space for the list elements (each string is ptr+len = 8 bytes)
	listSize := uint32(len(strs)) * 8
	listPtr, err := cabiRealloc(ctx, mod, listSize)
	if err != nil {
		return err
	}

	// Write each string
	for i, s := range strs {
		strBytes := []byte(s)
		var strPtr uint32
		if len(strBytes) > 0 {
			strPtr, err = cabiRealloc(ctx, mod, uint32(len(strBytes)))
			if err != nil {
				return err
			}
			mem.Write(strPtr, strBytes)
		}
		offset := listPtr + uint32(i)*8
		mem.WriteUint32Le(offset, strPtr)
		mem.WriteUint32Le(offset+4, uint32(len(strBytes)))
	}

	mem.WriteUint32Le(retPtr, listPtr)
	mem.WriteUint32Le(retPtr+4, uint32(len(strs)))
	return nil
}

// writeStringPairListToMemory writes a list<tuple<string, string>> to guest memory.
func writeStringPairListToMemory(ctx context.Context, mod api.Module, retPtr uint32, pairs [][2]string) error {
	mem := mod.Memory()
	if mem == nil {
		return fmt.Errorf("no memory")
	}
	if len(pairs) == 0 {
		mem.WriteUint32Le(retPtr, 0)
		mem.WriteUint32Le(retPtr+4, 0)
		return nil
	}

	// Each tuple<string, string> = 4 fields * 4 bytes = 16 bytes
	listSize := uint32(len(pairs)) * 16
	listPtr, err := cabiRealloc(ctx, mod, listSize)
	if err != nil {
		return err
	}

	for i, pair := range pairs {
		offset := listPtr + uint32(i)*16
		for j := 0; j < 2; j++ {
			strBytes := []byte(pair[j])
			var strPtr uint32
			if len(strBytes) > 0 {
				strPtr, err = cabiRealloc(ctx, mod, uint32(len(strBytes)))
				if err != nil {
					return err
				}
				mem.Write(strPtr, strBytes)
			}
			mem.WriteUint32Le(offset+uint32(j)*8, strPtr)
			mem.WriteUint32Le(offset+uint32(j)*8+4, uint32(len(strBytes)))
		}
	}

	mem.WriteUint32Le(retPtr, listPtr)
	mem.WriteUint32Le(retPtr+4, uint32(len(pairs)))
	return nil
}

// RegisterAll registers all WASI host functions into the ComponentLinker.
func (h *ComponentHost) RegisterAll(cl *wazero.ComponentLinker) {
	h.registerRandom(cl)
	h.registerClocks(cl)
	h.registerCLI(cl)
	h.registerCLIp3(cl)
	h.registerIO(cl)
	h.registerFilesystem(cl)
	h.registerHTTP(cl)
	h.registerSockets(cl)
	h.registerBuiltins(cl)
	// Set the pre-callback hook to restore context before each callback invocation.
	cl.SetPreCallbackHook(h.contextRestore)
	// Set event source to deliver subtask events during the callback loop.
	cl.SetEventSource(h.pollEvent)
	// Set up the generic import handler for [async-lower], [stream-*], and [future-*] variants.
	cl.SetImportHandler(h.genericImportHandler)
}

// genericImportHandler handles unregistered imports across all WASI modules.
func (h *ComponentHost) genericImportHandler(moduleName, funcName string, paramTypes, resultTypes []api.ValueType) api.GoModuleFunction {
	// Handle unregistered imports across all WASI modules.
	// Handle stream/future plumbing for any WASI module.
	if strings.HasPrefix(funcName, "[stream-") || strings.HasPrefix(funcName, "[future-") {
		return h.streamFuturePlumbing(moduleName, funcName, paramTypes, resultTypes)
	}

	// Handle [async-lower] variants.
	if strings.HasPrefix(funcName, "[async-lower]") {
		inner := funcName[len("[async-lower]"):]

		// Delegate to filesystem-specific handler.
		if strings.Contains(moduleName, "filesystem") {
			return h.asyncLowerFS(inner, paramTypes, resultTypes)
		}

		// For [async-lower][stream-*] or [async-lower][future-*], use generic plumbing.
		if strings.HasPrefix(inner, "[stream-") || strings.HasPrefix(inner, "[future-") {
			return h.streamFuturePlumbing(moduleName, inner, paramTypes, resultTypes)
		}

		// Delegate to sockets-specific async-lower handler.
		if strings.Contains(moduleName, "sockets") {
			fn := h.asyncLowerSockets(inner, paramTypes, resultTypes)
			if fn != nil {
				return fn
			}
		}

		// Generic async-lower: synchronous completion returning RETURNED(2).
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			if len(resultTypes) > 0 {
				stack[0] = 2 // RETURNED
			}
		})
	}

	// Delegate to filesystem import handler for filesystem modules.
	if strings.Contains(moduleName, "filesystem") {
		return h.filesystemImportHandler(moduleName, funcName, paramTypes, resultTypes)
	}

	// Delegate to HTTP import handler for HTTP modules.
	if strings.Contains(moduleName, "http") {
		return h.httpImportHandler(moduleName, funcName, paramTypes, resultTypes)
	}

	// Delegate to sockets import handler for sockets modules.
	if strings.Contains(moduleName, "sockets") {
		return h.socketsImportHandler(moduleName, funcName, paramTypes, resultTypes)
	}

	return nil
}

// pollEvent returns the next event for the callback loop.
// It writes subtask results to guest memory before delivering the event.
func (h *ComponentHost) pollEvent(mod api.Module) (uint32, uint32, uint32) {
	// Check for subtask events first.
	idx, st := h.subtasks.PopReady()
	if st != nil {
		if st.resultsFn != nil {
			st.resultsFn(h.ctx, mod)
		} else if mem := mod.Memory(); mem != nil && st.results != nil {
			mem.Write(st.retPtr, st.results)
		}
		return 1, idx, 2 // SUBTASK event, subtask index, RETURNED state
	}

	// Non-blocking check for async events from background goroutines.
	select {
	case evt := <-h.asyncEvents:
		return evt.eventType, evt.p1, evt.p2
	default:
		// Yield to let background goroutines make progress.
		runtime.Gosched()
	}

	return 0, 0, 0 // NONE
}

// registerRandom registers wasi:random/* host functions.
func (h *ComponentHost) registerRandom(cl *wazero.ComponentLinker) {
	// wasi:random/random
	cl.DefineFunc("wasi:random/random@0.3.0-rc-2026-03-15", "get-random-u64",
		nil, []api.ValueType{i64},
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			var buf [8]byte
			_, _ = h.randSource.Read(buf[:])
			stack[0] = binary.LittleEndian.Uint64(buf[:])
		}))

	cl.DefineFunc("wasi:random/random@0.3.0-rc-2026-03-15", "get-random-bytes",
		[]api.ValueType{i64, i32}, nil,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			length := uint32(stack[0])
			retPtr := uint32(stack[1])
			buf := make([]byte, length)
			_, _ = h.randSource.Read(buf)
			_ = writeListToMemory(ctx, mod, retPtr, buf)
		}))

	// wasi:random/insecure@0.3.0-rc-2026-03-15
	cl.DefineFunc("wasi:random/insecure@0.3.0-rc-2026-03-15", "get-insecure-random-u64",
		nil, []api.ValueType{i64},
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			var buf [8]byte
			_, _ = h.randSource.Read(buf[:])
			stack[0] = binary.LittleEndian.Uint64(buf[:])
		}))

	cl.DefineFunc("wasi:random/insecure@0.3.0-rc-2026-03-15", "get-insecure-random-bytes",
		[]api.ValueType{i64, i32}, nil,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			length := uint32(stack[0])
			retPtr := uint32(stack[1])
			buf := make([]byte, length)
			_, _ = h.randSource.Read(buf)
			_ = writeListToMemory(ctx, mod, retPtr, buf)
		}))

	// wasi:random/insecure-seed@0.3.0-rc-2026-03-15
	cl.DefineFunc("wasi:random/insecure-seed@0.3.0-rc-2026-03-15", "get-insecure-seed",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[0])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			// Return two u64 values (seed1, seed2) at retPtr.
			// Must return the same seed on every call within the same instance.
			h.insecureSeedOnce.Do(func() {
				_, _ = h.randSource.Read(h.insecureSeed[:])
			})
			mem.Write(retPtr, h.insecureSeed[:])
		}))
}

// registerClocks registers wasi:clocks/* host functions.
func (h *ComponentHost) registerClocks(cl *wazero.ComponentLinker) {
	// wasi:clocks/monotonic-clock@0.3.0-rc-2026-03-15
	cl.DefineFunc("wasi:clocks/monotonic-clock@0.3.0-rc-2026-03-15", "now",
		nil, []api.ValueType{i64},
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = uint64(time.Now().UnixNano())
		}))

	cl.DefineFunc("wasi:clocks/monotonic-clock@0.3.0-rc-2026-03-15", "get-resolution",
		nil, []api.ValueType{i64},
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = 1 // 1ns resolution
		}))

	// [async-lower]wait-for: async version, returns packed subtask status.
	// Subtask.State: STARTING=0, STARTED=1, RETURNED=2
	// Return value: state | (subtask_index << 4). For sync completion: just RETURNED=2.
	cl.DefineFunc("wasi:clocks/monotonic-clock@0.3.0-rc-2026-03-15", "[async-lower]wait-for",
		[]api.ValueType{i64}, []api.ValueType{i32},
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			duration := stack[0]
			time.Sleep(time.Duration(duration))
			stack[0] = 2 // RETURNED (synchronous completion)
		}))

	// [async-lower]wait-until: async version
	cl.DefineFunc("wasi:clocks/monotonic-clock@0.3.0-rc-2026-03-15", "[async-lower]wait-until",
		[]api.ValueType{i64}, []api.ValueType{i32},
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			instant := stack[0]
			now := uint64(time.Now().UnixNano())
			if instant > now {
				time.Sleep(time.Duration(instant - now))
			}
			stack[0] = 2 // RETURNED (synchronous completion)
		}))

	// wasi:clocks/system-clock@0.3.0-rc-2026-03-15
	cl.DefineFunc("wasi:clocks/system-clock@0.3.0-rc-2026-03-15", "now",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[0])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			now := time.Now()
			// datetime: record { seconds: u64, nanoseconds: u32 }
			mem.WriteUint64Le(retPtr, uint64(now.Unix()))
			mem.WriteUint32Le(retPtr+8, uint32(now.Nanosecond()))
		}))

	cl.DefineFunc("wasi:clocks/system-clock@0.3.0-rc-2026-03-15", "get-resolution",
		nil, []api.ValueType{i64},
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = 1 // 1ns resolution
		}))
}

// registerCLI registers wasi:cli/* host functions (0.2.0 versions).
func (h *ComponentHost) registerCLI(cl *wazero.ComponentLinker) {
	// wasi:cli/environment@0.2.0
	cl.DefineFunc("wasi:cli/environment@0.2.0", "get-environment",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[0])
			_ = writeStringPairListToMemory(ctx, mod, retPtr, h.env)
		}))

	// wasi:cli/exit@0.2.0
	// exit terminates execution immediately, like proc_exit in WASI P1.
	cl.DefineFunc("wasi:cli/exit@0.2.0", "exit",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			// The parameter is a result<_, _> discriminant: 0=ok, 1=error
			resultDiscriminant := uint32(stack[0])
			var exitCode uint32
			if resultDiscriminant != 0 {
				exitCode = 1
			}
			h.exitCode.Store(int32(exitCode))
			h.exited.Store(true)
			_ = mod.CloseWithExitCode(ctx, exitCode)
			panic(sys.NewExitError(exitCode))
		}))

	// wasi:cli/stdin@0.2.0 - returns a stream resource handle
	cl.DefineFunc("wasi:cli/stdin@0.2.0", "get-stdin",
		nil, []api.ValueType{i32},
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			id := h.resources.New(&streamResource{reader: h.stdin})
			stack[0] = uint64(id)
		}))

	// wasi:cli/stdout@0.2.0
	cl.DefineFunc("wasi:cli/stdout@0.2.0", "get-stdout",
		nil, []api.ValueType{i32},
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			id := h.resources.New(&streamResource{writer: h.stdout})
			stack[0] = uint64(id)
		}))

	// wasi:cli/stderr@0.2.0
	cl.DefineFunc("wasi:cli/stderr@0.2.0", "get-stderr",
		nil, []api.ValueType{i32},
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			id := h.resources.New(&streamResource{writer: h.stderr})
			stack[0] = uint64(id)
		}))

	// wasi:cli/terminal-input@0.2.0
	cl.DefineFunc("wasi:cli/terminal-input@0.2.0", "[resource-drop]terminal-input",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			h.resources.Drop(uint32(stack[0]))
		}))

	// wasi:cli/terminal-output@0.2.0
	cl.DefineFunc("wasi:cli/terminal-output@0.2.0", "[resource-drop]terminal-output",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			h.resources.Drop(uint32(stack[0]))
		}))

	// wasi:cli/terminal-stdin@0.2.0
	cl.DefineFunc("wasi:cli/terminal-stdin@0.2.0", "get-terminal-stdin",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[0])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			// option<terminal-input>: discriminant=0 (none)
			mem.WriteByte(retPtr, 0)
		}))

	// wasi:cli/terminal-stdout@0.2.0
	cl.DefineFunc("wasi:cli/terminal-stdout@0.2.0", "get-terminal-stdout",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[0])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			mem.WriteByte(retPtr, 0)
		}))

	// wasi:cli/terminal-stderr@0.2.0
	cl.DefineFunc("wasi:cli/terminal-stderr@0.2.0", "get-terminal-stderr",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[0])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			mem.WriteByte(retPtr, 0)
		}))
}

// registerCLIp3 registers wasi:cli/* host functions (0.3.0-rc versions).
func (h *ComponentHost) registerCLIp3(cl *wazero.ComponentLinker) {
	// wasi:cli/environment@0.3.0-rc-2026-03-15
	cl.DefineFunc("wasi:cli/environment@0.3.0-rc-2026-03-15", "get-environment",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[0])
			_ = writeStringPairListToMemory(ctx, mod, retPtr, h.env)
		}))

	cl.DefineFunc("wasi:cli/environment@0.3.0-rc-2026-03-15", "get-arguments",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[0])
			_ = writeStringListToMemory(ctx, mod, retPtr, h.args)
		}))

	cl.DefineFunc("wasi:cli/environment@0.3.0-rc-2026-03-15", "get-initial-cwd",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[0])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			// option<string>: none (discriminant=0)
			mem.WriteByte(retPtr, 0)
		}))

	// wasi:cli/exit@0.3.0-rc-2026-03-15
	cl.DefineFunc("wasi:cli/exit@0.3.0-rc-2026-03-15", "exit",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			resultDiscriminant := uint32(stack[0])
			var exitCode uint32
			if resultDiscriminant != 0 {
				exitCode = 1
			}
			h.exitCode.Store(int32(exitCode))
			h.exited.Store(true)
			_ = mod.CloseWithExitCode(ctx, exitCode)
			panic(sys.NewExitError(exitCode))
		}))

	// wasi:cli/stdout@0.3.0-rc-2026-03-15 - write-via-stream
	// Returns tuple<stream<u8>, future<result<_, error-code>>>.
	// Writes stream handle at retPtr+0, future handle at retPtr+4.
	h.registerWriteViaStream(cl, "wasi:cli/stdout@0.3.0-rc-2026-03-15", h.stdout)

	// wasi:cli/stderr@0.3.0-rc-2026-03-15 - write-via-stream
	h.registerWriteViaStream(cl, "wasi:cli/stderr@0.3.0-rc-2026-03-15", h.stderr)

	// wasi:cli/stdin@0.3.0-rc-2026-03-15 - read-via-stream
	// Returns tuple<stream<u8>, future<result<_, error-code>>>.
	// Writes stream handle at retPtr+0, future handle at retPtr+4.
	cl.DefineFunc("wasi:cli/stdin@0.3.0-rc-2026-03-15", "read-via-stream",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[0])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			streamHandle := h.resources.New(&streamResource{reader: h.stdin})
			futureResult := make([]byte, 20) // result<_, error-code>: disc=0 (Ok)
			futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
			mem.WriteUint32Le(retPtr, streamHandle)
			mem.WriteUint32Le(retPtr+4, futureHandle)
		}))

	// wasi:cli/terminal-input@0.3.0-rc-2026-03-15
	cl.DefineFunc("wasi:cli/terminal-input@0.3.0-rc-2026-03-15", "[resource-drop]terminal-input",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			h.resources.Drop(uint32(stack[0]))
		}))

	// wasi:cli/terminal-output@0.3.0-rc-2026-03-15
	cl.DefineFunc("wasi:cli/terminal-output@0.3.0-rc-2026-03-15", "[resource-drop]terminal-output",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			h.resources.Drop(uint32(stack[0]))
		}))

	// wasi:cli/terminal-stdin@0.3.0-rc-2026-03-15
	cl.DefineFunc("wasi:cli/terminal-stdin@0.3.0-rc-2026-03-15", "get-terminal-stdin",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[0])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			mem.WriteByte(retPtr, 0) // none
		}))

	// wasi:cli/terminal-stdout@0.3.0-rc-2026-03-15
	cl.DefineFunc("wasi:cli/terminal-stdout@0.3.0-rc-2026-03-15", "get-terminal-stdout",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[0])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			mem.WriteByte(retPtr, 0) // none
		}))

	// wasi:cli/terminal-stderr@0.3.0-rc-2026-03-15
	cl.DefineFunc("wasi:cli/terminal-stderr@0.3.0-rc-2026-03-15", "get-terminal-stderr",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[0])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			mem.WriteByte(retPtr, 0) // none
		}))
}

// registerIO registers wasi:io/* host functions.
func (h *ComponentHost) registerIO(cl *wazero.ComponentLinker) {
	// wasi:io/error@0.2.0
	cl.DefineFunc("wasi:io/error@0.2.0", "[resource-drop]error",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			h.resources.Drop(uint32(stack[0]))
		}))

	// wasi:io/poll@0.2.0
	cl.DefineFunc("wasi:io/poll@0.2.0", "[resource-drop]pollable",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			h.resources.Drop(uint32(stack[0]))
		}))

	cl.DefineFunc("wasi:io/poll@0.2.0", "[method]pollable.block",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			// Block until ready - for now just return immediately.
		}))

	// wasi:io/streams@0.2.0
	cl.DefineFunc("wasi:io/streams@0.2.0", "[resource-drop]input-stream",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			h.resources.Drop(uint32(stack[0]))
		}))

	cl.DefineFunc("wasi:io/streams@0.2.0", "[resource-drop]output-stream",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			h.resources.Drop(uint32(stack[0]))
		}))

	cl.DefineFunc("wasi:io/streams@0.2.0", "[method]output-stream.check-write",
		[]api.ValueType{i32, i32}, nil,
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			// self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			// result<u64, stream-error>: ok discriminant=0, value=4096
			mem.WriteByte(retPtr, 0) // ok
			mem.WriteUint64Le(retPtr+8, 4096)
		}))

	cl.DefineFunc("wasi:io/streams@0.2.0", "[method]output-stream.write",
		[]api.ValueType{i32, i32, i32, i32}, nil,
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			selfHandle := uint32(stack[0])
			dataPtr := uint32(stack[1])
			dataLen := uint32(stack[2])
			retPtr := uint32(stack[3])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			data, ok := mem.Read(dataPtr, dataLen)
			if !ok {
				mem.WriteByte(retPtr, 1) // error
				return
			}

			res, found := h.resources.Get(selfHandle)
			if found {
				if sr, ok := res.(*streamResource); ok && sr.writer != nil {
					_, _ = sr.writer.Write(data)
				}
			}

			mem.WriteByte(retPtr, 0) // ok
		}))

	cl.DefineFunc("wasi:io/streams@0.2.0", "[method]output-stream.blocking-flush",
		[]api.ValueType{i32, i32}, nil,
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			// self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			// result<_, stream-error>: ok
			mem.WriteByte(retPtr, 0)
		}))

	cl.DefineFunc("wasi:io/streams@0.2.0", "[method]output-stream.subscribe",
		[]api.ValueType{i32}, []api.ValueType{i32},
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			// Return a pollable handle.
			id := h.resources.New(&pollableResource{ready: true})
			stack[0] = uint64(id)
		}))
}

// registerBuiltins registers the component model builtin functions ($root, [export]...).
func (h *ComponentHost) registerBuiltins(cl *wazero.ComponentLinker) {
	// $root module - component model async builtins
	// Context slots are per-task storage used by the async runtime.
	// contextSlot0 holds the current value of context slot 0.
	// savedContext0 holds the last non-zero value, used to restore context
	// before callback invocations (the guest clears it between callbacks).
	var contextSlot0 atomic.Int64
	h.savedContext0.Store(0)

	cl.DefineFunc("$root", "[context-get-0]",
		nil, []api.ValueType{i32},
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = uint64(contextSlot0.Load())
		}))

	cl.DefineFunc("$root", "[context-set-0]",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			val := int64(stack[0])
			contextSlot0.Store(val)
			if val != 0 {
				h.savedContext0.Store(val)
			}
		}))

	// contextRestore is used by the async entry loop to restore context before callbacks.
	// Also used to verify memory hasn't been clobbered.
	h.contextRestore = func() {
		saved := h.savedContext0.Load()
		if saved != 0 {
			contextSlot0.Store(saved)
		}
	}

	cl.DefineFunc("$root", "[waitable-join]",
		[]api.ValueType{i32, i32}, nil,
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			// No-op: waitable registration is handled internally by the wasm runtime.
		}))

	cl.DefineFunc("$root", "[waitable-set-new]",
		nil, []api.ValueType{i32},
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = 1 // Return a handle.
		}))

	cl.DefineFunc("$root", "[waitable-set-poll]",
		[]api.ValueType{i32, i32}, []api.ValueType{i32},
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			// Non-blocking poll: return 0 events.
			// Events are delivered through the callback loop (pollEvent).
			stack[0] = 0
		}))

	cl.DefineFunc("$root", "[waitable-set-drop]",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			// No-op.
		}))

	cl.DefineFunc("$root", "[subtask-cancel]",
		[]api.ValueType{i32}, []api.ValueType{i32},
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = 0
		}))

	cl.DefineFunc("$root", "[subtask-drop]",
		[]api.ValueType{i32}, nil,
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			h.subtasks.Drop(uint32(stack[0]))
		}))

	// [export]$root
	cl.DefineFunc("[export]$root", "[task-cancel]",
		nil, nil,
		api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			// No-op.
		}))

	// [export]wasi:cli/run - register task-return for known versions.
	taskReturnFn := api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
		h.taskReturnValue.Store(int32(stack[0]))
	})
	for _, ver := range []string{"0.3.0-rc-2026-03-15", "0.3.0-rc-2026-03-15"} {
		cl.DefineFunc("[export]wasi:cli/run@"+ver, "[task-return]run",
			[]api.ValueType{i32}, nil, taskReturnFn)
	}
}

// registerWriteViaStream registers write-via-stream for a P3 CLI output stream.
// write-via-stream(data: stream<u8>) -> future<result<_, error-code>>
// Lowered: (i32) -> (i32) where param = stream readable handle, return = future readable handle.
func (h *ComponentHost) registerWriteViaStream(cl *wazero.ComponentLinker, moduleName string, writer io.Writer) {
	cl.DefineFunc(moduleName, "write-via-stream",
		[]api.ValueType{i32}, []api.ValueType{i32},
		api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			streamHandle := uint32(stack[0])
			// Connect the stream to the actual writer (stdout/stderr).
			// The stream handle is the readable end from [stream-new-0].
			// Both readable and writable handles share the same streamResource,
			// so setting writer here makes [stream-write-0] on the writable end
			// write directly to the output.
			if res, ok := h.resources.Get(streamHandle); ok {
				if sr, ok := res.(*streamResource); ok {
					sr.writer = writer
				}
			}
			// Return 0 as the future handle. The guest treats this as the readable
			// end of the future for the result.
			stack[0] = 0
		}))
}

// ExitCode returns the exit code if exit() was called, or -1 if not.
func (h *ComponentHost) ExitCode() (int32, bool) {
	if h.exited.Load() {
		return h.exitCode.Load(), true
	}
	return -1, false
}

// TaskReturnValue returns the value passed to task-return.
func (h *ComponentHost) TaskReturnValue() int32 {
	return h.taskReturnValue.Load()
}

// MustInstantiateComponent is a convenience that creates a ComponentLinker with all WASI host functions,
// instantiates the component, and calls _start (or the async run entry point).
func MustInstantiateComponent(ctx context.Context, rt wazero.Runtime, componentBinary []byte, config wazero.ModuleConfig) (api.Module, error) {
	cl := wazero.NewComponentLinker()
	host := NewComponentHost(nil, nil, nil, nil, nil)
	host.RegisterAll(cl)

	return cl.InstantiateComponent(ctx, rt, componentBinary, config)
}

// InstantiateComponentWithHost creates a ComponentLinker with the given host,
// instantiates the component, and returns both the module and host.
func InstantiateComponentWithHost(ctx context.Context, rt wazero.Runtime, componentBinary []byte, config wazero.ModuleConfig, host *ComponentHost) (api.Module, error) {
	cl := wazero.NewComponentLinker()
	host.RegisterAll(cl)
	mod, err := cl.InstantiateComponent(ctx, rt, componentBinary, config)
	if err != nil {
		return nil, err
	}

	// Check if exit() was called during execution.
	if exitCode, exited := host.ExitCode(); exited && exitCode != 0 {
		return mod, sys.NewExitError(uint32(exitCode))
	}

	return mod, nil
}

// Ensure we don't use fmt for anything but errors.
var _ = fmt.Errorf
