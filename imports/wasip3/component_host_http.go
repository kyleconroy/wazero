package wasip3

import (
	"context"
	"strings"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// headerErrorInvalidSyntax is the WASI HTTP header-error enum value for invalid-syntax.
const headerErrorInvalidSyntax = 0

// isValidFieldName checks if a header name is a valid HTTP token per RFC 9110 Section 5.6.2.
func isValidFieldName(name string) bool {
	if len(name) == 0 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !isTchar(c) {
			return false
		}
	}
	return true
}

func isTchar(c byte) bool {
	// tchar = "!" / "#" / "$" / "%" / "&" / "'" / "*" / "+" / "-" / "." /
	//         "^" / "_" / "`" / "|" / "~" / DIGIT / ALPHA
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}

// isValidFieldValue checks if a header value is valid per RFC 9110.
func isValidFieldValue(val []byte) bool {
	for _, b := range val {
		if b == '\t' || b == ' ' || (b >= 0x21 && b <= 0x7e) || b >= 0x80 {
			continue
		}
		return false
	}
	return true
}

// Header error codes per the WASI HTTP types enum.
const headerErrorImmutable = 2

// fieldsResource represents an HTTP Fields (headers/trailers) resource.
type fieldsResource struct {
	entries   []fieldEntry
	immutable bool
}

type fieldEntry struct {
	name  string
	value []byte
}

// requestResource represents an HTTP request resource.
type requestResource struct {
	method    string
	path      string
	scheme    string
	authority string
	headers   uint32 // handle to fieldsResource
}

// responseResource represents an HTTP response resource.
type responseResource struct {
	statusCode uint32
	headers    uint32 // handle to fieldsResource
}

// cloneFieldsImmutable creates a deep, immutable copy of the fieldsResource at the given handle.
func (h *ComponentHost) cloneFieldsImmutable(handle uint32) *fieldsResource {
	res, ok := h.resources.Get(handle)
	if !ok {
		return &fieldsResource{immutable: true}
	}
	fields := res.(*fieldsResource)
	clone := &fieldsResource{entries: make([]fieldEntry, len(fields.entries)), immutable: true}
	for i, e := range fields.entries {
		val := make([]byte, len(e.value))
		copy(val, e.value)
		clone.entries[i] = fieldEntry{name: e.name, value: val}
	}
	return clone
}

// isValidScheme validates a scheme string per RFC 3986: ALPHA *( ALPHA / DIGIT / "+" / "-" / "." )
func isValidScheme(s string) bool {
	if len(s) == 0 {
		return false
	}
	if !isAlpha(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !isAlpha(c) && !(c >= '0' && c <= '9') && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isValidPathChar checks if a character is valid in a URI path per RFC 3986.
func isValidPathChar(c byte) bool {
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
		return true
	}
	switch c {
	case '-', '.', '_', '~', // unreserved
		'%',                      // pct-encoded
		'!', '$', '&', '\'', '(', ')', '*', '+', ',', ';', '=', // sub-delims
		':', '@', // pchar extras
		'/', '?': // path/query separators
		return true
	}
	return c >= 0x80 // raw UTF-8
}

// isValidPath validates a URI path-with-query string.
func isValidPath(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isValidPathChar(s[i]) {
			return false
		}
	}
	return true
}

// isValidAuthorityChar checks if a char is valid in a URI authority.
func isValidAuthorityChar(c byte) bool {
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
		return true
	}
	switch c {
	case '-', '.', '_', '~', // unreserved
		'%',                      // pct-encoded
		'!', '$', '&', '\'', '(', ')', '*', '+', ',', ';', '=': // sub-delims
		return true
	}
	return false
}

