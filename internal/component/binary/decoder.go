// Package binary implements the WebAssembly Component Model binary format decoder.
//
// The component binary format shares the same magic number as core modules but
// uses a different layer byte to distinguish components from modules.
//
// See https://github.com/WebAssembly/component-model/blob/main/design/mvp/Binary.md
package binary

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/tetratelabs/wazero/internal/component"
	"github.com/tetratelabs/wazero/internal/leb128"
)

// ComponentMagic is the 4 byte preamble (same as core modules: "\0asm").
var ComponentMagic = []byte{0x00, 0x61, 0x73, 0x6D}

// ComponentVersion is the version field for components.
// The layer byte (0x0d) distinguishes components from core modules (0x01).
var ComponentVersion = []byte{0x0d, 0x00, 0x01, 0x00}

// ErrInvalidMagicNumber means the binary doesn't start with the expected preamble.
var ErrInvalidMagicNumber = errors.New("invalid magic number")

// ErrInvalidVersion means the version/layer field doesn't match the expected component version.
var ErrInvalidVersion = errors.New("invalid component version")

// IsComponent checks whether the given binary is a WebAssembly component
// (as opposed to a core module) by inspecting the version/layer bytes.
func IsComponent(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	return bytes.Equal(data[:4], ComponentMagic) && bytes.Equal(data[4:8], ComponentVersion)
}

// Component section IDs in the binary format.
const (
	// Core sections (0x00-0x0b) reuse core module section IDs.
	sectionCoreModule    byte = 0x00
	sectionCoreInstance  byte = 0x01
	sectionCoreType      byte = 0x02

	// Component sections use higher IDs.
	sectionComponent         byte = 0x04
	sectionComponentInstance byte = 0x05
	sectionAlias             byte = 0x06
	sectionComponentType     byte = 0x07
	sectionCanon             byte = 0x08
	sectionComponentStart    byte = 0x09
	sectionImport            byte = 0x0a
	sectionExport            byte = 0x0b
)

// DecodeComponent decodes a WebAssembly component from its binary representation.
func DecodeComponent(data []byte) (*component.Component, error) {
	r := bytes.NewReader(data)

	// Magic number.
	buf := make([]byte, 4)
	if _, err := io.ReadFull(r, buf); err != nil || !bytes.Equal(buf, ComponentMagic) {
		return nil, ErrInvalidMagicNumber
	}

	// Version.
	if _, err := io.ReadFull(r, buf); err != nil || !bytes.Equal(buf, ComponentVersion) {
		return nil, ErrInvalidVersion
	}

	c := &component.Component{}
	for {
		sectionID, err := r.ReadByte()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("read section id: %w", err)
		}

		sectionSize, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return nil, fmt.Errorf("get size of section 0x%02x: %w", sectionID, err)
		}

		// Read the entire section payload to limit the reader.
		payload := make([]byte, sectionSize)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, fmt.Errorf("read section 0x%02x payload: %w", sectionID, err)
		}

		sr := bytes.NewReader(payload)

		switch sectionID {
		case sectionCoreModule:
			mod, err := decodeCoreModule(sr, sectionSize)
			if err != nil {
				return nil, fmt.Errorf("decode core module: %w", err)
			}
			c.CoreModules = append(c.CoreModules, *mod)

		case sectionCoreInstance:
			instances, err := decodeCoreInstances(sr)
			if err != nil {
				return nil, fmt.Errorf("decode core instances: %w", err)
			}
			c.CoreInstances = append(c.CoreInstances, instances...)

		case sectionComponentType:
			types, err := decodeComponentTypes(sr)
			if err != nil {
				return nil, fmt.Errorf("decode component types: %w", err)
			}
			c.ComponentTypes = append(c.ComponentTypes, types...)

		case sectionImport:
			imports, err := decodeImports(sr)
			if err != nil {
				return nil, fmt.Errorf("decode imports: %w", err)
			}
			c.Imports = append(c.Imports, imports...)

		case sectionExport:
			exports, err := decodeExports(sr)
			if err != nil {
				return nil, fmt.Errorf("decode exports: %w", err)
			}
			c.Exports = append(c.Exports, exports...)

		case sectionCanon:
			canons, err := decodeCanonicalFunctions(sr)
			if err != nil {
				return nil, fmt.Errorf("decode canonical functions: %w", err)
			}
			c.CanonicalFunctions = append(c.CanonicalFunctions, canons...)

		case sectionAlias:
			aliases, err := decodeAliases(sr)
			if err != nil {
				return nil, fmt.Errorf("decode aliases: %w", err)
			}
			c.Aliases = append(c.Aliases, aliases...)

		case sectionComponentInstance:
			instances, err := decodeComponentInstances(sr)
			if err != nil {
				return nil, fmt.Errorf("decode component instances: %w", err)
			}
			c.Instances = append(c.Instances, instances...)

		case sectionComponent:
			// Nested component: skip for now, store as custom data.
			c.CustomSections = append(c.CustomSections, component.CustomSection{
				Name: "component",
				Data: payload,
			})

		default:
			// Unknown or custom sections are stored but not decoded further.
			c.CustomSections = append(c.CustomSections, component.CustomSection{
				Name: fmt.Sprintf("section-0x%02x", sectionID),
				Data: payload,
			})
		}
	}

	return c, nil
}

