// Package canon implements the WebAssembly Component Model Canonical ABI.
// The canonical ABI defines how component-level types (strings, lists, records, etc.)
// are lowered into core WebAssembly linear memory and lifted back out.
//
// See https://github.com/WebAssembly/component-model/blob/main/design/mvp/CanonicalABI.md
package canon

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/tetratelabs/wazero/internal/wasm/component"
)

// StringEncoding represents how strings are encoded in linear memory.
type StringEncoding byte

const (
	StringEncodingUTF8    StringEncoding = 0
	StringEncodingUTF16   StringEncoding = 1
	StringEncodingLatin1  StringEncoding = 2
)

// Alignment returns the byte alignment required for a component type in linear memory.
func Alignment(t *component.ComponentType) uint32 {
	switch t.Kind {
	case component.ComponentTypeKindPrimitive:
		return primitiveAlignment(t.Primitive)
	case component.ComponentTypeKindRecord:
		return recordAlignment(t.Record)
	case component.ComponentTypeKindVariant:
		return variantAlignment(t.Variant)
	case component.ComponentTypeKindList:
		return 4 // pointer + length, both i32
	case component.ComponentTypeKindTuple:
		return tupleAlignment(t.Tuple)
	case component.ComponentTypeKindFlags:
		return flagsAlignment(t.Flags)
	case component.ComponentTypeKindEnum:
		return enumAlignment(t.Enum)
	case component.ComponentTypeKindOption:
		return optionAlignment(t.Option)
	case component.ComponentTypeKindResult:
		return resultAlignment(t.Result)
	case component.ComponentTypeKindOwn, component.ComponentTypeKindBorrow:
		return 4 // handle is i32
	case component.ComponentTypeKindStream, component.ComponentTypeKindFuture:
		return 4 // handle is i32
	default:
		return 1
	}
}

// Size returns the byte size of a component type in linear memory.
func Size(t *component.ComponentType) uint32 {
	switch t.Kind {
	case component.ComponentTypeKindPrimitive:
		return primitiveSize(t.Primitive)
	case component.ComponentTypeKindRecord:
		return recordSize(t.Record)
	case component.ComponentTypeKindVariant:
		return variantSize(t.Variant)
	case component.ComponentTypeKindList:
		return 8 // pointer (i32) + length (i32)
	case component.ComponentTypeKindTuple:
		return tupleSize(t.Tuple)
	case component.ComponentTypeKindFlags:
		return flagsSize(t.Flags)
	case component.ComponentTypeKindEnum:
		return enumSize(t.Enum)
	case component.ComponentTypeKindOption:
		return optionSize(t.Option)
	case component.ComponentTypeKindResult:
		return resultSize(t.Result)
	case component.ComponentTypeKindOwn, component.ComponentTypeKindBorrow:
		return 4 // handle is i32
	case component.ComponentTypeKindStream, component.ComponentTypeKindFuture:
		return 4 // handle is i32
	default:
		return 0
	}
}

// CoreType returns the core wasm types that a component type lowers to.
func CoreType(t *component.ComponentType) []byte {
	switch t.Kind {
	case component.ComponentTypeKindPrimitive:
		return primitiveCoreType(t.Primitive)
	case component.ComponentTypeKindList:
		return []byte{0x7f, 0x7f} // i32 pointer, i32 length
	case component.ComponentTypeKindOwn, component.ComponentTypeKindBorrow:
		return []byte{0x7f} // i32 handle
	case component.ComponentTypeKindEnum:
		return []byte{0x7f} // i32 discriminant
	case component.ComponentTypeKindFlags:
		n := flagsSize(t.Flags)
		count := (n + 3) / 4 // number of i32s needed
		result := make([]byte, count)
		for i := range result {
			result[i] = 0x7f // i32
		}
		return result
	default:
		// Complex types are passed via pointer to linear memory.
		return []byte{0x7f} // i32 pointer
	}
}

func primitiveAlignment(v component.ValType) uint32 {
	switch v {
	case component.ValTypeBool, component.ValTypeS8, component.ValTypeU8:
		return 1
	case component.ValTypeS16, component.ValTypeU16:
		return 2
	case component.ValTypeS32, component.ValTypeU32, component.ValTypeF32, component.ValTypeChar:
		return 4
	case component.ValTypeS64, component.ValTypeU64, component.ValTypeF64:
		return 8
	case component.ValTypeString:
		return 4 // pointer + length, both i32
	default:
		return 1
	}
}

func primitiveSize(v component.ValType) uint32 {
	switch v {
	case component.ValTypeBool, component.ValTypeS8, component.ValTypeU8:
		return 1
	case component.ValTypeS16, component.ValTypeU16:
		return 2
	case component.ValTypeS32, component.ValTypeU32, component.ValTypeF32, component.ValTypeChar:
		return 4
	case component.ValTypeS64, component.ValTypeU64, component.ValTypeF64:
		return 8
	case component.ValTypeString:
		return 8 // pointer (i32) + length (i32)
	default:
		return 0
	}
}

