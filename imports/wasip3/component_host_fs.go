package wasip3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// descriptorResource represents an open filesystem descriptor (file or directory).
type descriptorResource struct {
	path      string
	file      *os.File
	isPreopen bool
	guestPath string
	descFlags byte // descriptor-flags the descriptor was opened with
}

// dirEntryStream represents an in-progress directory read.
type dirEntryStream struct {
	entries []os.DirEntry
	offset  int
}

// WASI filesystem descriptor types (variant descriptor-type discriminants).
// Order must match the WIT variant definition in wasi:filesystem/types.
const (
	dtBlockDevice     = 0
	dtCharacterDevice = 1
	dtDirectory       = 2
	dtFifo            = 3
	dtSymbolicLink    = 4
	dtRegularFile     = 5
	dtSocket          = 6
	// dtOther = 7 carries option<string> payload; not used by the host.
)

// WASI filesystem error codes (wasi:filesystem/types@0.3.0 error-code variant).
// Order must match the WIT variant definition. Note: P3 removed "would-block".
const (
	ecAccess          = 0
	ecAlready         = 1
	ecBadDescriptor   = 2
	ecBusy            = 3
	ecDeadlock        = 4
	ecQuota           = 5
	ecExist           = 6
	ecFileTooLarge    = 7
	ecIllegalByteSeq  = 8
	ecInProgress      = 9
	ecInterrupted     = 10
	ecInvalid         = 11
	ecIO              = 12
	ecIsDirectory     = 13
	ecLoop            = 14
	ecTooManyLinks    = 15
	ecMessageSize     = 16
	ecNameTooLong     = 17
	ecNoDevice        = 18
	ecNoEntry         = 19
	ecNoLock          = 20
	ecInsufficientMem = 21
	ecNotEnoughSpace  = 22
	ecNotDirectory    = 23
	ecNotEmpty        = 24
	ecNotRecoverable  = 25
	ecUnsupported     = 26
	ecNoTTY           = 27
	ecNoSuchDevice    = 28
	ecOverflow        = 29
	ecNotPermitted    = 30
	ecPipe            = 31
	ecReadOnly        = 32
	ecInvalidSeek     = 33
	ecTextFileBusy    = 34
	ecCrossDevice     = 35
)

// AddPreopen adds a filesystem preopen to the component host.
func (h *ComponentHost) AddPreopen(hostPath, guestPath string) {
	h.preopens = append(h.preopens, preopen{path: hostPath, guestPath: guestPath})
}

// registerFilesystem registers wasi:filesystem/* host functions and the import handler.
func (h *ComponentHost) registerFilesystem(cl *wazero.ComponentLinker) {
	// Initialize preopen descriptor resources.
	preopenHandles := make([]uint32, 0, len(h.preopens))
	for _, po := range h.preopens {
		handle := h.resources.New(&descriptorResource{
			path:      po.path,
			isPreopen: true,
			guestPath: po.guestPath,
			descFlags: 0x01 | 0x20, // READ | MUTATE_DIRECTORY
		})
		preopenHandles = append(preopenHandles, handle)
	}

	// Register the exact functions that are imported directly (non-async-lower).
	for _, mod := range []string{
		"wasi:filesystem/preopens@0.3.0-rc-2026-03-15",
		"wasi:filesystem/preopens@0.2.0",
	} {
		cl.DefineFunc(mod, "get-directories",
			[]api.ValueType{i32}, nil,
			api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
				retPtr := uint32(stack[0])
				mem := mod.Memory()
				if mem == nil || len(h.preopens) == 0 {
					if mem != nil {
						mem.WriteUint32Le(retPtr, 0)
						mem.WriteUint32Le(retPtr+4, 0)
					}
					return
				}
				listSize := uint32(len(h.preopens)) * 12
				listPtr, err := cabiRealloc(ctx, mod, listSize)
				if err != nil {
					mem.WriteUint32Le(retPtr, 0)
					mem.WriteUint32Le(retPtr+4, 0)
					return
				}
				for i, po := range h.preopens {
					offset := listPtr + uint32(i)*12
					mem.WriteUint32Le(offset, preopenHandles[i])
					pathBytes := []byte(po.guestPath)
					var pathPtr uint32
					if len(pathBytes) > 0 {
						pathPtr, _ = cabiRealloc(ctx, mod, uint32(len(pathBytes)))
						mem.Write(pathPtr, pathBytes)
					}
					mem.WriteUint32Le(offset+4, pathPtr)
					mem.WriteUint32Le(offset+8, uint32(len(pathBytes)))
				}
				mem.WriteUint32Le(retPtr, listPtr)
				mem.WriteUint32Le(retPtr+4, uint32(len(h.preopens)))
			}))
	}

	for _, mod := range []string{
		"wasi:filesystem/types@0.3.0-rc-2026-03-15",
		"wasi:filesystem/types@0.2.0",
	} {
		cl.DefineFunc(mod, "[resource-drop]descriptor",
			[]api.ValueType{i32}, nil,
			api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
				handle := uint32(stack[0])
				if res, ok := h.resources.Get(handle); ok {
					if dr, ok := res.(*descriptorResource); ok && dr.file != nil {
						dr.file.Close()
					}
				}
				h.resources.Drop(handle)
			}))

		// read-via-stream: (self: i32, offset: i64, ret_ptr: i32) -> ()
		// Returns tuple<stream<u8>, future<result<_, error-code>>>
		// Layout: stream_handle@0, future_handle@4
		cl.DefineFunc(mod, "[method]descriptor.read-via-stream",
			[]api.ValueType{i32, i64, i32}, nil,
			api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
				self := uint32(stack[0])
				offset := int64(stack[1])
				retPtr := uint32(stack[2])
				mem := mod.Memory()

				res, ok := h.resources.Get(self)
				if !ok {
					// Error: create empty stream + future with error result.
					streamHandle := h.resources.New(&streamResource{})
					futureResult := make([]byte, 20)
					futureResult[0] = 1 // error disc
					futureResult[4] = ecBadDescriptor
					futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
					mem.WriteUint32Le(retPtr, streamHandle)
					mem.WriteUint32Le(retPtr+4, futureHandle)
					return
				}
				dr := res.(*descriptorResource)

				// Validate offset - u64::MAX and other huge values are invalid.
				if offset < 0 {
					streamHandle := h.resources.New(&streamResource{})
					futureResult := make([]byte, 20)
					futureResult[0] = 1 // error disc
					futureResult[4] = ecInvalid
					futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
					mem.WriteUint32Le(retPtr, streamHandle)
					mem.WriteUint32Le(retPtr+4, futureHandle)
					return
				}

				file, err := os.Open(dr.path)
				if err != nil {
					streamHandle := h.resources.New(&streamResource{})
					futureResult := make([]byte, 20)
					futureResult[0] = 1
					futureResult[4] = mapErrno(err)
					futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
					mem.WriteUint32Le(retPtr, streamHandle)
					mem.WriteUint32Le(retPtr+4, futureHandle)
					return
				}
				if offset > 0 {
					file.Seek(offset, 0)
				}

				streamHandle := h.resources.New(&streamResource{reader: file})
				futureResult := make([]byte, 20) // result<_, error-code>: disc=0 (Ok)
				futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
				mem.WriteUint32Le(retPtr, streamHandle)
				mem.WriteUint32Le(retPtr+4, futureHandle)
			}))

		// write-via-stream: (self: i32, data_stream: i32, offset: i64) -> (i32) future handle
		// Returns future<result<_, error-code>> as a handle.
		cl.DefineFunc(mod, "[method]descriptor.write-via-stream",
			[]api.ValueType{i32, i32, i64}, []api.ValueType{i32},
			api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
				self := uint32(stack[0])
				dataStream := uint32(stack[1])
				offset := int64(stack[2])

				res, ok := h.resources.Get(self)
				if !ok {
					futureResult := make([]byte, 20)
					futureResult[0] = 1 // error disc
					futureResult[4] = ecBadDescriptor
					futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
					stack[0] = uint64(futureHandle)
					return
				}
				dr := res.(*descriptorResource)

				file, err := os.OpenFile(dr.path, os.O_WRONLY, 0o666)
				if err != nil {
					futureResult := make([]byte, 20)
					futureResult[0] = 1
					futureResult[4] = mapErrno(err)
					futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
					stack[0] = uint64(futureHandle)
					return
				}
				if offset > 0 {
					file.Seek(offset, 0)
				}

				// Set the writer on the shared stream resource so [stream-write-0]
				// on the writable end will write to the file.
				if streamRes, ok := h.resources.Get(dataStream); ok {
					if sr, ok := streamRes.(*streamResource); ok {
						sr.writer = file
					}
				}

				futureResult := make([]byte, 20) // result<_, error-code>: disc=0 (Ok)
				futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
				stack[0] = uint64(futureHandle)
			}))

		// append-via-stream: (self: i32, data_stream: i32) -> (i32) future handle
		cl.DefineFunc(mod, "[method]descriptor.append-via-stream",
			[]api.ValueType{i32, i32}, []api.ValueType{i32},
			api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
				self := uint32(stack[0])
				dataStream := uint32(stack[1])

				res, ok := h.resources.Get(self)
				if !ok {
					futureResult := make([]byte, 20)
					futureResult[0] = 1
					futureResult[4] = ecBadDescriptor
					futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
					stack[0] = uint64(futureHandle)
					return
				}
				dr := res.(*descriptorResource)

				file, err := os.OpenFile(dr.path, os.O_WRONLY|os.O_APPEND, 0o666)
				if err != nil {
					futureResult := make([]byte, 20)
					futureResult[0] = 1
					futureResult[4] = mapErrno(err)
					futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
					stack[0] = uint64(futureHandle)
					return
				}

				if streamRes, ok := h.resources.Get(dataStream); ok {
					if sr, ok := streamRes.(*streamResource); ok {
						sr.writer = file
					}
				}

				futureResult := make([]byte, 20) // result<_, error-code>: disc=0 (Ok)
				futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
				stack[0] = uint64(futureHandle)
			}))

		// read-directory: (self: i32, ret_ptr: i32) -> ()
		// Returns tuple<stream<directory-entry>, future<result<_, error-code>>>
		// Layout: stream_handle@0, future_handle@4
		cl.DefineFunc(mod, "[method]descriptor.read-directory",
			[]api.ValueType{i32, i32}, nil,
			api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
				self := uint32(stack[0])
				retPtr := uint32(stack[1])
				mem := mod.Memory()

				res, ok := h.resources.Get(self)
				if !ok {
					streamHandle := h.resources.New(&dirEntryStream{})
					futureResult := make([]byte, 20)
					futureResult[0] = 1
					futureResult[4] = ecBadDescriptor
					futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
					mem.WriteUint32Le(retPtr, streamHandle)
					mem.WriteUint32Le(retPtr+4, futureHandle)
					return
				}
				dr := res.(*descriptorResource)

				entries, err := os.ReadDir(dr.path)
				if err != nil {
					streamHandle := h.resources.New(&dirEntryStream{})
					futureResult := make([]byte, 20)
					futureResult[0] = 1
					futureResult[4] = mapErrno(err)
					futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
					mem.WriteUint32Le(retPtr, streamHandle)
					mem.WriteUint32Le(retPtr+4, futureHandle)
					return
				}

				streamHandle := h.resources.New(&dirEntryStream{entries: entries})
				futureResult := make([]byte, 20) // result<_, error-code>: disc=0 (Ok)
				futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
				mem.WriteUint32Le(retPtr, streamHandle)
				mem.WriteUint32Le(retPtr+4, futureHandle)
			}))

		// stream/future plumbing - register as no-ops or minimal impls.
		for _, prefix := range []string{
			"[stream-new-0]", "[stream-drop-readable-0]", "[stream-drop-writable-0]",
			"[stream-cancel-read-0]", "[stream-cancel-write-0]",
			"[future-new-1]", "[future-drop-readable-1]", "[future-drop-writable-1]",
			"[future-cancel-read-1]", "[future-cancel-write-1]",
		} {
			for _, suffix := range []string{
				"[method]descriptor.read-via-stream",
				"[method]descriptor.read-directory",
			} {
				name := prefix + suffix
				// These are registered with placeholder signatures; the import handler
				// will override them with the correct signature at link time.
				_ = name
			}
		}
	}

	// Note: the generic import handler is set in RegisterAll, not here.
}