func decodeCoreModule(r *bytes.Reader, size uint32) (*component.CoreModule, error) {
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("read core module data: %w", err)
	}
	return &component.CoreModule{Data: data}, nil
}

func decodeCoreInstances(r *bytes.Reader) ([]component.CoreInstance, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get count: %w", err)
	}

	result := make([]component.CoreInstance, count)
	for i := uint32(0); i < count; i++ {
		kind, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read instance kind: %w", err)
		}
		result[i].Kind = component.CoreInstanceKind(kind)

		switch result[i].Kind {
		case component.CoreInstanceKindInstantiate:
			idx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read module index: %w", err)
			}
			result[i].ModuleIndex = idx

			argCount, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read args count: %w", err)
			}
			result[i].Args = make([]component.InstantiationArg, argCount)
			for j := uint32(0); j < argCount; j++ {
				name, err := decodeName(r)
				if err != nil {
					return nil, fmt.Errorf("read arg name: %w", err)
				}
				result[i].Args[j].Name = name

				sortByte, err := r.ReadByte()
				if err != nil {
					return nil, fmt.Errorf("read arg sort: %w", err)
				}
				result[i].Args[j].Kind = component.ExternKind(sortByte)

				idx, _, err := leb128.DecodeUint32(r)
				if err != nil {
					return nil, fmt.Errorf("read arg index: %w", err)
				}
				result[i].Args[j].Index = idx
			}

		case component.CoreInstanceKindFromExports:
			exportCount, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read export count: %w", err)
			}
			result[i].Exports = make([]component.CoreExportItem, exportCount)
			for j := uint32(0); j < exportCount; j++ {
				name, err := decodeName(r)
				if err != nil {
					return nil, fmt.Errorf("read export name: %w", err)
				}
				result[i].Exports[j].Name = name

				kindByte, err := r.ReadByte()
				if err != nil {
					return nil, fmt.Errorf("read export kind: %w", err)
				}
				result[i].Exports[j].Kind = component.ExternKind(kindByte)

				idx, _, err := leb128.DecodeUint32(r)
				if err != nil {
					return nil, fmt.Errorf("read export index: %w", err)
				}
				result[i].Exports[j].Index = idx
			}
		}
	}

	return result, nil
}

func decodeComponentTypes(r *bytes.Reader) ([]component.ComponentType, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get count: %w", err)
	}

	result := make([]component.ComponentType, count)
	for i := uint32(0); i < count; i++ {
		typeByte, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read type kind byte: %w", err)
		}

		switch typeByte {
		case 0x40: // func type
			result[i].Kind = component.TypeKindFunc
			ft, err := decodeFuncType(r)
			if err != nil {
				return nil, fmt.Errorf("decode func type: %w", err)
			}
			result[i].Func = ft

		case 0x72: // record
			result[i].Kind = component.TypeKindRecord
			rt, err := decodeRecordType(r)
			if err != nil {
				return nil, fmt.Errorf("decode record type: %w", err)
			}
			result[i].Record = rt

		case 0x71: // variant
			result[i].Kind = component.TypeKindVariant
			vt, err := decodeVariantType(r)
			if err != nil {
				return nil, fmt.Errorf("decode variant type: %w", err)
			}
			result[i].Variant = vt

		case 0x70: // list
			result[i].Kind = component.TypeKindList
			lt, err := decodeListType(r)
			if err != nil {
				return nil, fmt.Errorf("decode list type: %w", err)
			}
			result[i].List = lt

		case 0x6f: // tuple
			result[i].Kind = component.TypeKindTuple
			tt, err := decodeTupleType(r)
			if err != nil {
				return nil, fmt.Errorf("decode tuple type: %w", err)
			}
			result[i].Tuple = tt

		case 0x6e: // flags
			result[i].Kind = component.TypeKindFlags
			ft, err := decodeFlagsType(r)
			if err != nil {
				return nil, fmt.Errorf("decode flags type: %w", err)
			}
			result[i].Flags = ft

		case 0x6d: // enum
			result[i].Kind = component.TypeKindEnum
			et, err := decodeEnumType(r)
			if err != nil {
				return nil, fmt.Errorf("decode enum type: %w", err)
			}
			result[i].Enum = et

		case 0x6b: // option
			result[i].Kind = component.TypeKindOption
			ot, err := decodeOptionType(r)
			if err != nil {
				return nil, fmt.Errorf("decode option type: %w", err)
			}
			result[i].Option = ot

		case 0x6a: // result
			result[i].Kind = component.TypeKindResult
			resType, err := decodeResultType(r)
			if err != nil {
				return nil, fmt.Errorf("decode result type: %w", err)
			}
			result[i].Result = resType

		case 0x69: // own
			result[i].Kind = component.TypeKindOwn
			idx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("decode own type index: %w", err)
			}
			result[i].Own = &component.OwnType{TypeIndex: idx}

		case 0x68: // borrow
			result[i].Kind = component.TypeKindBorrow
			idx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("decode borrow type index: %w", err)
			}
			result[i].Borrow = &component.BorrowType{TypeIndex: idx}

		default:
			// May be a primitive value type.
			if isPrimitiveValType(typeByte) {
				result[i].Kind = component.TypeKindPrimitive
				result[i].Primitive = component.PrimitiveValType(typeByte)
			} else {
				return nil, fmt.Errorf("unknown type byte 0x%02x at index %d", typeByte, i)
			}
		}
	}

	return result, nil
}