// isValidAuthority validates a URI authority string.
func isValidAuthority(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Allow userinfo@host:port, IP-literal [...]
	// Basic validation: check each char is valid authority/userinfo char
	// plus '@', ':', '[', ']' as structural characters.
	hasAt := false
	hasColon := false
	lastColon := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '@' {
			if hasAt {
				return false // double @
			}
			hasAt = true
			hasColon = false
			lastColon = -1
			continue
		}
		if c == ':' {
			hasColon = true
			lastColon = i
			continue
		}
		if !isValidAuthorityChar(c) {
			return false
		}
	}
	// Reject patterns like "::" or ":@" or "@:"
	if s[0] == ':' || s[0] == '@' {
		return false
	}
	if s[len(s)-1] == '@' || s[len(s)-1] == ':' {
		return false
	}
	// If there's a port part (colon after @, or colon with no @), validate it's numeric.
	if hasColon && lastColon >= 0 {
		portStart := lastColon + 1
		if portStart < len(s) {
			// If there's content after the last colon, and it's not before @, it's a port.
			portPart := s[portStart:]
			if !hasAt || lastColon > strings.LastIndex(s, "@") {
				for _, c := range []byte(portPart) {
					if c < '0' || c > '9' {
						return false
					}
				}
			}
		}
	}
	return true
}

// registerHTTP is a no-op; all HTTP functions are handled dynamically by
// httpImportHandler to avoid signature mismatches with the component model's
// flat ABI lowering.
func (h *ComponentHost) registerHTTP(cl *wazero.ComponentLinker) {}