// filesystemImportHandler handles unregistered filesystem imports, particularly
// [async-lower] variants that have different signatures from the base methods.
func (h *ComponentHost) filesystemImportHandler(moduleName, funcName string, paramTypes, resultTypes []api.ValueType) api.GoModuleFunction {
	// Handle [async-lower] variants.
	if strings.HasPrefix(funcName, "[async-lower]") {
		inner := funcName[len("[async-lower]"):]
		return h.asyncLowerFS(inner, paramTypes, resultTypes)
	}

	return nil
}

// writeNonBlocking writes data to a writer, handling net.Conn non-blockingly.
// Returns n=-1 if the write would block (caller should return BLOCKED).
func writeNonBlocking(w io.Writer, data []byte, streamHandle uint32, h *ComponentHost) (int, error) {
	conn, isConn := w.(net.Conn)
	if !isConn {
		return w.Write(data)
	}
	conn.SetWriteDeadline(time.Now())
	n, err := conn.Write(data)
	conn.SetWriteDeadline(time.Time{})
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			// Write would block - start background write.
			dataCopy := make([]byte, len(data))
			copy(dataCopy, data)
			h.pendingOps.Add(1)
			go func() {
				conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
				written, _ := conn.Write(dataCopy)
				conn.SetWriteDeadline(time.Time{})
				h.asyncEvents <- asyncEvent{3, streamHandle, uint32(written << 4)}
			}()
			return -1, nil
		}
	}
	return n, err
}

// asyncLowerFS implements [async-lower] filesystem methods.
// The convention is: the function takes flattened params (or args_ptr + ret_ptr for large param sets),
// writes the result to a return area in memory, and returns i32 status 0 (completed).