func isPrimitiveValType(b byte) bool {
	return b >= 0x73 && b <= 0x7f
}

func decodeFuncType(r *bytes.Reader) (*component.FuncType, error) {
	ft := &component.FuncType{}

	// Params.
	paramCount, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read param count: %w", err)
	}
	ft.Params = make([]component.NamedType, paramCount)
	for i := uint32(0); i < paramCount; i++ {
		name, err := decodeName(r)
		if err != nil {
			return nil, fmt.Errorf("read param name: %w", err)
		}
		ft.Params[i].Name = name

		vt, err := decodeValType(r)
		if err != nil {
			return nil, fmt.Errorf("read param type: %w", err)
		}
		ft.Params[i].Type = *vt
	}

	// Results.
	resultTag, err := r.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("read result tag: %w", err)
	}

	ft.Results = &component.FuncResult{}
	switch resultTag {
	case 0x00: // single unnamed result
		vt, err := decodeValType(r)
		if err != nil {
			return nil, fmt.Errorf("read single result type: %w", err)
		}
		ft.Results.Type = vt

	case 0x01: // named results
		resultCount, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return nil, fmt.Errorf("read result count: %w", err)
		}
		ft.Results.Named = make([]component.NamedType, resultCount)
		for i := uint32(0); i < resultCount; i++ {
			name, err := decodeName(r)
			if err != nil {
				return nil, fmt.Errorf("read result name: %w", err)
			}
			ft.Results.Named[i].Name = name

			vt, err := decodeValType(r)
			if err != nil {
				return nil, fmt.Errorf("read result type: %w", err)
			}
			ft.Results.Named[i].Type = *vt
		}
	}

	return ft, nil
}

func decodeValType(r *bytes.Reader) (*component.ValType, error) {
	b, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	if isPrimitiveValType(b) {
		p := component.PrimitiveValType(b)
		return &component.ValType{Primitive: &p}, nil
	}

	// Non-primitive: it's a type index encoded as LEB128.
	if err := r.UnreadByte(); err != nil {
		return nil, err
	}
	idx, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, err
	}
	return &component.ValType{TypeIndex: &idx}, nil
}

func decodeRecordType(r *bytes.Reader) (*component.RecordType, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, err
	}

	rt := &component.RecordType{Fields: make([]component.NamedType, count)}
	for i := uint32(0); i < count; i++ {
		name, err := decodeName(r)
		if err != nil {
			return nil, err
		}
		rt.Fields[i].Name = name

		vt, err := decodeValType(r)
		if err != nil {
			return nil, err
		}
		rt.Fields[i].Type = *vt
	}
	return rt, nil
}

func decodeVariantType(r *bytes.Reader) (*component.VariantType, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, err
	}

	vt := &component.VariantType{Cases: make([]component.VariantCase, count)}
	for i := uint32(0); i < count; i++ {
		name, err := decodeName(r)
		if err != nil {
			return nil, err
		}
		vt.Cases[i].Name = name

		hasPayload, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if hasPayload == 0x01 {
			valType, err := decodeValType(r)
			if err != nil {
				return nil, err
			}
			vt.Cases[i].Type = valType
		}

		hasRefines, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if hasRefines == 0x01 {
			idx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, err
			}
			vt.Cases[i].Refines = &idx
		}
	}
	return vt, nil
}