// httpImportHandler handles unregistered imports for wasi:http/* modules.
// It creates functions with the exact paramTypes/resultTypes that the wasm binary expects.
func (h *ComponentHost) httpImportHandler(moduleName, funcName string, paramTypes, resultTypes []api.ValueType) api.GoModuleFunction {
	switch funcName {
	case "[constructor]fields":
		// () -> (i32)
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			handle := h.resources.New(&fieldsResource{})
			stack[0] = uint64(handle)
		})

	case "[resource-drop]fields":
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			h.resources.Drop(uint32(stack[0]))
		})

	case "[resource-drop]request":
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			h.resources.Drop(uint32(stack[0]))
		})

	case "[resource-drop]response":
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			h.resources.Drop(uint32(stack[0]))
		})

	case "[resource-drop]request-options":
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			h.resources.Drop(uint32(stack[0]))
		})

	case "[static]fields.from-list":
		// (list_ptr: i32, list_len: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			listPtr := uint32(stack[0])
			listLen := uint32(stack[1])
			retPtr := uint32(stack[2])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			fields := &fieldsResource{}
			for i := uint32(0); i < listLen; i++ {
				offset := listPtr + i*16
				namePtr, _ := mem.ReadUint32Le(offset)
				nameLen, _ := mem.ReadUint32Le(offset + 4)
				valPtr, _ := mem.ReadUint32Le(offset + 8)
				valLen, _ := mem.ReadUint32Le(offset + 12)
				nameBytes, _ := mem.Read(namePtr, nameLen)
				valBytes, _ := mem.Read(valPtr, valLen)

				name := string(nameBytes)
				if !isValidFieldName(name) || !isValidFieldValue(valBytes) {
					mem.WriteByte(retPtr, 1) // Err
					mem.WriteByte(retPtr+4, headerErrorInvalidSyntax)
					return
				}
				val := make([]byte, len(valBytes))
				copy(val, valBytes)
				fields.entries = append(fields.entries, fieldEntry{name: strings.ToLower(name), value: val})
			}

			handle := h.resources.New(fields)
			mem.WriteByte(retPtr, 0) // Ok
			mem.WriteUint32Le(retPtr+4, handle)
		})

	case "[method]fields.set":
		// (self: i32, name_ptr: i32, name_len: i32, values_ptr: i32, values_len: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			namePtr := uint32(stack[1])
			nameLen := uint32(stack[2])
			valuesPtr := uint32(stack[3])
			valuesLen := uint32(stack[4])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			retPtr := uint32(stack[5])

			// Check immutability first.
			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				return
			}
			fields := res.(*fieldsResource)
			if fields.immutable {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, headerErrorImmutable)
				return
			}

			nameBytes, _ := mem.Read(namePtr, nameLen)
			name := string(nameBytes)
			lowerName := strings.ToLower(name)

			if !isValidFieldName(name) {
				mem.WriteByte(retPtr, 1) // error
				mem.WriteByte(retPtr+4, headerErrorInvalidSyntax)
				return
			}

			// Validate all values first.
			var newEntries []fieldEntry
			for i := uint32(0); i < valuesLen; i++ {
				valPtr, _ := mem.ReadUint32Le(valuesPtr + i*8)
				valLen, _ := mem.ReadUint32Le(valuesPtr + i*8 + 4)
				val, _ := mem.Read(valPtr, valLen)
				if !isValidFieldValue(val) {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, headerErrorInvalidSyntax)
					return
				}
				valCopy := make([]byte, len(val))
				copy(valCopy, val)
				newEntries = append(newEntries, fieldEntry{name: lowerName, value: valCopy})
			}

			filtered := fields.entries[:0]
			for _, e := range fields.entries {
				if e.name != lowerName {
					filtered = append(filtered, e)
				}
			}
			fields.entries = append(filtered, newEntries...)

			mem.WriteByte(retPtr, 0) // ok
		})

	case "[method]fields.delete":
		// (self: i32, name_ptr: i32, name_len: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			namePtr := uint32(stack[1])
			nameLen := uint32(stack[2])
			retPtr := uint32(stack[3])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				return
			}
			fields := res.(*fieldsResource)
			if fields.immutable {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, headerErrorImmutable)
				return
			}

			nameBytes, _ := mem.Read(namePtr, nameLen)
			name := string(nameBytes)

			if !isValidFieldName(name) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, headerErrorInvalidSyntax)
				return
			}

			lowerName := strings.ToLower(name)

			filtered := fields.entries[:0]
			for _, e := range fields.entries {
				if e.name != lowerName {
					filtered = append(filtered, e)
				}
			}
			fields.entries = filtered
			mem.WriteByte(retPtr, 0) // ok
		})

	case "[method]fields.get-and-delete":
		// (self: i32, name_ptr: i32, name_len: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			namePtr := uint32(stack[1])
			nameLen := uint32(stack[2])
			retPtr := uint32(stack[3])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				return
			}
			fields := res.(*fieldsResource)
			if fields.immutable {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, headerErrorImmutable)
				return
			}

			nameBytes, _ := mem.Read(namePtr, nameLen)
			name := string(nameBytes)

			if !isValidFieldName(name) {
				mem.WriteByte(retPtr, 1) // Err
				mem.WriteByte(retPtr+4, headerErrorInvalidSyntax)
				return
			}

			lowerName := strings.ToLower(name)

			var matching [][]byte
			filtered := fields.entries[:0]
			for _, e := range fields.entries {
				if e.name == lowerName {
					matching = append(matching, e.value)
				} else {
					filtered = append(filtered, e)
				}
			}
			fields.entries = filtered

			// result<list<list<u8>>, header-error>: disc at offset 0, payload at offset 4
			mem.WriteByte(retPtr, 0) // Ok

			if len(matching) == 0 {
				mem.WriteUint32Le(retPtr+4, 1) // non-null dangling ptr
				mem.WriteUint32Le(retPtr+8, 0)
				return
			}

			listSize := uint32(len(matching)) * 8
			listAlloc, _ := cabiRealloc(ctx, mod, listSize)
			for i, val := range matching {
				valPtr, _ := cabiRealloc(ctx, mod, uint32(max(len(val), 1)))
				if len(val) > 0 {
					mem.Write(valPtr, val)
				}
				mem.WriteUint32Le(listAlloc+uint32(i)*8, valPtr)
				mem.WriteUint32Le(listAlloc+uint32(i)*8+4, uint32(len(val)))
			}
			mem.WriteUint32Le(retPtr+4, listAlloc)
			mem.WriteUint32Le(retPtr+8, uint32(len(matching)))
		})

	case "[method]fields.append":
		// (self: i32, name_ptr: i32, name_len: i32, val_ptr: i32, val_len: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			namePtr := uint32(stack[1])
			nameLen := uint32(stack[2])
			valPtr := uint32(stack[3])
			valLen := uint32(stack[4])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			retPtr := uint32(stack[5])

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				return
			}
			fields := res.(*fieldsResource)
			if fields.immutable {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, headerErrorImmutable)
				return
			}

			nameBytes, _ := mem.Read(namePtr, nameLen)
			name := string(nameBytes)

			if !isValidFieldName(name) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, headerErrorInvalidSyntax)
				return
			}

			val, _ := mem.Read(valPtr, valLen)
			if !isValidFieldValue(val) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, headerErrorInvalidSyntax)
				return
			}

			valCopy := make([]byte, len(val))
			copy(valCopy, val)

			fields.entries = append(fields.entries, fieldEntry{name: strings.ToLower(name), value: valCopy})
			mem.WriteByte(retPtr, 0) // ok
		})

	case "[method]fields.copy-all":
		// (self: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteUint32Le(retPtr, 1)
				mem.WriteUint32Le(retPtr+4, 0)
				return
			}
			fields := res.(*fieldsResource)

			if len(fields.entries) == 0 {
				mem.WriteUint32Le(retPtr, 1)
				mem.WriteUint32Le(retPtr+4, 0)
				return
			}

			listSize := uint32(len(fields.entries)) * 16
			listAlloc, _ := cabiRealloc(ctx, mod, listSize)
			for i, e := range fields.entries {
				offset := listAlloc + uint32(i)*16
				nameBytes := []byte(e.name)
				// Always allocate (even for empty) to avoid null pointers.
				nameAllocPtr, _ := cabiRealloc(ctx, mod, uint32(max(len(nameBytes), 1)))
				if len(nameBytes) > 0 {
					mem.Write(nameAllocPtr, nameBytes)
				}
				mem.WriteUint32Le(offset, nameAllocPtr)
				mem.WriteUint32Le(offset+4, uint32(len(nameBytes)))

				valAllocPtr, _ := cabiRealloc(ctx, mod, uint32(max(len(e.value), 1)))
				if len(e.value) > 0 {
					mem.Write(valAllocPtr, e.value)
				}
				mem.WriteUint32Le(offset+8, valAllocPtr)
				mem.WriteUint32Le(offset+12, uint32(len(e.value)))
			}
			mem.WriteUint32Le(retPtr, listAlloc)
			mem.WriteUint32Le(retPtr+4, uint32(len(fields.entries)))
		})

	case "[method]fields.clone":
		// (self: i32) -> (i32)
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			self := uint32(stack[0])
			res, ok := h.resources.Get(self)
			if !ok {
				stack[0] = uint64(h.resources.New(&fieldsResource{}))
				return
			}
			fields := res.(*fieldsResource)
			clone := &fieldsResource{entries: make([]fieldEntry, len(fields.entries))}
			for i, e := range fields.entries {
				val := make([]byte, len(e.value))
				copy(val, e.value)
				clone.entries[i] = fieldEntry{name: e.name, value: val}
			}
			stack[0] = uint64(h.resources.New(clone))
		})

	case "[method]fields.get":
		// (self: i32, name_ptr: i32, name_len: i32, ret_ptr: i32) -> ()
		return api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			namePtr := uint32(stack[1])
			nameLen := uint32(stack[2])
			retPtr := uint32(stack[3])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			nameBytes, _ := mem.Read(namePtr, nameLen)
			lowerName := strings.ToLower(string(nameBytes))

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteUint32Le(retPtr, 1)
				mem.WriteUint32Le(retPtr+4, 0)
				return
			}
			fields := res.(*fieldsResource)

			var matching [][]byte
			for _, e := range fields.entries {
				if e.name == lowerName {
					matching = append(matching, e.value)
				}
			}

			if len(matching) == 0 {
				mem.WriteUint32Le(retPtr, 1)
				mem.WriteUint32Le(retPtr+4, 0)
				return
			}

			listSize := uint32(len(matching)) * 8
			listAlloc, _ := cabiRealloc(ctx, mod, listSize)
			for i, val := range matching {
				valPtr, _ := cabiRealloc(ctx, mod, uint32(max(len(val), 1)))
				if len(val) > 0 {
					mem.Write(valPtr, val)
				}
				mem.WriteUint32Le(listAlloc+uint32(i)*8, valPtr)
				mem.WriteUint32Le(listAlloc+uint32(i)*8+4, uint32(len(val)))
			}
			mem.WriteUint32Le(retPtr, listAlloc)
			mem.WriteUint32Le(retPtr+4, uint32(len(matching)))
		})

	case "[method]fields.has":
		// (self: i32, name_ptr: i32, name_len: i32) -> (i32)
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			namePtr := uint32(stack[1])
			nameLen := uint32(stack[2])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			nameBytes, _ := mem.Read(namePtr, nameLen)
			lowerName := strings.ToLower(string(nameBytes))

			res, ok := h.resources.Get(self)
			if !ok {
				stack[0] = 0
				return
			}
			fields := res.(*fieldsResource)
			for _, e := range fields.entries {
				if e.name == lowerName {
					stack[0] = 1
					return
				}
			}
			stack[0] = 0
		})

	case "[static]request.new":
		// Wasm signature: (headers: i32, body_opt_disc: i32, body_opt_val: i32, trailers: i32, options_opt_disc: i32, options_opt_val: i32, retPtr: i32) -> ()
		// Returns tuple<request, future, future> at retPtr.
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			headersHandle := uint32(stack[0])
			retPtrIdx := len(paramTypes) - 1
			retPtr := uint32(stack[retPtrIdx])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			immutableHeaders := h.resources.New(h.cloneFieldsImmutable(headersHandle))
			req := &requestResource{
				method:  "GET",
				headers: immutableHeaders,
			}
			reqHandle := h.resources.New(req)
			future1 := h.resources.New(&futureResource{ready: false})
			future2 := h.resources.New(&futureResource{ready: false})

			mem.WriteUint32Le(retPtr, reqHandle)
			mem.WriteUint32Le(retPtr+4, future1)
			mem.WriteUint32Le(retPtr+8, future2)
		})

	case "[method]request.get-method":
		// (self: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				return
			}
			req := res.(*requestResource)
			// Method is a variant: GET=0, HEAD=1, POST=2, PUT=3, DELETE=4, CONNECT=5, OPTIONS=6, TRACE=7, PATCH=8, other(string)=9
			switch req.method {
			case "GET":
				mem.WriteByte(retPtr, 0)
			case "HEAD":
				mem.WriteByte(retPtr, 1)
			case "POST":
				mem.WriteByte(retPtr, 2)
			case "PUT":
				mem.WriteByte(retPtr, 3)
			case "DELETE":
				mem.WriteByte(retPtr, 4)
			case "CONNECT":
				mem.WriteByte(retPtr, 5)
			case "OPTIONS":
				mem.WriteByte(retPtr, 6)
			case "TRACE":
				mem.WriteByte(retPtr, 7)
			case "PATCH":
				mem.WriteByte(retPtr, 8)
			default:
				mem.WriteByte(retPtr, 9) // other
				writeListToMemory(ctx, mod, retPtr+4, []byte(req.method))
			}
		})

	case "[method]request.set-method":
		// (self: i32, method_disc: i32, str_ptr: i32, str_len: i32) -> (i32)
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			methodDisc := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				if len(resultTypes) > 0 {
					stack[0] = 1
				}
				return
			}
			req := res.(*requestResource)

			methods := []string{"GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH"}
			if int(methodDisc) < len(methods) {
				req.method = methods[methodDisc]
			} else {
				ptr := uint32(stack[2])
				length := uint32(stack[3])
				data, _ := mem.Read(ptr, length)
				name := string(data)
				// Validate method name: must be a valid HTTP token (tchar).
				if !isValidFieldName(name) {
					if len(resultTypes) > 0 {
						stack[0] = 1 // error
					}
					return
				}
				// Normalize known method names.
				for _, m := range methods {
					if name == m {
						req.method = m
						if len(resultTypes) > 0 {
							stack[0] = 0
						}
						return
					}
				}
				req.method = name
			}

			if len(resultTypes) > 0 {
				stack[0] = 0 // ok
			}
		})

	case "[method]request.get-path-with-query":
		// (self: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 0) // none
				return
			}
			req := res.(*requestResource)
			if req.path == "" {
				mem.WriteByte(retPtr, 0) // none
			} else {
				mem.WriteByte(retPtr, 1) // some
				writeListToMemory(ctx, mod, retPtr+4, []byte(req.path))
			}
		})

	case "[method]request.set-path-with-query":
		// (self: i32, opt_disc: i32, str_ptr: i32, str_len: i32) -> (i32)
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			optDisc := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				if len(resultTypes) > 0 {
					stack[0] = 1
				}
				return
			}
			req := res.(*requestResource)
			if optDisc == 0 {
				req.path = ""
			} else {
				ptr := uint32(stack[2])
				length := uint32(stack[3])
				data, _ := mem.Read(ptr, length)
				s := string(data)
				if s == "" {
					s = "/"
				}
				if !isValidPath(s) {
					if len(resultTypes) > 0 {
						stack[0] = 1
					}
					return
				}
				req.path = s
			}
			if len(resultTypes) > 0 {
				stack[0] = 0 // ok
			}
		})

	case "[method]request.get-scheme":
		// (self: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 0) // none
				return
			}
			req := res.(*requestResource)
			if req.scheme == "" {
				mem.WriteByte(retPtr, 0) // none
			} else {
				mem.WriteByte(retPtr, 1) // some
				switch req.scheme {
				case "http":
					mem.WriteByte(retPtr+4, 0)
				case "https":
					mem.WriteByte(retPtr+4, 1)
				default:
					mem.WriteByte(retPtr+4, 2)
					writeListToMemory(ctx, mod, retPtr+8, []byte(req.scheme))
				}
			}
		})

	case "[method]request.set-scheme":
		// (self: i32, opt_disc: i32, scheme_disc: i32, str_ptr: i32, str_len: i32) -> (i32)
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			optDisc := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				if len(resultTypes) > 0 {
					stack[0] = 1
				}
				return
			}
			req := res.(*requestResource)
			if optDisc == 0 {
				req.scheme = ""
			} else {
				schemeDisc := uint32(stack[2])
				switch schemeDisc {
				case 0:
					req.scheme = "http"
				case 1:
					req.scheme = "https"
				default:
					ptr := uint32(stack[3])
					length := uint32(stack[4])
					data, _ := mem.Read(ptr, length)
					s := string(data)
					// Validate scheme per RFC 3986.
					if !isValidScheme(s) {
						if len(resultTypes) > 0 {
							stack[0] = 1
						}
						return
					}
					// Normalize to built-in variants.
					if s == "http" {
						req.scheme = "http"
					} else if s == "https" {
						req.scheme = "https"
					} else {
						req.scheme = s
					}
				}
			}
			if len(resultTypes) > 0 {
				stack[0] = 0 // ok
			}
		})

	case "[method]request.get-authority":
		// (self: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 0)
				return
			}
			req := res.(*requestResource)
			if req.authority == "" {
				mem.WriteByte(retPtr, 0)
			} else {
				mem.WriteByte(retPtr, 1)
				writeListToMemory(ctx, mod, retPtr+4, []byte(req.authority))
			}
		})

	case "[method]request.set-authority":
		// (self: i32, opt_disc: i32, str_ptr: i32, str_len: i32) -> (i32)
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			optDisc := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				if len(resultTypes) > 0 {
					stack[0] = 1
				}
				return
			}
			req := res.(*requestResource)
			if optDisc == 0 {
				req.authority = ""
			} else {
				ptr := uint32(stack[2])
				length := uint32(stack[3])
				data, _ := mem.Read(ptr, length)
				s := string(data)
				if !isValidAuthority(s) {
					if len(resultTypes) > 0 {
						stack[0] = 1
					}
					return
				}
				req.authority = s
			}
			if len(resultTypes) > 0 {
				stack[0] = 0 // ok
			}
		})

	case "[method]request.get-options":
		// (self: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			mem.WriteByte(retPtr, 0) // none
		})

	case "[method]request.get-headers":
		// (self: i32) -> (i32)
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			self := uint32(stack[0])
			res, ok := h.resources.Get(self)
			if !ok {
				stack[0] = 0
				return
			}
			req := res.(*requestResource)
			// Return a fresh immutable clone each time, since wasm will drop the handle.
			clone := h.cloneFieldsImmutable(req.headers)
			stack[0] = uint64(h.resources.New(clone))
		})

	case "[static]response.new":
		// Wasm signature: (headers: i32, opt_body_disc: i32, opt_body_val: i32, trailers: i32, retPtr: i32) -> ()
		// Returns tuple<response, future<sent-trailers>> at retPtr.
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			headersHandle := uint32(stack[0])
			retPtrIdx := len(paramTypes) - 1
			retPtr := uint32(stack[retPtrIdx])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			// Mark the headers as immutable by cloning them.
			immutableHeaders := h.resources.New(h.cloneFieldsImmutable(headersHandle))

			resp := &responseResource{
				statusCode: 200, // default
				headers:    immutableHeaders,
			}
			respHandle := h.resources.New(resp)
			future1 := h.resources.New(&futureResource{ready: false})

			mem.WriteUint32Le(retPtr, respHandle)
			mem.WriteUint32Le(retPtr+4, future1)
		})

	case "[method]response.get-status-code":
		// (self: i32) -> (i32)
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			self := uint32(stack[0])
			res, ok := h.resources.Get(self)
			if !ok {
				stack[0] = 0
				return
			}
			resp := res.(*responseResource)
			stack[0] = uint64(resp.statusCode)
		})

	case "[method]response.set-status-code":
		// (self: i32, code: i32) -> (i32)
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			self := uint32(stack[0])
			code := uint32(stack[1])
			res, ok := h.resources.Get(self)
			if !ok {
				if len(resultTypes) > 0 {
					stack[0] = 1
				}
				return
			}
			// Valid status codes: 100-599
			if code < 100 || code > 599 {
				if len(resultTypes) > 0 {
					stack[0] = 1 // error
				}
				return
			}
			resp := res.(*responseResource)
			resp.statusCode = code
			if len(resultTypes) > 0 {
				stack[0] = 0 // ok
			}
		})

	case "[method]response.get-headers":
		// (self: i32) -> (i32)
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			self := uint32(stack[0])
			res, ok := h.resources.Get(self)
			if !ok {
				stack[0] = 0
				return
			}
			resp := res.(*responseResource)
			// Return a fresh immutable clone each time, since wasm will drop the handle.
			clone := h.cloneFieldsImmutable(resp.headers)
			stack[0] = uint64(h.resources.New(clone))
		})

	case "[static]request.consume-body":
		// (self: i32, ..., retPtr: i32) -> ()
		// Returns result<tuple<stream<u8>, future<...>>, error-code> at retPtr.
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtrIdx := len(paramTypes) - 1
			retPtr := uint32(stack[retPtrIdx])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			// Ok result with stream + future handles.
			streamHandle := h.resources.New(&streamResource{})
			futureHandle := h.resources.New(&futureResource{ready: false})
			mem.WriteByte(retPtr, 0) // ok
			mem.WriteUint32Le(retPtr+4, streamHandle)
			mem.WriteUint32Le(retPtr+8, futureHandle)
		})
	}

	return nil
}