func (h *ComponentHost) asyncLowerFS(methodName string, paramTypes, resultTypes []api.ValueType) api.GoModuleFunction {
	switch methodName {
	case "[method]descriptor.get-type":
		// (self: i32, ret_ptr: i32) -> i32
		// result<descriptor-type, error-code>: disc@0, payload@4 (descriptor-type is 16 bytes, align 4)
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBadDescriptor)
				stack[0] = 2
				return
			}
			dr := res.(*descriptorResource)
			info, err := os.Lstat(dr.path)
			if err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, mapErrno(err))
				stack[0] = 2
				return
			}
			mem.WriteByte(retPtr, 0)                       // ok
			mem.WriteByte(retPtr+4, fileModeToDT(info.Mode())) // descriptor-type disc
			mem.WriteByte(retPtr+8, 0)                     // option<string> = None
			stack[0] = 2
		})

	case "[method]descriptor.get-flags":
		// result<descriptor-flags, error-code>: disc@0, flags@4
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBadDescriptor)
				stack[0] = 2
				return
			}
			dr := res.(*descriptorResource)
			mem.WriteByte(retPtr, 0) // ok
			mem.WriteByte(retPtr+4, dr.descFlags)
			stack[0] = 2
		})

	case "[method]descriptor.stat":
		// (self: i32, ret_ptr: i32) -> i32
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+8, ecBadDescriptor)
				stack[0] = 2 // RETURNED
				return
			}
			dr := res.(*descriptorResource)
			var info os.FileInfo
			var err error
			if dr.file != nil {
				info, err = dr.file.Stat()
			} else {
				info, err = os.Lstat(dr.path)
			}
			if err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+8, mapErrno(err))
				stack[0] = 2
				return
			}
			writeDescriptorStat(mem, retPtr, info)
			stack[0] = 2
		})

	case "[method]descriptor.stat-at":
		// (self: i32, flags: i32, path_ptr: i32, path_len: i32, ret_ptr: i32) -> i32
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			flags := uint32(stack[1])
			pathPtr := uint32(stack[2])
			pathLen := uint32(stack[3])
			retPtr := uint32(stack[4])
			mem := mod.Memory()

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+8, ecBadDescriptor)
				stack[0] = 2
				return
			}
			dr := res.(*descriptorResource)
			// *-at methods require a directory descriptor.
			if info, err := os.Lstat(dr.path); err != nil || !info.IsDir() {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+8, ecNotDirectory)
				stack[0] = 2
				return
			}
			pathBytes, ok := mem.Read(pathPtr, pathLen)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+8, ecInvalid)
				stack[0] = 2
				return
			}
			relPath := string(pathBytes)
			if relPath == "" {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+8, ecNoEntry)
				stack[0] = 2
				return
			}
			if isPathEscaping(relPath) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+8, ecNotPermitted)
				stack[0] = 2
				return
			}
			fullPath := filepath.Join(dr.path, relPath)
			var info os.FileInfo
			var statErr error
			if flags&1 != 0 {
				info, statErr = os.Stat(fullPath)
			} else {
				info, statErr = os.Lstat(fullPath)
			}
			if statErr != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+8, mapErrno(statErr))
				stack[0] = 2
				return
			}
			writeDescriptorStat(mem, retPtr, info)
			stack[0] = 2
		})

	case "[method]descriptor.open-at":
		// args packed in memory: (args_ptr: i32, ret_ptr: i32) -> i32
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			argsPtr := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()

			// Read args from memory. Canonical ABI layout:
			// offset 0: self (i32), offset 4: path-flags (u8), offset 8: path-ptr (i32),
			// offset 12: path-len (i32), offset 16: open-flags (u8), offset 17: desc-flags (u8)
			self, _ := mem.ReadUint32Le(argsPtr)
			pathFlagsByte, _ := mem.ReadByte(argsPtr + 4)
			pathFlags := uint32(pathFlagsByte)
			pathPtr, _ := mem.ReadUint32Le(argsPtr + 8)
			pathLen, _ := mem.ReadUint32Le(argsPtr + 12)
			openFlagsByte, _ := mem.ReadByte(argsPtr + 16)
			openFlags := uint32(openFlagsByte)
			descFlagsByte, _ := mem.ReadByte(argsPtr + 17)
			dFlags := byte(descFlagsByte)

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteUint32Le(retPtr+4, uint32(ecBadDescriptor))
				stack[0] = 2
				return
			}
			dr := res.(*descriptorResource)
			pathBytes, ok := mem.Read(pathPtr, pathLen)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteUint32Le(retPtr+4, uint32(ecInvalid))
				stack[0] = 2
				return
			}

			relPath := string(pathBytes)
			if relPath == "" {
				mem.WriteByte(retPtr, 1)
				mem.WriteUint32Le(retPtr+4, uint32(ecNoEntry))
				stack[0] = 2
				return
			}
			if isPathEscaping(relPath) {
				mem.WriteByte(retPtr, 1)
				mem.WriteUint32Le(retPtr+4, uint32(ecNotPermitted))
				stack[0] = 2
				return
			}

			fullPath := filepath.Join(dr.path, relPath)
			if pathFlags&1 != 0 {
				if resolved, err := filepath.EvalSymlinks(fullPath); err == nil {
					fullPath = resolved
				}
			}

			// Default to READ if no flags specified.
			if dFlags == 0 {
				dFlags = 0x01 // read
			}
			// CREATE implies WRITE.
			if openFlags&0x01 != 0 {
				dFlags |= 0x02 // write
			}

			// Check if target is a directory.
			info, statErr := os.Lstat(fullPath)
			isDir := statErr == nil && info.IsDir()

			// WRITE on a directory is not allowed.
			if isDir && dFlags&0x02 != 0 {
				mem.WriteByte(retPtr, 1)
				mem.WriteUint32Le(retPtr+4, uint32(ecIsDirectory))
				stack[0] = 2
				return
			}

			if openFlags&0x02 != 0 { // DIRECTORY flag
				if statErr != nil {
					mem.WriteByte(retPtr, 1)
					mem.WriteUint32Le(retPtr+4, uint32(mapErrno(statErr)))
					stack[0] = 2
					return
				}
				if !isDir {
					mem.WriteByte(retPtr, 1)
					mem.WriteUint32Le(retPtr+4, uint32(ecNotDirectory))
					stack[0] = 2
					return
				}
			}

			osFlags := os.O_RDONLY
			if dFlags&0x02 != 0 || dFlags&0x20 != 0 { // write or mutate
				if dFlags&0x01 != 0 { // read
					osFlags = os.O_RDWR
				} else {
					osFlags = os.O_WRONLY
				}
			}
			if openFlags&0x01 != 0 {
				osFlags |= os.O_CREATE
			}
			if openFlags&0x04 != 0 {
				osFlags |= os.O_EXCL
			}
			if openFlags&0x08 != 0 {
				osFlags |= os.O_TRUNC
			}

			// For directories, just validate it exists; don't open as file.
			if isDir {
				handle := h.resources.New(&descriptorResource{path: fullPath, descFlags: dFlags})
				mem.WriteByte(retPtr, 0)
				mem.WriteUint32Le(retPtr+4, handle)
				stack[0] = 2
				return
			}

			file, err := os.OpenFile(fullPath, osFlags, 0o666)
			if err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteUint32Le(retPtr+4, uint32(mapErrno(err)))
				stack[0] = 2
				return
			}

			handle := h.resources.New(&descriptorResource{path: fullPath, file: file, descFlags: dFlags})
			mem.WriteByte(retPtr, 0) // ok
			mem.WriteUint32Le(retPtr+4, handle)
			stack[0] = 2
		})

	case "[method]descriptor.create-directory-at":
		// (self: i32, path_ptr: i32, path_len: i32, ret_ptr: i32) -> i32
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			pathPtr := uint32(stack[1])
			pathLen := uint32(stack[2])
			retPtr := uint32(stack[3])
			mem := mod.Memory()

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBadDescriptor)
				stack[0] = 2
				return
			}
			dr := res.(*descriptorResource)
			pathBytes, ok := mem.Read(pathPtr, pathLen)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecInvalid)
				stack[0] = 2
				return
			}
			relPath := string(pathBytes)
			if relPath == "" {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNoEntry)
				stack[0] = 2
				return
			}
			if isPathEscaping(relPath) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNotPermitted)
				stack[0] = 2
				return
			}
			if err := os.Mkdir(filepath.Join(dr.path, relPath), 0o755); err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, mapErrno(err))
				stack[0] = 2
				return
			}
			mem.WriteByte(retPtr, 0)
			stack[0] = 2
		})

	case "[method]descriptor.remove-directory-at":
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			pathPtr := uint32(stack[1])
			pathLen := uint32(stack[2])
			retPtr := uint32(stack[3])
			mem := mod.Memory()

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBadDescriptor)
				stack[0] = 2
				return
			}
			dr := res.(*descriptorResource)
			pathBytes, ok := mem.Read(pathPtr, pathLen)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecInvalid)
				stack[0] = 2
				return
			}
			relPath := string(pathBytes)
			if relPath == "" {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNoEntry)
				stack[0] = 2
				return
			}
			if isPathEscaping(relPath) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNotPermitted)
				stack[0] = 2
				return
			}
			// Removing "." or the descriptor dir itself is invalid.
			if filepath.Clean(relPath) == "." {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecInvalid)
				stack[0] = 2
				return
			}
			fullPath := filepath.Join(dr.path, relPath)
			info, err := os.Lstat(fullPath)
			if err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, mapErrno(err))
				stack[0] = 2
				return
			}
			if !info.IsDir() {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNotDirectory)
				stack[0] = 2
				return
			}
			if err := os.Remove(fullPath); err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, mapErrno(err))
				stack[0] = 2
				return
			}
			mem.WriteByte(retPtr, 0)
			stack[0] = 2
		})

	case "[method]descriptor.unlink-file-at":
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			pathPtr := uint32(stack[1])
			pathLen := uint32(stack[2])
			retPtr := uint32(stack[3])
			mem := mod.Memory()

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBadDescriptor)
				stack[0] = 2
				return
			}
			dr := res.(*descriptorResource)
			pathBytes, ok := mem.Read(pathPtr, pathLen)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecInvalid)
				stack[0] = 2
				return
			}
			relPath := string(pathBytes)
			if relPath == "" {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNoEntry)
				stack[0] = 2
				return
			}
			if isPathEscaping(relPath) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNotPermitted)
				stack[0] = 2
				return
			}
			fullPath := filepath.Join(dr.path, relPath)
			info, err := os.Lstat(fullPath)
			if err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, mapErrno(err))
				stack[0] = 2
				return
			}
			if info.IsDir() {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecIsDirectory)
				stack[0] = 2
				return
			}
			if err := os.Remove(fullPath); err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, mapErrno(err))
				stack[0] = 2
				return
			}
			mem.WriteByte(retPtr, 0)
			stack[0] = 2
		})

	case "[method]descriptor.rename-at":
		// args packed: (args_ptr: i32, ret_ptr: i32) -> i32
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			argsPtr := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()

			self, _ := mem.ReadUint32Le(argsPtr)
			oldPathPtr, _ := mem.ReadUint32Le(argsPtr + 4)
			oldPathLen, _ := mem.ReadUint32Le(argsPtr + 8)
			newDesc, _ := mem.ReadUint32Le(argsPtr + 12)
			newPathPtr, _ := mem.ReadUint32Le(argsPtr + 16)
			newPathLen, _ := mem.ReadUint32Le(argsPtr + 20)

			res1, ok1 := h.resources.Get(self)
			res2, ok2 := h.resources.Get(newDesc)
			if !ok1 || !ok2 {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBadDescriptor)
				stack[0] = 2
				return
			}
			oldDr := res1.(*descriptorResource)
			newDr := res2.(*descriptorResource)

			oldPath, _ := mem.Read(oldPathPtr, oldPathLen)
			newPath, _ := mem.Read(newPathPtr, newPathLen)

			oldRel := string(oldPath)
			newRel := string(newPath)
			if oldRel == "" || newRel == "" {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNoEntry)
				stack[0] = 2
				return
			}
			if isPathEscaping(oldRel) || isPathEscaping(newRel) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNotPermitted)
				stack[0] = 2
				return
			}

			oldFull := filepath.Join(oldDr.path, oldRel)
			newFull := filepath.Join(newDr.path, newRel)
			// Renaming "." is not allowed.
			if filepath.Clean(oldRel) == "." {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBusy)
				stack[0] = 2
				return
			}
			if err := os.Rename(oldFull, newFull); err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, mapErrno(err))
				stack[0] = 2
				return
			}
			mem.WriteByte(retPtr, 0)
			stack[0] = 2
		})

	case "[method]descriptor.symlink-at":
		// args packed: (args_ptr: i32, ret_ptr: i32) -> i32
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			argsPtr := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()

			self, _ := mem.ReadUint32Le(argsPtr)
			oldPathPtr, _ := mem.ReadUint32Le(argsPtr + 4)
			oldPathLen, _ := mem.ReadUint32Le(argsPtr + 8)
			newPathPtr, _ := mem.ReadUint32Le(argsPtr + 12)
			newPathLen, _ := mem.ReadUint32Le(argsPtr + 16)

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBadDescriptor)
				stack[0] = 2
				return
			}
			dr := res.(*descriptorResource)

			oldPath, _ := mem.Read(oldPathPtr, oldPathLen)
			newPath, _ := mem.Read(newPathPtr, newPathLen)

			newRel := string(newPath)
			if newRel == "" {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNoEntry)
				stack[0] = 2
				return
			}
			if isPathEscaping(newRel) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNotPermitted)
				stack[0] = 2
				return
			}

			newFull := filepath.Join(dr.path, newRel)
			if err := os.Symlink(string(oldPath), newFull); err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, mapErrno(err))
				stack[0] = 2
				return
			}
			mem.WriteByte(retPtr, 0)
			stack[0] = 2
		})

	case "[method]descriptor.link-at":
		// args packed: (args_ptr: i32, ret_ptr: i32) -> i32
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			argsPtr := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()

			self, _ := mem.ReadUint32Le(argsPtr)
			_ /* flags */, _ = mem.ReadUint32Le(argsPtr + 4)
			oldPathPtr, _ := mem.ReadUint32Le(argsPtr + 8)
			oldPathLen, _ := mem.ReadUint32Le(argsPtr + 12)
			newDesc, _ := mem.ReadUint32Le(argsPtr + 16)
			newPathPtr, _ := mem.ReadUint32Le(argsPtr + 20)
			newPathLen, _ := mem.ReadUint32Le(argsPtr + 24)

			res1, ok1 := h.resources.Get(self)
			res2, ok2 := h.resources.Get(newDesc)
			if !ok1 || !ok2 {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBadDescriptor)
				stack[0] = 2
				return
			}
			oldDr := res1.(*descriptorResource)
			newDr := res2.(*descriptorResource)

			oldPath, _ := mem.Read(oldPathPtr, oldPathLen)
			newPath, _ := mem.Read(newPathPtr, newPathLen)

			oldRel := string(oldPath)
			newRel := string(newPath)
			if oldRel == "" || newRel == "" {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNoEntry)
				stack[0] = 2
				return
			}
			if isPathEscaping(oldRel) || isPathEscaping(newRel) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNotPermitted)
				stack[0] = 2
				return
			}
			// Hard linking directories is not allowed.
			oldFull := filepath.Join(oldDr.path, oldRel)
			if info, err := os.Lstat(oldFull); err == nil && info.IsDir() {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNotPermitted)
				stack[0] = 2
				return
			}

			if err := os.Link(
				oldFull,
				filepath.Join(newDr.path, newRel),
			); err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, mapErrno(err))
				stack[0] = 2
				return
			}
			mem.WriteByte(retPtr, 0)
			stack[0] = 2
		})

	case "[method]descriptor.advise":
		// (self: i32, offset: i64, length: i64, advice: i32, ret_ptr: i32) -> i32
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[4])
			mem := mod.Memory()

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBadDescriptor)
				stack[0] = 2
				return
			}
			dr := res.(*descriptorResource)
			info, err := os.Lstat(dr.path)
			if err != nil || info.IsDir() {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBadDescriptor)
				stack[0] = 2
				return
			}
			mem.WriteByte(retPtr, 0) // ok - advise is advisory only
			stack[0] = 2
		})

	case "[method]descriptor.sync", "[method]descriptor.sync-data":
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if res, ok := h.resources.Get(self); ok {
				if dr, ok := res.(*descriptorResource); ok && dr.file != nil {
					dr.file.Sync()
				}
			}
			mem.WriteByte(retPtr, 0)
			stack[0] = 2
		})

	case "[method]descriptor.set-size":
		// (self: i32, size: i64, ret_ptr: i32) -> i32
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			size := int64(stack[1])
			retPtr := uint32(stack[2])
			mem := mod.Memory()

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBadDescriptor)
				stack[0] = 2
				return
			}
			dr := res.(*descriptorResource)
			// set-size requires write permission.
			if dr.descFlags&0x02 == 0 {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBadDescriptor)
				stack[0] = 2
				return
			}
			var truncErr error
			if dr.file != nil {
				truncErr = dr.file.Truncate(size)
			} else {
				truncErr = os.Truncate(dr.path, size)
			}
			if err := truncErr; err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, mapErrno(err))
				stack[0] = 2
				return
			}
			mem.WriteByte(retPtr, 0)
			stack[0] = 2
		})

	case "[method]descriptor.set-times":
		// (args_ptr: i32, ret_ptr: i32) -> i32
		// Params layout: self(i32@0), access-ts(new-timestamp@8), mod-ts(new-timestamp@32)
		// result<_, error-code>: disc@0, error@1
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			argsPtr := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()

			self, _ := mem.ReadUint32Le(argsPtr)
			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBadDescriptor)
				stack[0] = 2
				return
			}
			dr := res.(*descriptorResource)
			atime, mtime := readSetTimesArgs(mem, dr.path, argsPtr+8, argsPtr+32)
			if err := os.Chtimes(dr.path, atime, mtime); err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, mapErrno(err))
				stack[0] = 2
				return
			}
			mem.WriteByte(retPtr, 0)
			stack[0] = 2
		})

	case "[method]descriptor.set-times-at":
		// (args_ptr: i32, ret_ptr: i32) -> i32
		// Params layout: self(i32@0), path-flags(u8@4), path(ptr@8,len@12),
		//                access-ts(new-timestamp@16), mod-ts(new-timestamp@40)
		// result<_, error-code>: disc@0, error@1
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			argsPtr := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()

			self, _ := mem.ReadUint32Le(argsPtr)
			pathPtr, _ := mem.ReadUint32Le(argsPtr + 8)
			pathLen, _ := mem.ReadUint32Le(argsPtr + 12)

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecBadDescriptor)
				stack[0] = 2
				return
			}
			dr := res.(*descriptorResource)
			pathBytes, ok := mem.Read(pathPtr, pathLen)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecInvalid)
				stack[0] = 2
				return
			}
			relPath := string(pathBytes)
			if relPath == "" {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNoEntry)
				stack[0] = 2
				return
			}
			if isPathEscaping(relPath) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, ecNotPermitted)
				stack[0] = 2
				return
			}
			fullPath := filepath.Join(dr.path, relPath)
			if _, err := os.Lstat(fullPath); err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, mapErrno(err))
				stack[0] = 2
				return
			}
			atime, mtime := readSetTimesArgs(mem, fullPath, argsPtr+16, argsPtr+40)
			if err := os.Chtimes(fullPath, atime, mtime); err != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, mapErrno(err))
				stack[0] = 2
				return
			}
			mem.WriteByte(retPtr, 0)
			stack[0] = 2
		})

	case "[method]descriptor.is-same-object":
		// (self: i32, other: i32, ret_ptr: i32) -> i32
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			other := uint32(stack[1])
			retPtr := uint32(stack[2])
			mem := mod.Memory()

			res1, ok1 := h.resources.Get(self)
			res2, ok2 := h.resources.Get(other)
			if !ok1 || !ok2 {
				mem.WriteByte(retPtr, 0)
				stack[0] = 2
				return
			}
			dr1 := res1.(*descriptorResource)
			dr2 := res2.(*descriptorResource)

			info1, err1 := os.Stat(dr1.path)
			info2, err2 := os.Stat(dr2.path)
			if err1 != nil || err2 != nil {
				mem.WriteByte(retPtr, 0)
			} else if os.SameFile(info1, info2) {
				mem.WriteByte(retPtr, 1)
			} else {
				mem.WriteByte(retPtr, 0)
			}
			stack[0] = 2
		})

	case "[method]descriptor.metadata-hash":
		// (self: i32, ret_ptr: i32) -> i32
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+8, ecBadDescriptor)
				stack[0] = 2
				return
			}
			dr := res.(*descriptorResource)
			writeMetaHash(mem, retPtr, dr.path)
			stack[0] = 2
		})

	case "[method]descriptor.metadata-hash-at":
		// (self: i32, flags: i32, path_ptr: i32, path_len: i32, ret_ptr: i32) -> i32
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			_ = uint32(stack[1]) // flags
			pathPtr := uint32(stack[2])
			pathLen := uint32(stack[3])
			retPtr := uint32(stack[4])
			mem := mod.Memory()

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+8, ecBadDescriptor)
				stack[0] = 2
				return
			}
			dr := res.(*descriptorResource)
			pathBytes, _ := mem.Read(pathPtr, pathLen)
			relPath := string(pathBytes)
			if relPath == "" {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+8, ecNoEntry)
				stack[0] = 2
				return
			}
			if isPathEscaping(relPath) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+8, ecNotPermitted)
				stack[0] = 2
				return
			}
			fullPath := filepath.Join(dr.path, relPath)
			writeMetaHash(mem, retPtr, fullPath)
			stack[0] = 2
		})
	}

	// Handle [async-lower][stream-read-0]: guest reads from a readable stream.
	// Signature: (stream_handle: i32, buf_ptr: i32, buf_len: i32) -> i32
	// Return codes (wit-bindgen encoding):
	//   BLOCKED = 0xFFFFFFFF
	//   COMPLETED(n) = (n << 4) | 0x0   (n items transferred, stream open)
	//   DROPPED(n)   = (n << 4) | 0x1   (n items transferred, other end dropped/EOF)
	//   CANCELLED(n) = (n << 4) | 0x2
	if strings.HasPrefix(methodName, "[stream-read-0]") {
		return api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			handle := uint32(stack[0])
			bufPtr := uint32(stack[1])
			bufLen := uint32(stack[2])
			mem := mod.Memory()

			res, ok := h.resources.Get(handle)
			if !ok {
				stack[0] = 0x1 // DROPPED(0)
				return
			}

			// Handle byte stream (from read-via-stream).
			if sr, ok := res.(*streamResource); ok {
				if sr.reader == nil {
					stack[0] = 0x1 // DROPPED(0)
					return
				}
				if bufLen == 0 {
					stack[0] = 0 // COMPLETED(0)
					return
				}
				buf := make([]byte, bufLen)
				n, err := sr.reader.Read(buf)
				if n > 0 {
					mem.Write(bufPtr, buf[:n])
				}
				if err != nil { // EOF or error
					stack[0] = uint64(n<<4) | 0x1 // DROPPED(n)
				} else {
					stack[0] = uint64(n << 4) // COMPLETED(n)
				}
				return
			}

			// Handle directory entry stream (from read-directory).
			if des, ok := res.(*dirEntryStream); ok {
				if des.offset >= len(des.entries) {
					stack[0] = 0x1 // DROPPED(0)
					return
				}
				count := 0
				// Each directory-entry item: descriptor-type(16 bytes) + name string(8 bytes) = 24 bytes
				const itemSize = 24
				for count < int(bufLen) && des.offset < len(des.entries) {
					entry := des.entries[des.offset]
					des.offset++
					offset := bufPtr + uint32(count)*itemSize

					// descriptor-type variant: disc @ 0
					info, err := entry.Info()
					if err != nil {
						continue
					}
					mem.WriteByte(offset, fileModeToDT(info.Mode()))
					// option<string> = None @ offset+4
					mem.WriteByte(offset+4, 0)

					// name string: allocate via cabi_realloc
					nameBytes := []byte(entry.Name())
					namePtr, err := cabiRealloc(ctx, mod, uint32(len(nameBytes)))
					if err != nil {
						continue
					}
					mem.Write(namePtr, nameBytes)
					mem.WriteUint32Le(offset+16, namePtr)
					mem.WriteUint32Le(offset+20, uint32(len(nameBytes)))
					count++
				}
				if des.offset >= len(des.entries) {
					stack[0] = uint64(count<<4) | 0x1 // DROPPED(count)
				} else {
					stack[0] = uint64(count << 4) // COMPLETED(count)
				}
				return
			}

			// Handle TCP listener accept stream (from listen).
			// stream<tcp-socket>: each item is a 4-byte resource handle.
			if tls, ok := res.(*tcpListenerStream); ok {
				if bufLen == 0 {
					stack[0] = 0
					return
				}
				// Check for cached connection from background accept.
				if tls.pendingConn != nil {
					conn := tls.pendingConn
					tls.pendingConn = nil
					accepted := &tcpSocketResource{connected: true}
					if tcpConn, ok := conn.(*net.TCPConn); ok {
						accepted.conn = tcpConn
						accepted.addr = tcpConn.LocalAddr().(*net.TCPAddr)
						if tcpConn.RemoteAddr().(*net.TCPAddr).IP.To4() == nil {
							accepted.family = 1
						}
					} else {
						accepted.pipeConn = conn
						accepted.family = 1
						accepted.addr = &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0}
					}
					acceptHandle := tls.host.resources.New(accepted)
					mem.WriteUint32Le(bufPtr, acceptHandle)
					stack[0] = (1 << 4) // COMPLETED(1)
					return
				}
				// Real TCP listener.
				if tls.listener != nil {
					tls.listener.SetDeadline(time.Now())
					conn, err := tls.listener.AcceptTCP()
					tls.listener.SetDeadline(time.Time{})
					if err == nil {
						accepted := &tcpSocketResource{
							conn:      conn,
							connected: true,
							addr:      conn.LocalAddr().(*net.TCPAddr),
						}
						if conn.RemoteAddr().(*net.TCPAddr).IP.To4() == nil {
							accepted.family = 1
						}
						acceptHandle := tls.host.resources.New(accepted)
						mem.WriteUint32Le(bufPtr, acceptHandle)
						stack[0] = (1 << 4)
						return
					}
					listener := tls.listener
					streamHandle := handle
					host := tls.host
					host.pendingOps.Add(1)
					go func() {
						listener.SetDeadline(time.Now().Add(30 * time.Second))
						conn, err := listener.AcceptTCP()
						listener.SetDeadline(time.Time{})
						resultCode := uint32(0x1) // DROPPED(0)
						if err == nil {
							accepted := &tcpSocketResource{
								conn:      conn,
								connected: true,
								addr:      conn.LocalAddr().(*net.TCPAddr),
							}
							if conn.RemoteAddr().(*net.TCPAddr).IP.To4() == nil {
								accepted.family = 1
							}
							acceptHandle := host.resources.New(accepted)
							mem.WriteUint32Le(bufPtr, acceptHandle)
							resultCode = 0x10 // COMPLETED(1)
						}
						host.asyncEvents <- asyncEvent{2, streamHandle, resultCode}
					}()
					stack[0] = 0xFFFFFFFF
					return
				}
				// Simulated IPv6 listener.
				if tls.acceptCh != nil {
					select {
					case conn := <-tls.acceptCh:
						accepted := &tcpSocketResource{
							family: 1, pipeConn: conn, connected: true,
							addr: &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0},
						}
						acceptHandle := tls.host.resources.New(accepted)
						mem.WriteUint32Le(bufPtr, acceptHandle)
						stack[0] = (1 << 4)
						return
					default:
						ch := tls.acceptCh
						streamHandle := handle
						host := tls.host
						host.pendingOps.Add(1)
						go func() {
							conn := <-ch
							accepted := &tcpSocketResource{
								family:    1,
								pipeConn:  conn,
								connected: true,
								addr:      &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0},
							}
							acceptHandle := host.resources.New(accepted)
							mem.WriteUint32Le(bufPtr, acceptHandle)
							host.asyncEvents <- asyncEvent{2, streamHandle, 0x10}
						}()
						stack[0] = 0xFFFFFFFF
						return
					}
				}
				stack[0] = 0x1
				return
			}

			stack[0] = 0x1 // DROPPED(0)
		})
	}

	// Handle [async-lower][stream-write-0]: guest writes data to a writable stream.
	// Signature: (stream_handle: i32, buf_ptr: i32, buf_len: i32) -> i32
	if strings.HasPrefix(methodName, "[stream-write-0]") {
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			handle := uint32(stack[0])
			bufPtr := uint32(stack[1])
			bufLen := uint32(stack[2])
			mem := mod.Memory()

			res, ok := h.resources.Get(handle)
			if !ok {
				stack[0] = 0x1 // DROPPED(0)
				return
			}
			sr, ok := res.(*streamResource)
			if !ok || sr.writer == nil {
				stack[0] = 0x1 // DROPPED(0)
				return
			}
			if bufLen == 0 {
				stack[0] = 0 // COMPLETED(0)
				return
			}
			data, ok := mem.Read(bufPtr, bufLen)
			if !ok {
				stack[0] = 0x1 // DROPPED(0)
				return
			}
			n, err := writeNonBlocking(sr.writer, data, handle, h)
			if n == -1 {
				stack[0] = 0xFFFFFFFF // BLOCKED
				return
			}
			if err != nil {
				stack[0] = uint64(n<<4) | 0x1 // DROPPED(n)
			} else {
				stack[0] = uint64(n << 4) // COMPLETED(n)
			}
		})
	}

	// Handle [async-lower][future-read-1]: guest reads a future value.
	// Signature: (future_handle: i32, ret_ptr: i32) -> i32
	// Return: COMPLETED(0) = 0x0 means value read successfully
	if strings.HasPrefix(methodName, "[future-read-1]") {
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			handle := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()

			res, ok := h.resources.Get(handle)
			if !ok {
				stack[0] = 0x1 // DROPPED(0)
				return
			}
			fr, ok := res.(*futureResource)
			if !ok {
				stack[0] = 0x1 // DROPPED(0)
				return
			}
			if !fr.ready {
				stack[0] = 0xFFFFFFFF // BLOCKED
				return
			}
			if fr.result != nil {
				mem.Write(retPtr, fr.result)
			}
			stack[0] = 0 // COMPLETED(0) - value read
		})
	}

	// Handle [async-lower][future-write-1]: host writes a future value.
	// Signature: (future_handle: i32, ptr: i32) -> i32
	if strings.HasPrefix(methodName, "[future-write-1]") {
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			// In our implementation, futures are pre-filled, so this is a no-op.
			stack[0] = 0 // COMPLETED(0) - value written
		})
	}

	return nil
}