func primitiveCoreType(v component.ValType) []byte {
	switch v {
	case component.ValTypeBool, component.ValTypeS8, component.ValTypeU8,
		component.ValTypeS16, component.ValTypeU16,
		component.ValTypeS32, component.ValTypeU32, component.ValTypeChar:
		return []byte{0x7f} // i32
	case component.ValTypeS64, component.ValTypeU64:
		return []byte{0x7e} // i64
	case component.ValTypeF32:
		return []byte{0x7d} // f32
	case component.ValTypeF64:
		return []byte{0x7c} // f64
	case component.ValTypeString:
		return []byte{0x7f, 0x7f} // i32 pointer, i32 length
	default:
		return nil
	}
}

func recordAlignment(r *component.RecordType) uint32 {
	maxAlign := uint32(1)
	for _, f := range r.Fields {
		a := Alignment(&f.Type)
		if a > maxAlign {
			maxAlign = a
		}
	}
	return maxAlign
}

func recordSize(r *component.RecordType) uint32 {
	offset := uint32(0)
	for _, f := range r.Fields {
		offset = alignTo(offset, Alignment(&f.Type))
		offset += Size(&f.Type)
	}
	return alignTo(offset, recordAlignment(r))
}

func variantAlignment(v *component.VariantType) uint32 {
	maxAlign := discriminantAlignment(len(v.Cases))
	for _, c := range v.Cases {
		if c.Type != nil {
			a := Alignment(c.Type)
			if a > maxAlign {
				maxAlign = a
			}
		}
	}
	return maxAlign
}

func variantSize(v *component.VariantType) uint32 {
	discSize := discriminantSize(len(v.Cases))
	maxPayloadSize := uint32(0)
	for _, c := range v.Cases {
		if c.Type != nil {
			s := Size(c.Type)
			if s > maxPayloadSize {
				maxPayloadSize = s
			}
		}
	}
	align := variantAlignment(v)
	return alignTo(alignTo(discSize, align) + maxPayloadSize, align)
}

func tupleAlignment(t *component.TupleType) uint32 {
	maxAlign := uint32(1)
	for _, ct := range t.Types {
		a := Alignment(&ct)
		if a > maxAlign {
			maxAlign = a
		}
	}
	return maxAlign
}

func tupleSize(t *component.TupleType) uint32 {
	offset := uint32(0)
	for _, ct := range t.Types {
		offset = alignTo(offset, Alignment(&ct))
		offset += Size(&ct)
	}
	return alignTo(offset, tupleAlignment(t))
}

func flagsAlignment(f *component.FlagsType) uint32 {
	n := len(f.Names)
	switch {
	case n == 0:
		return 1
	case n <= 8:
		return 1
	case n <= 16:
		return 2
	default:
		return 4
	}
}

func flagsSize(f *component.FlagsType) uint32 {
	n := len(f.Names)
	switch {
	case n == 0:
		return 0
	case n <= 8:
		return 1
	case n <= 16:
		return 2
	default:
		return 4 * uint32((n+31)/32)
	}
}

func enumAlignment(e *component.EnumType) uint32 {
	return discriminantAlignment(len(e.Names))
}

func enumSize(e *component.EnumType) uint32 {
	return discriminantSize(len(e.Names))
}

func optionAlignment(o *component.OptionType) uint32 {
	a := Alignment(&o.Type)
	if a < 1 {
		return 1
	}
	return a
}

func optionSize(o *component.OptionType) uint32 {
	// option<T> is like variant { none, some(T) }
	payloadSize := Size(&o.Type)
	payloadAlign := Alignment(&o.Type)
	return alignTo(alignTo(1, payloadAlign)+payloadSize, optionAlignment(o))
}

func resultAlignment(r *component.ResultType) uint32 {
	maxAlign := uint32(1)
	if r.Ok != nil {
		a := Alignment(r.Ok)
		if a > maxAlign {
			maxAlign = a
		}
	}
	if r.Err != nil {
		a := Alignment(r.Err)
		if a > maxAlign {
			maxAlign = a
		}
	}
	return maxAlign
}

func resultSize(r *component.ResultType) uint32 {
	maxPayload := uint32(0)
	if r.Ok != nil {
		s := Size(r.Ok)
		if s > maxPayload {
			maxPayload = s
		}
	}
	if r.Err != nil {
		s := Size(r.Err)
		if s > maxPayload {
			maxPayload = s
		}
	}
	align := resultAlignment(r)
	return alignTo(alignTo(1, align)+maxPayload, align)
}

func discriminantAlignment(cases int) uint32 {
	switch {
	case cases <= 0xff:
		return 1
	case cases <= 0xffff:
		return 2
	default:
		return 4
	}
}

