package runtime

import "sync"

// ResourceTable manages component model resources, async context, and waitable sets.
type ResourceTable struct {
	mu sync.Mutex

	// resources maps handle -> (typeIndex, representation)
	resources map[int32]*resourceEntry
	nextHandle int32

	// context slots for async context.get/set
	contextSlots [8]int32

	// waitableSet counter
	nextWaitableSet int32

	// stream/future counter
	nextStream int32

	// error context counter
	nextErrorCtx int32

	// taskReturn stores the last task.return values.
	taskReturnValues []uint64
}

type resourceEntry struct {
	typeIdx uint32
	rep     int32
}

// NewResourceTable creates a new resource table.
func NewResourceTable() *ResourceTable {
	return &ResourceTable{
		resources:  make(map[int32]*resourceEntry),
		nextHandle: 1, // 0 is reserved
	}
}

// New creates a new resource handle with the given type and representation.
func (rt *ResourceTable) New(typeIdx uint32, rep int32) int32 {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	handle := rt.nextHandle
	rt.nextHandle++
	rt.resources[handle] = &resourceEntry{typeIdx: typeIdx, rep: rep}
	return handle
}

// Drop removes a resource handle.
func (rt *ResourceTable) Drop(handle int32) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.resources, handle)
}

// Rep returns the representation of a resource handle.
func (rt *ResourceTable) Rep(handle int32) int32 {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if entry, ok := rt.resources[handle]; ok {
		return entry.rep
	}
	return 0
}

// ContextGet returns the value in context slot i.
func (rt *ResourceTable) ContextGet(i uint32) int32 {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if int(i) < len(rt.contextSlots) {
		return rt.contextSlots[i]
	}
	return 0
}

// ContextSet sets the value in context slot i.
func (rt *ResourceTable) ContextSet(i uint32, val int32) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if int(i) < len(rt.contextSlots) {
		rt.contextSlots[i] = val
	}
}

// WaitableSetNew creates a new waitable set and returns its ID.
func (rt *ResourceTable) WaitableSetNew() int32 {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.nextWaitableSet++
	return rt.nextWaitableSet
}

// StreamNew creates a new stream/future and returns its ID.
func (rt *ResourceTable) StreamNew() int32 {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.nextStream++
	return rt.nextStream
}

// ErrorContextNew creates a new error context.
func (rt *ResourceTable) ErrorContextNew() int32 {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.nextErrorCtx++
	return rt.nextErrorCtx
}

// SetTaskReturn stores the task return values.
func (rt *ResourceTable) SetTaskReturn(stack []uint64) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.taskReturnValues = make([]uint64, len(stack))
	copy(rt.taskReturnValues, stack)
}

// GetTaskReturn returns the stored task return values.
func (rt *ResourceTable) GetTaskReturn() []uint64 {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.taskReturnValues
}