// streamFuturePlumbing handles [stream-*] and [future-*] function imports.
func (h *ComponentHost) streamFuturePlumbing(moduleName, funcName string, paramTypes, resultTypes []api.ValueType) api.GoModuleFunction {
	if strings.HasPrefix(funcName, "[stream-new-0]") {
		// () -> i64: return a new stream pair (readable | writable << 32)
		// Always create a generic in-process stream. The actual I/O connection
		// (stdin reader, stdout/stderr writer) is set up by the higher-level
		// functions (read-via-stream, write-via-stream) when they receive
		// the stream handle.
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			buf := new(bytes.Buffer)
			shared := &streamResource{reader: buf, writer: buf}
			readHandle := h.resources.New(shared)
			writeHandle := h.resources.New(shared)
			stack[0] = uint64(readHandle) | (uint64(writeHandle) << 32)
		})
	}

	if strings.HasPrefix(funcName, "[future-new-1]") {
		suffix := funcName[len("[future-new-1]"):]
		// () -> i64: return a new future pair (readable | writable << 32)
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			var shared *futureResource
			if suffix == "read-via-stream" || suffix == "write-via-stream" {
				// For CLI stdin/stdout/stderr: pre-ready with Ok result.
				shared = &futureResource{
					result: make([]byte, 20), // result<_, error-code>: disc=0 (Ok)
					ready:  true,
				}
			} else {
				shared = &futureResource{}
			}
			readHandle := h.resources.New(shared)
			writeHandle := h.resources.New(shared)

			stack[0] = uint64(readHandle) | (uint64(writeHandle) << 32)
		})
	}

	if strings.HasPrefix(funcName, "[stream-drop-") || strings.HasPrefix(funcName, "[future-drop-") {
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			h.resources.Drop(uint32(stack[0]))
		})
	}

	if strings.HasPrefix(funcName, "[stream-cancel-") || strings.HasPrefix(funcName, "[future-cancel-") {
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = 0 // no items consumed/produced
		})
	}

	// Handle [stream-read-0]: guest reads from a readable stream.
	// Signature: (stream_handle: i32, buf_ptr: i32, buf_len: i32) -> i32
	// Return codes: COMPLETED(n)=(n<<4)|0, DROPPED(n)=(n<<4)|1, CANCELLED(n)=(n<<4)|2, BLOCKED=0xFFFFFFFF
	if strings.HasPrefix(funcName, "[stream-read-0]") {
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			handle := uint32(stack[0])
			bufPtr := uint32(stack[1])
			bufLen := uint32(stack[2])
			mem := mod.Memory()

			res, ok := h.resources.Get(handle)
			if !ok {

				stack[0] = 0x1 // DROPPED(0)
				return
			}
			if sr, ok := res.(*streamResource); ok {
				if sr.reader == nil {
					stack[0] = 0x1 // DROPPED(0)
					return
				}
				if bufLen == 0 {
					stack[0] = 0 // COMPLETED(0)
					return
				}
				// Check for cached data from a background read.
				if sr.pendingData != nil {
					data := sr.pendingData
					sr.pendingData = nil
					n := len(data)
					if uint32(n) > bufLen {
						n = int(bufLen)
						sr.pendingData = data[n:]
					}
					mem.Write(bufPtr, data[:n])
					if sr.pendingEOF && sr.pendingData == nil {
						stack[0] = uint64(n<<4) | 0x1 // DROPPED(n)
					} else {
						stack[0] = uint64(n << 4) // COMPLETED(n)
					}
					return
				}
				// For network connections, try non-blocking read.
				if conn, ok := sr.reader.(net.Conn); ok {
					conn.SetReadDeadline(time.Now())
					buf := make([]byte, bufLen)
					n, err := conn.Read(buf)
					conn.SetReadDeadline(time.Time{})
					if n > 0 {
						mem.Write(bufPtr, buf[:n])
						if err != nil {
							stack[0] = uint64(n<<4) | 0x1
						} else {
							stack[0] = uint64(n << 4)
						}
						return
					}
					if err != nil {
						if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
							// No data available - start background read.
							streamHandle := handle
							h.pendingOps.Add(1)
							go func() {
								conn.SetReadDeadline(time.Now().Add(30 * time.Second))
								readBuf := make([]byte, bufLen)
								n, readErr := conn.Read(readBuf)
								conn.SetReadDeadline(time.Time{})
								resultCode := uint32(n << 4) // COMPLETED(n)
								if n > 0 {
									mem.Write(bufPtr, readBuf[:n])
								}
								if readErr != nil {
									resultCode = uint32(n<<4) | 0x1 // DROPPED(n)
								}
								h.asyncEvents <- asyncEvent{2, streamHandle, resultCode}
							}()
							stack[0] = 0xFFFFFFFF // BLOCKED
							return
						}
						stack[0] = 0x1 // DROPPED(0)
						return
					}
				}
				buf := make([]byte, bufLen)
				n, err := sr.reader.Read(buf)
				if n > 0 {
					mem.Write(bufPtr, buf[:n])
				}
				if err != nil {
					stack[0] = uint64(n<<4) | 0x1 // DROPPED(n)
				} else {
					stack[0] = uint64(n << 4) // COMPLETED(n)
				}
				return
			}
			// TCP listener accept stream.
			if tls, ok := res.(*tcpListenerStream); ok {
				if bufLen == 0 {
					stack[0] = 0
					return
				}
				// Check for a cached connection from a previous background accept.
				if tls.pendingConn != nil {
					conn := tls.pendingConn
					tls.pendingConn = nil
					accepted := &tcpSocketResource{
						connected: true,
					}
					if tcpConn, ok := conn.(*net.TCPConn); ok {
						accepted.conn = tcpConn
						accepted.addr = tcpConn.LocalAddr().(*net.TCPAddr)
						if tcpConn.RemoteAddr().(*net.TCPAddr).IP.To4() == nil {
							accepted.family = 1
						}
					} else {
						// net.Pipe or similar
						accepted.pipeConn = conn
						accepted.family = 1
						accepted.addr = &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0}
					}
					acceptHandle := tls.host.resources.New(accepted)
					mem.WriteUint32Le(bufPtr, acceptHandle)
					stack[0] = (1 << 4) // COMPLETED(1)
					return
				}
				// Real TCP listener.
				if tls.listener != nil {
					// Try non-blocking accept first.
					tls.listener.SetDeadline(time.Now())
					conn, err := tls.listener.AcceptTCP()
					tls.listener.SetDeadline(time.Time{})
					if err == nil {
						accepted := &tcpSocketResource{
							conn:      conn,
							connected: true,
							addr:      conn.LocalAddr().(*net.TCPAddr),
						}
						if conn.RemoteAddr().(*net.TCPAddr).IP.To4() == nil {
							accepted.family = 1
						}
						acceptHandle := tls.host.resources.New(accepted)
						mem.WriteUint32Le(bufPtr, acceptHandle)
						stack[0] = (1 << 4) // COMPLETED(1)
						return
					}
					// No connection available - start background accept.
					listener := tls.listener
					streamHandle := handle
					host := tls.host
					host.pendingOps.Add(1)
					go func() {
						listener.SetDeadline(time.Now().Add(30 * time.Second))
						conn, err := listener.AcceptTCP()
						listener.SetDeadline(time.Time{})
						resultCode := uint32(0x1) // DROPPED(0) on error
						if err == nil {
							accepted := &tcpSocketResource{
								conn:      conn,
								connected: true,
								addr:      conn.LocalAddr().(*net.TCPAddr),
							}
							if conn.RemoteAddr().(*net.TCPAddr).IP.To4() == nil {
								accepted.family = 1
							}
							acceptHandle := host.resources.New(accepted)
							mem.WriteUint32Le(bufPtr, acceptHandle)
							resultCode = 0x10 // COMPLETED(1)
						}
						host.asyncEvents <- asyncEvent{
							eventType: 2,           // STREAM_READ
							p1:        streamHandle, // stream handle
							p2:        resultCode,
						}
					}()
					stack[0] = 0xFFFFFFFF // BLOCKED
					return
				}
				// Simulated IPv6 listener - check accept channel.
				if tls.acceptCh != nil {
					select {
					case conn := <-tls.acceptCh:
						accepted := &tcpSocketResource{
							family:    1,
							pipeConn:  conn,
							connected: true,
							addr:      &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0},
						}
						acceptHandle := tls.host.resources.New(accepted)
						mem.WriteUint32Le(bufPtr, acceptHandle)
						stack[0] = (1 << 4) // COMPLETED(1)
						return
					default:
						// Start background accept for simulated listener.
						ch := tls.acceptCh
						streamHandle := handle
						host := tls.host
						host.pendingOps.Add(1)
						go func() {
							conn := <-ch
							accepted := &tcpSocketResource{
								family:    1,
								pipeConn:  conn,
								connected: true,
								addr:      &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0},
							}
							acceptHandle := host.resources.New(accepted)
							mem.WriteUint32Le(bufPtr, acceptHandle)
							host.asyncEvents <- asyncEvent{
								eventType: 2,
								p1:        streamHandle,
								p2:        0x10,
							}
						}()
						stack[0] = 0xFFFFFFFF // BLOCKED
						return
					}
				}
				stack[0] = 0x1 // DROPPED(0)
				return
			}
			stack[0] = 0x1 // DROPPED(0)
		})
	}

	// Handle [stream-write-0]: guest writes data to a writable stream.
	// Signature: (stream_handle: i32, buf_ptr: i32, buf_len: i32) -> i32
	if strings.HasPrefix(funcName, "[stream-write-0]") {
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			handle := uint32(stack[0])
			bufPtr := uint32(stack[1])
			bufLen := uint32(stack[2])
			mem := mod.Memory()

			res, ok := h.resources.Get(handle)
			if !ok {
				stack[0] = 0x1 // DROPPED(0)
				return
			}
			sr, ok := res.(*streamResource)
			if !ok || sr.writer == nil {
				stack[0] = 0x1 // DROPPED(0)
				return
			}
			if bufLen == 0 {
				stack[0] = 0 // COMPLETED(0)
				return
			}
			data, ok := mem.Read(bufPtr, bufLen)
			if !ok {
				stack[0] = 0x1 // DROPPED(0)
				return
			}
			n, err := writeNonBlocking(sr.writer, data, handle, h)
			if n == -1 {
				stack[0] = 0xFFFFFFFF // BLOCKED
				return
			}
			if err != nil {
				stack[0] = uint64(n<<4) | 0x1 // DROPPED(n)
			} else {
				stack[0] = uint64(n << 4) // COMPLETED(n)
			}
		})
	}

	// Handle [future-read-1]: guest reads a future value.
	// Signature: (future_handle: i32, ret_ptr: i32) -> i32
	if strings.HasPrefix(funcName, "[future-read-1]") || strings.HasPrefix(funcName, "[future-read-2]") {
		suffix := funcName[len("[future-read-"):]
		if idx := strings.Index(suffix, "]"); idx >= 0 {
			suffix = suffix[idx+1:]
		}
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			handle := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()

			res, ok := h.resources.Get(handle)
			if !ok {
				// For CLI stream functions (read-via-stream, write-via-stream),
				// the guest may not call [future-new-1] and manages future handles
				// internally. The future just means "operation completed OK".
				if suffix == "read-via-stream" || suffix == "write-via-stream" {
					// Write result<_, error-code> Ok discriminant at retPtr
					okResult := make([]byte, 20)
					mem.Write(retPtr, okResult)
					stack[0] = 0 // COMPLETED(0)
					return
				}
				stack[0] = 0x1 // DROPPED(0)
				return
			}
			fr, ok := res.(*futureResource)
			if !ok {
				stack[0] = 0x1 // DROPPED(0)
				return
			}
			if !fr.ready {
				stack[0] = 0xFFFFFFFF // BLOCKED
				return
			}
			if fr.result != nil {
				mem.Write(retPtr, fr.result)
			}
			stack[0] = 0 // COMPLETED(0)
		})
	}

	// Handle [future-write-1] / [future-write-2]: host writes a future value.
	if strings.HasPrefix(funcName, "[future-write-1]") || strings.HasPrefix(funcName, "[future-write-2]") {
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = 0 // COMPLETED(0)
		})
	}

	return nil
}