func discriminantSize(cases int) uint32 {
	switch {
	case cases <= 0xff:
		return 1
	case cases <= 0xffff:
		return 2
	default:
		return 4
	}
}

func alignTo(offset, align uint32) uint32 {
	return (offset + align - 1) &^ (align - 1)
}

// LiftString reads a string from linear memory at the given pointer and length.
func LiftString(mem []byte, ptr, length uint32, encoding StringEncoding) (string, error) {
	switch encoding {
	case StringEncodingUTF8:
		if uint64(ptr)+uint64(length) > uint64(len(mem)) {
			return "", errors.New("string out of bounds")
		}
		data := mem[ptr : ptr+length]
		if !utf8.Valid(data) {
			return "", errors.New("invalid UTF-8 string")
		}
		return string(data), nil

	case StringEncodingUTF16:
		byteLen := length * 2
		if uint64(ptr)+uint64(byteLen) > uint64(len(mem)) {
			return "", errors.New("string out of bounds")
		}
		runes := make([]rune, 0, length)
		for i := uint32(0); i < length; i++ {
			code := binary.LittleEndian.Uint16(mem[ptr+i*2:])
			runes = append(runes, rune(code))
		}
		return string(runes), nil

	case StringEncodingLatin1:
		if uint64(ptr)+uint64(length) > uint64(len(mem)) {
			return "", errors.New("string out of bounds")
		}
		runes := make([]rune, length)
		for i := uint32(0); i < length; i++ {
			runes[i] = rune(mem[ptr+i])
		}
		return string(runes), nil

	default:
		return "", fmt.Errorf("unknown string encoding: %d", encoding)
	}
}

// LowerString writes a string to linear memory, returning the pointer and length.
// The caller must provide a realloc function to allocate memory.
func LowerString(s string, encoding StringEncoding) (data []byte, err error) {
	switch encoding {
	case StringEncodingUTF8:
		return []byte(s), nil

	case StringEncodingUTF16:
		runes := []rune(s)
		buf := make([]byte, len(runes)*2)
		for i, r := range runes {
			binary.LittleEndian.PutUint16(buf[i*2:], uint16(r))
		}
		return buf, nil

	case StringEncodingLatin1:
		buf := make([]byte, len(s))
		for i, r := range s {
			if r > 0xff {
				return nil, fmt.Errorf("character U+%04X cannot be encoded as Latin-1", r)
			}
			buf[i] = byte(r)
		}
		return buf, nil

	default:
		return nil, fmt.Errorf("unknown string encoding: %d", encoding)
	}
}

// LiftBool reads a bool from an i32 value.
func LiftBool(v uint32) bool {
	return v != 0
}

// LowerBool converts a bool to an i32 value.
func LowerBool(v bool) uint32 {
	if v {
		return 1
	}
	return 0
}

// LiftChar validates and returns a Unicode code point from an i32 value.
func LiftChar(v uint32) (rune, error) {
	if v > 0x10FFFF || (v >= 0xD800 && v <= 0xDFFF) {
		return 0, fmt.Errorf("invalid Unicode code point: U+%04X", v)
	}
	return rune(v), nil
}

// LowerChar converts a rune to an i32 value.
func LowerChar(r rune) uint32 {
	return uint32(r)
}

// LiftF32 converts i32 bits to a float32 value, validating it's not a NaN with payload.
func LiftF32(bits uint32) float32 {
	f := math.Float32frombits(bits)
	if math.IsNaN(float64(f)) {
		return float32(math.NaN())
	}
	return f
}

// LiftF64 converts i64 bits to a float64 value, validating it's not a NaN with payload.
func LiftF64(bits uint64) float64 {
	f := math.Float64frombits(bits)
	if math.IsNaN(f) {
		return math.NaN()
	}
	return f
}

// LowerF32 converts a float32 to i32 bits, canonicalizing NaN.
func LowerF32(f float32) uint32 {
	if math.IsNaN(float64(f)) {
		return 0x7fc00000 // canonical NaN
	}
	return math.Float32bits(f)
}

// LowerF64 converts a float64 to i64 bits, canonicalizing NaN.
func LowerF64(f float64) uint64 {
	if math.IsNaN(f) {
		return 0x7ff8000000000000 // canonical NaN
	}
	return math.Float64bits(f)
}

// LiftList reads a list from linear memory at the given pointer and length.
func LiftList(mem []byte, ptr, length uint32, elemType *component.ComponentType) ([]byte, error) {
	elemSize := Size(elemType)
	totalSize := uint64(length) * uint64(elemSize)
	if uint64(ptr)+totalSize > uint64(len(mem)) {
		return nil, errors.New("list out of bounds")
	}
	result := make([]byte, totalSize)
	copy(result, mem[ptr:uint64(ptr)+totalSize])
	return result, nil
}