func decodeListType(r *bytes.Reader) (*component.ListType, error) {
	vt, err := decodeValType(r)
	if err != nil {
		return nil, err
	}
	return &component.ListType{ElementType: *vt}, nil
}

func decodeTupleType(r *bytes.Reader) (*component.TupleType, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, err
	}
	tt := &component.TupleType{Types: make([]component.ValType, count)}
	for i := uint32(0); i < count; i++ {
		vt, err := decodeValType(r)
		if err != nil {
			return nil, err
		}
		tt.Types[i] = *vt
	}
	return tt, nil
}

func decodeFlagsType(r *bytes.Reader) (*component.FlagsType, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, err
	}
	ft := &component.FlagsType{Names: make([]string, count)}
	for i := uint32(0); i < count; i++ {
		name, err := decodeName(r)
		if err != nil {
			return nil, err
		}
		ft.Names[i] = name
	}
	return ft, nil
}

func decodeEnumType(r *bytes.Reader) (*component.EnumType, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, err
	}
	et := &component.EnumType{Names: make([]string, count)}
	for i := uint32(0); i < count; i++ {
		name, err := decodeName(r)
		if err != nil {
			return nil, err
		}
		et.Names[i] = name
	}
	return et, nil
}

func decodeOptionType(r *bytes.Reader) (*component.OptionType, error) {
	vt, err := decodeValType(r)
	if err != nil {
		return nil, err
	}
	return &component.OptionType{Type: *vt}, nil
}

func decodeResultType(r *bytes.Reader) (*component.ResultType, error) {
	rt := &component.ResultType{}

	tag, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	switch tag {
	case 0x00: // both ok and error
		ok, err := decodeValType(r)
		if err != nil {
			return nil, err
		}
		rt.Ok = ok
		errType, err := decodeValType(r)
		if err != nil {
			return nil, err
		}
		rt.Err = errType

	case 0x01: // only ok
		ok, err := decodeValType(r)
		if err != nil {
			return nil, err
		}
		rt.Ok = ok

	case 0x02: // only error
		errType, err := decodeValType(r)
		if err != nil {
			return nil, err
		}
		rt.Err = errType

	case 0x03: // neither
		// No payload.
	}

	return rt, nil
}

func decodeImports(r *bytes.Reader) ([]component.Import, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get count: %w", err)
	}

	result := make([]component.Import, count)
	for i := uint32(0); i < count; i++ {
		name, err := decodeName(r)
		if err != nil {
			return nil, fmt.Errorf("read import name: %w", err)
		}
		result[i].Name = name

		kindByte, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read import kind: %w", err)
		}
		result[i].Kind = component.ExternKind(kindByte)

		idx, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return nil, fmt.Errorf("read import type index: %w", err)
		}
		result[i].TypeIndex = idx
	}
	return result, nil
}

func decodeExports(r *bytes.Reader) ([]component.Export, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get count: %w", err)
	}

	result := make([]component.Export, count)
	for i := uint32(0); i < count; i++ {
		name, err := decodeName(r)
		if err != nil {
			return nil, fmt.Errorf("read export name: %w", err)
		}
		result[i].Name = name

		kindByte, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read export kind: %w", err)
		}
		result[i].Kind = component.ExternKind(kindByte)

		idx, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return nil, fmt.Errorf("read export index: %w", err)
		}
		result[i].Index = idx

		// Optional type annotation.
		hasType, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read export type flag: %w", err)
		}
		if hasType == 0x01 {
			typeIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read export type index: %w", err)
			}
			result[i].TypeIndex = &typeIdx
		}
	}
	return result, nil
}

func decodeCanonicalFunctions(r *bytes.Reader) ([]component.CanonicalFunction, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get count: %w", err)
	}

	result := make([]component.CanonicalFunction, count)
	for i := uint32(0); i < count; i++ {
		kindByte, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read canon kind: %w", err)
		}
		result[i].Kind = component.CanonicalFunctionKind(kindByte)

		switch result[i].Kind {
		case component.CanonicalFunctionLift:
			coreFuncIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read lift core func index: %w", err)
			}
			result[i].CoreFuncIndex = coreFuncIdx

			opts, err := decodeCanonicalOptions(r)
			if err != nil {
				return nil, fmt.Errorf("decode lift options: %w", err)
			}
			result[i].Options = opts

			typeIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read lift type index: %w", err)
			}
			result[i].TypeIndex = typeIdx

		case component.CanonicalFunctionLower:
			funcIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read lower func index: %w", err)
			}
			result[i].CoreFuncIndex = funcIdx

			opts, err := decodeCanonicalOptions(r)
			if err != nil {
				return nil, fmt.Errorf("decode lower options: %w", err)
			}
			result[i].Options = opts

		case component.CanonicalFunctionResourceNew,
			component.CanonicalFunctionResourceDrop,
			component.CanonicalFunctionResourceRep:
			typeIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read resource type index: %w", err)
			}
			result[i].TypeIndex = typeIdx
		}
	}
	return result, nil
}