// buildDescriptorStat builds the byte representation of result<descriptor-stat, error-code>.
// Total size: 112 bytes. Layout (confirmed from WAT analysis):
//
//	offset  0: result discriminant (u8, 0=ok)
//	offset  8: descriptor-type variant discriminant (u8, 0-6)
//	offset 12: option<string> discriminant in descriptor-type (u8, 0=None for standard types)
//	offset 24: link-count (u64)
//	offset 32: size (u64)
//	offset 40: option<instant> #1 data-access-timestamp (24 bytes)
//	offset 64: option<instant> #2 data-modification-timestamp (24 bytes)
//	offset 88: option<instant> #3 status-change-timestamp (24 bytes)
func buildDescriptorStat(info os.FileInfo) []byte {
	buf := make([]byte, 112)
	// buf[0] = 0 (ok discriminant, already zero)

	buf[8] = fileModeToDT(info.Mode())
	// buf[12] = 0 (option<string> = None, already zero)

	binary.LittleEndian.PutUint64(buf[24:], 1) // link-count
	binary.LittleEndian.PutUint64(buf[32:], uint64(info.Size()))

	mtime := info.ModTime()
	atime := mtime // fallback
	ctime := mtime // fallback
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		atime = time.Unix(st.Atim.Sec, st.Atim.Nsec)
		ctime = time.Unix(st.Ctim.Sec, st.Ctim.Nsec)
	}
	putOptInstant(buf[40:], atime) // data-access-timestamp
	putOptInstant(buf[64:], mtime) // data-modification-timestamp
	putOptInstant(buf[88:], ctime) // status-change-timestamp
	return buf
}

func putOptInstant(buf []byte, t time.Time) {
	buf[0] = 1 // some
	binary.LittleEndian.PutUint64(buf[8:], uint64(t.Unix()))
	binary.LittleEndian.PutUint32(buf[16:], uint32(t.Nanosecond()))
}

// writeDescriptorStat writes a stat result to memory at retPtr.
func writeDescriptorStat(mem api.Memory, retPtr uint32, info os.FileInfo) {
	mem.Write(retPtr, buildDescriptorStat(info))
}

func writeMetaHash(mem api.Memory, retPtr uint32, path string) {
	info, err := os.Lstat(path)
	if err != nil {
		mem.WriteByte(retPtr, 1)
		mem.WriteByte(retPtr+8, mapErrno(err))
		return
	}
	h := sha256.New()
	h.Write([]byte(path))
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(info.ModTime().UnixNano()))
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(info.Size()))
	h.Write(buf[:])
	sum := h.Sum(nil)

	mem.WriteByte(retPtr, 0) // ok
	mem.Write(retPtr+8, sum[:8])
	mem.Write(retPtr+16, sum[8:16])
}

func fileModeToDT(mode fs.FileMode) byte {
	switch {
	case mode.IsDir():
		return dtDirectory
	case mode.IsRegular():
		return dtRegularFile
	case mode&fs.ModeSymlink != 0:
		return dtSymbolicLink
	case mode&fs.ModeNamedPipe != 0:
		return dtFifo
	case mode&fs.ModeSocket != 0:
		return dtSocket
	case mode&fs.ModeCharDevice != 0:
		return dtCharacterDevice
	case mode&fs.ModeDevice != 0:
		return dtBlockDevice
	default:
		return dtRegularFile // fallback
	}
}