func decodeCanonicalOptions(r *bytes.Reader) ([]component.CanonicalOption, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, err
	}

	result := make([]component.CanonicalOption, count)
	for i := uint32(0); i < count; i++ {
		kindByte, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		result[i].Kind = component.CanonicalOptionKind(kindByte)

		switch result[i].Kind {
		case component.CanonicalOptionMemory, component.CanonicalOptionRealloc, component.CanonicalOptionPostReturn:
			val, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, err
			}
			result[i].Value = val
		}
		// UTF8, UTF16, Latin1 have no payload.
	}
	return result, nil
}

func decodeAliases(r *bytes.Reader) ([]component.Alias, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get count: %w", err)
	}

	result := make([]component.Alias, count)
	for i := uint32(0); i < count; i++ {
		kindByte, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read alias kind: %w", err)
		}
		result[i].Kind = component.AliasKind(kindByte)

		switch result[i].Kind {
		case component.AliasKindInstanceExport:
			externKindByte, err := r.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("read alias extern kind: %w", err)
			}
			result[i].Target.ExternKind = component.ExternKind(externKindByte)

			instIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read alias instance index: %w", err)
			}
			result[i].Target.InstanceIndex = instIdx

			name, err := decodeName(r)
			if err != nil {
				return nil, fmt.Errorf("read alias name: %w", err)
			}
			result[i].Target.Name = name

		case component.AliasKindOuter:
			externKindByte, err := r.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("read alias extern kind: %w", err)
			}
			result[i].Target.ExternKind = component.ExternKind(externKindByte)

			outerCount, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read alias outer count: %w", err)
			}
			result[i].Target.OuterCount = outerCount

			idx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read alias index: %w", err)
			}
			result[i].Target.Index = idx
		}
	}
	return result, nil
}

func decodeComponentInstances(r *bytes.Reader) ([]component.Instance, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("get count: %w", err)
	}

	result := make([]component.Instance, count)
	for i := uint32(0); i < count; i++ {
		kindByte, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read instance kind: %w", err)
		}
		result[i].Kind = component.InstanceKind(kindByte)

		switch result[i].Kind {
		case component.InstanceKindInstantiate:
			compIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read component index: %w", err)
			}
			result[i].ComponentIndex = compIdx

			argCount, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read args count: %w", err)
			}
			result[i].Args = make([]component.InstantiationArg, argCount)
			for j := uint32(0); j < argCount; j++ {
				name, err := decodeName(r)
				if err != nil {
					return nil, fmt.Errorf("read arg name: %w", err)
				}
				result[i].Args[j].Name = name

				sortByte, err := r.ReadByte()
				if err != nil {
					return nil, fmt.Errorf("read arg sort: %w", err)
				}
				result[i].Args[j].Kind = component.ExternKind(sortByte)

				idx, _, err := leb128.DecodeUint32(r)
				if err != nil {
					return nil, fmt.Errorf("read arg index: %w", err)
				}
				result[i].Args[j].Index = idx
			}

		case component.InstanceKindFromExports:
			exportCount, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read export count: %w", err)
			}
			result[i].Exports = make([]component.Export, exportCount)
			for j := uint32(0); j < exportCount; j++ {
				name, err := decodeName(r)
				if err != nil {
					return nil, fmt.Errorf("read export name: %w", err)
				}
				result[i].Exports[j].Name = name

				kindByte, err := r.ReadByte()
				if err != nil {
					return nil, fmt.Errorf("read export kind: %w", err)
				}
				result[i].Exports[j].Kind = component.ExternKind(kindByte)

				idx, _, err := leb128.DecodeUint32(r)
				if err != nil {
					return nil, fmt.Errorf("read export index: %w", err)
				}
				result[i].Exports[j].Index = idx
			}
		}
	}
	return result, nil
}

func decodeName(r *bytes.Reader) (string, error) {
	nameLen, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return "", err
	}
	nameBytes := make([]byte, nameLen)
	if _, err := io.ReadFull(r, nameBytes); err != nil {
		return "", err
	}
	return string(nameBytes), nil
}