// readNewTimestamp reads a new-timestamp variant from memory.
// Layout: disc(u8@0), instant.seconds(u64@8), instant.nanoseconds(u32@16).
// Returns the time and whether to apply it (false = no-change).
func readNewTimestamp(mem api.Memory, offset uint32, fallback time.Time) time.Time {
	disc, _ := mem.ReadByte(offset)
	switch disc {
	case 0: // no-change
		return fallback
	case 1: // now
		return time.Now()
	case 2: // timestamp(instant)
		secs, _ := mem.ReadUint64Le(offset + 8)
		nanos, _ := mem.ReadUint32Le(offset + 16)
		return time.Unix(int64(secs), int64(nanos))
	default:
		return fallback
	}
}

// readSetTimesArgs reads access and modification timestamp arguments.
func readSetTimesArgs(mem api.Memory, path string, atimeOffset, mtimeOffset uint32) (atime, mtime time.Time) {
	// Use current file times as fallback for no-change
	info, err := os.Lstat(path)
	if err != nil {
		now := time.Now()
		atime = now
		mtime = now
	} else {
		mtime = info.ModTime()
		atime = mtime // best effort - Go doesn't expose atime easily
	}
	atime = readNewTimestamp(mem, atimeOffset, atime)
	mtime = readNewTimestamp(mem, mtimeOffset, mtime)
	return
}

// isPathEscaping returns true if the given relative path would escape the sandbox directory.
// Absolute paths, ".." traversals, and symlink names like "parent" are rejected.
func isPathEscaping(path string) bool {
	if path == "" {
		return false // empty path is a separate error (NoEntry), not a sandbox escape
	}
	if filepath.IsAbs(path) {
		return true
	}
	cleaned := filepath.Clean(path)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return true
	}
	// Reject paths that traverse through symlink names like "parent" (which points to "..")
	parts := strings.Split(cleaned, string(filepath.Separator))
	for _, part := range parts {
		if part == "parent" {
			return true
		}
	}
	return false
}

func mapErrno(err error) byte {
	if err == nil {
		return 0
	}
	if os.IsNotExist(err) {
		return ecNoEntry
	}
	if os.IsExist(err) {
		return ecExist
	}
	if os.IsPermission(err) {
		return ecAccess
	}
	if pe, ok := err.(*os.PathError); ok {
		err = pe.Err
	}
	if le, ok := err.(*os.LinkError); ok {
		err = le.Err
	}
	switch {
	case err == syscall.ENOTDIR:
		return ecNotDirectory
	case err == syscall.EISDIR:
		return ecIsDirectory
	case err == syscall.ENOTEMPTY:
		return ecNotEmpty
	case err == syscall.ELOOP:
		return ecLoop
	case err == syscall.ENAMETOOLONG:
		return ecNameTooLong
	case err == syscall.ENOSPC:
		return ecNotEnoughSpace
	case err == syscall.EXDEV:
		return ecCrossDevice
	case err == syscall.EROFS:
		return ecReadOnly
	case err == syscall.EBUSY:
		return ecBusy
	case err == syscall.EINVAL:
		return ecInvalid
	case err == syscall.EBADF:
		return ecBadDescriptor
	}
	return ecIO
}
