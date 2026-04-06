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
// See https://github.com/WebAssembly/component-model/blob/main/design/mvp/Binary.md
const (
	sectionCustom            byte = 0x00
	sectionCoreModule        byte = 0x01
	sectionCoreInstance      byte = 0x02
	sectionCoreType          byte = 0x03
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
		case sectionCustom:
			// Custom sections: read name and store.
			name, err := decodeName(sr)
			if err != nil {
				name = fmt.Sprintf("custom-0x%02x", sectionID)
			}
			remaining := make([]byte, sr.Len())
			_, _ = io.ReadFull(sr, remaining)
			c.CustomSections = append(c.CustomSections, component.CustomSection{
				Name: name,
				Data: remaining,
			})

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

		case sectionCoreType:
			// Core type sections: skip for now.

		case sectionComponent:
			// Nested component: decode recursively.
			nested, err := DecodeComponent(payload)
			if err != nil {
				// Store as raw data if we can't decode.
				c.CustomSections = append(c.CustomSections, component.CustomSection{
					Name: "component",
					Data: payload,
				})
			} else {
				c.NestedComponents = append(c.NestedComponents, *nested)
			}

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
		case 0x40, 0x43: // func type (0x40 = sync, 0x43 = async)
			result[i].Kind = component.TypeKindFunc
			ft, err := decodeFuncType(r)
			if err != nil {
				return nil, fmt.Errorf("decode func type: %w", err)
			}
			result[i].Func = ft

		case 0x41: // defined value type (core type definition within component type section)
			result[i].Kind = component.TypeKindPrimitive
			if err := skipCoreType(r); err != nil {
				return nil, fmt.Errorf("skip core type: %w", err)
			}

		case 0x42: // component type or instance type
			result[i].Kind = component.TypeKindInstance
			it, err := decodeInstanceType(r)
			if err != nil {
				return nil, fmt.Errorf("decode instance type: %w", err)
			}
			result[i].Instance = it

		case 0x44: // component type definition
			result[i].Kind = component.TypeKindComponent
			if err := skipComponentTypeDecl(r); err != nil {
				return nil, fmt.Errorf("skip component type decl: %w", err)
			}

		case 0x3f: // resource type
			result[i].Kind = component.TypeKindResource
			rt, err := decodeResourceType(r)
			if err != nil {
				return nil, fmt.Errorf("decode resource type: %w", err)
			}
			result[i].Resource = rt

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

		case 0x67: // fixed-length-list
			result[i].Kind = component.TypeKindList
			lt, err := decodeListType(r)
			if err != nil {
				return nil, fmt.Errorf("decode fixed-length-list type: %w", err)
			}
			result[i].List = lt
			// Skip the fixed length u32
			if _, _, err := leb128.DecodeUint32(r); err != nil {
				return nil, fmt.Errorf("decode fixed-length-list length: %w", err)
			}

		case 0x66: // stream
			result[i].Kind = component.TypeKindPrimitive // reuse for now
			if err := skipOptionalValType(r); err != nil {
				return nil, fmt.Errorf("decode stream type: %w", err)
			}

		case 0x65: // future
			result[i].Kind = component.TypeKindPrimitive // reuse for now
			if err := skipOptionalValType(r); err != nil {
				return nil, fmt.Errorf("decode future type: %w", err)
			}

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

	// Ok type: 0x00 = none, 0x01 = present (followed by valtype)
	okFlag, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	if okFlag == 0x01 {
		ok, err := decodeValType(r)
		if err != nil {
			return nil, err
		}
		rt.Ok = ok
	}

	// Err type: 0x00 = none, 0x01 = present (followed by valtype)
	errFlag, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	if errFlag == 0x01 {
		errType, err := decodeValType(r)
		if err != nil {
			return nil, err
		}
		rt.Err = errType
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
		name, err := decodeExternName(r)
		if err != nil {
			return nil, fmt.Errorf("read import name: %w", err)
		}
		result[i].Name = name

		if err := skipExternDesc(r); err != nil {
			return nil, fmt.Errorf("skip import desc: %w", err)
		}
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
		name, err := decodeExternName(r)
		if err != nil {
			return nil, fmt.Errorf("read export name: %w", err)
		}
		result[i].Name = name

		kind, err := decodeExternKind(r)
		if err != nil {
			return nil, fmt.Errorf("read export kind: %w", err)
		}
		result[i].Kind = kind

		idx, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return nil, fmt.Errorf("read export index: %w", err)
		}
		result[i].Index = idx

		// Optional type annotation: 0x00 = None, 0x01 = present.
		flag, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read export type flag: %w", err)
		}
		if flag == 0x01 {
			if err := skipExternDesc(r); err != nil {
				return nil, fmt.Errorf("skip export type annotation: %w", err)
			}
		}
		// 0x00 = no type annotation
	}
	return result, nil
}

// decodeExternKind reads a ComponentExternalKind.
// 0x00 0x11 = Module, 0x01 = Func, 0x02 = Value, 0x03 = Type,
// 0x04 = Component, 0x05 = Instance.
func decodeExternKind(r *bytes.Reader) (component.ExternKind, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	if b == 0x00 {
		sub, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		_ = sub // should be 0x11 for module
		return component.ExternKindCoreModule, nil
	}
	return component.ExternKind(b), nil
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
			// Lift: 0x00 0x00 core_func_idx opts type_idx
			if _, err := r.ReadByte(); err != nil { // sub-kind 0x00
				return nil, fmt.Errorf("read lift sub-kind: %w", err)
			}
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
			// Lower: 0x01 0x00 func_idx opts
			if _, err := r.ReadByte(); err != nil { // sub-kind 0x00
				return nil, fmt.Errorf("read lower sub-kind: %w", err)
			}
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

		case component.CanonicalFunctionKind(0x05): // task.cancel
			// No payload.

		case component.CanonicalFunctionKind(0x06): // subtask.cancel
			if _, err := r.ReadByte(); err != nil { // async flag
				return nil, err
			}

		case component.CanonicalFunctionKind(0x07): // resource.drop-async
			if _, _, err := leb128.DecodeUint32(r); err != nil { // resource type index
				return nil, err
			}

		case component.CanonicalFunctionKind(0x09): // task.return
			// Result type uses read_resultlist encoding:
			// 0x00 valtype → Some(result), 0x01 0x00 → None
			if err := skipResultList(r); err != nil {
				return nil, fmt.Errorf("skip task.return result: %w", err)
			}
			if _, err := decodeCanonicalOptions(r); err != nil {
				return nil, fmt.Errorf("skip task.return options: %w", err)
			}

		case component.CanonicalFunctionKind(0x0a), // context.get
			component.CanonicalFunctionKind(0x0b): // context.set
			// core:valtype + index
			if _, err := r.ReadByte(); err != nil { // core valtype
				return nil, err
			}
			if _, _, err := leb128.DecodeUint32(r); err != nil { // index
				return nil, err
			}

		case component.CanonicalFunctionKind(0x0c): // yield
			if _, err := r.ReadByte(); err != nil { // async flag
				return nil, err
			}

		case component.CanonicalFunctionKind(0x0d): // subtask.drop
			// No payload.

		case component.CanonicalFunctionKind(0x0e): // stream.new
			if _, _, err := leb128.DecodeUint32(r); err != nil { // type index
				return nil, err
			}

		case component.CanonicalFunctionKind(0x0f): // stream.read
			if _, _, err := leb128.DecodeUint32(r); err != nil { // type index
				return nil, err
			}
			if _, err := decodeCanonicalOptions(r); err != nil {
				return nil, err
			}

		case component.CanonicalFunctionKind(0x10): // stream.write
			if _, _, err := leb128.DecodeUint32(r); err != nil { // type index
				return nil, err
			}
			if _, err := decodeCanonicalOptions(r); err != nil {
				return nil, err
			}

		case component.CanonicalFunctionKind(0x11): // stream.cancel-read
			if _, _, err := leb128.DecodeUint32(r); err != nil { // type index
				return nil, err
			}
			if _, err := r.ReadByte(); err != nil { // async flag
				return nil, err
			}

		case component.CanonicalFunctionKind(0x12): // stream.cancel-write
			if _, _, err := leb128.DecodeUint32(r); err != nil { // type index
				return nil, err
			}
			if _, err := r.ReadByte(); err != nil { // async flag
				return nil, err
			}

		case component.CanonicalFunctionKind(0x13): // stream.close-readable
			if _, _, err := leb128.DecodeUint32(r); err != nil { // type index
				return nil, err
			}

		case component.CanonicalFunctionKind(0x14): // stream.close-writable
			if _, _, err := leb128.DecodeUint32(r); err != nil { // type index
				return nil, err
			}

		case component.CanonicalFunctionKind(0x15): // future.new
			if _, _, err := leb128.DecodeUint32(r); err != nil { // type index
				return nil, err
			}

		case component.CanonicalFunctionKind(0x16): // future.read
			if _, _, err := leb128.DecodeUint32(r); err != nil { // type index
				return nil, err
			}
			if _, err := decodeCanonicalOptions(r); err != nil {
				return nil, err
			}

		case component.CanonicalFunctionKind(0x17): // future.write
			if _, _, err := leb128.DecodeUint32(r); err != nil { // type index
				return nil, err
			}
			if _, err := decodeCanonicalOptions(r); err != nil {
				return nil, err
			}

		case component.CanonicalFunctionKind(0x18): // future.cancel-read
			if _, _, err := leb128.DecodeUint32(r); err != nil { // type index
				return nil, err
			}
			if _, err := r.ReadByte(); err != nil { // async flag
				return nil, err
			}

		case component.CanonicalFunctionKind(0x19): // future.cancel-write
			if _, _, err := leb128.DecodeUint32(r); err != nil { // type index
				return nil, err
			}
			if _, err := r.ReadByte(); err != nil { // async flag
				return nil, err
			}

		case component.CanonicalFunctionKind(0x1a): // future.close-readable
			if _, _, err := leb128.DecodeUint32(r); err != nil { // type index
				return nil, err
			}

		case component.CanonicalFunctionKind(0x1b): // future.close-writable
			if _, _, err := leb128.DecodeUint32(r); err != nil { // type index
				return nil, err
			}

		case component.CanonicalFunctionKind(0x1c): // error-context.new
			if _, err := decodeCanonicalOptions(r); err != nil {
				return nil, err
			}

		case component.CanonicalFunctionKind(0x1d): // error-context.debug-message
			if _, err := decodeCanonicalOptions(r); err != nil {
				return nil, err
			}

		case component.CanonicalFunctionKind(0x1e): // error-context.drop
			// No payload.

		case component.CanonicalFunctionKind(0x1f): // waitable-set.new
			// No payload.

		case component.CanonicalFunctionKind(0x20): // waitable-set.wait
			if _, err := r.ReadByte(); err != nil { // async flag
				return nil, err
			}
			if _, _, err := leb128.DecodeUint32(r); err != nil { // memory
				return nil, err
			}

		case component.CanonicalFunctionKind(0x21): // waitable-set.poll
			if _, err := r.ReadByte(); err != nil { // async flag
				return nil, err
			}
			if _, _, err := leb128.DecodeUint32(r); err != nil { // memory
				return nil, err
			}

		case component.CanonicalFunctionKind(0x22): // waitable-set.drop
			// No payload.

		case component.CanonicalFunctionKind(0x23): // waitable.join
			// No payload.

		case component.CanonicalFunctionKind(0x24): // backpressure.inc
			// No payload.

		case component.CanonicalFunctionKind(0x25): // backpressure.dec
			// No payload.

		case component.CanonicalFunctionKind(0x26): // thread.index
			// No payload.

		case component.CanonicalFunctionKind(0x27): // thread.new-indirect
			if _, _, err := leb128.DecodeUint32(r); err != nil { // func_ty_index
				return nil, err
			}
			if _, _, err := leb128.DecodeUint32(r); err != nil { // table_index
				return nil, err
			}

		case component.CanonicalFunctionKind(0x28): // thread.suspend-to-suspended
			if _, err := r.ReadByte(); err != nil { // cancellable
				return nil, err
			}

		case component.CanonicalFunctionKind(0x29): // thread.suspend
			if _, err := r.ReadByte(); err != nil { // cancellable
				return nil, err
			}

		case component.CanonicalFunctionKind(0x2a): // thread.unsuspend
			// No payload.

		case component.CanonicalFunctionKind(0x2b): // thread.yield-to-suspended
			if _, err := r.ReadByte(); err != nil { // cancellable
				return nil, err
			}

		case component.CanonicalFunctionKind(0x2c): // thread.suspend-to
			if _, err := r.ReadByte(); err != nil { // cancellable
				return nil, err
			}

		case component.CanonicalFunctionKind(0x40): // thread.spawn-ref
			if _, _, err := leb128.DecodeUint32(r); err != nil { // func_ty_index
				return nil, err
			}

		case component.CanonicalFunctionKind(0x41): // thread.spawn-indirect
			if _, _, err := leb128.DecodeUint32(r); err != nil { // func_ty_index
				return nil, err
			}
			if _, _, err := leb128.DecodeUint32(r); err != nil { // table_index
				return nil, err
			}

		case component.CanonicalFunctionKind(0x42): // thread.available-parallelism
			// No payload.

		default:
			return nil, fmt.Errorf("unknown canonical function kind 0x%02x", kindByte)
		}
	}
	return result, nil
}

// skipOptionalValType skips an optional valtype (0x00 = none, 0x01 valtype = some).
func skipOptionalValType(r *bytes.Reader) error {
	flag, err := r.ReadByte()
	if err != nil {
		return err
	}
	if flag == 0x01 {
		if _, err := decodeValType(r); err != nil {
			return err
		}
	}
	return nil
}

// skipResultList skips a result list encoding:
//
//	0x00 valtype → Some(result)
//	0x01 0x00   → None
func skipResultList(r *bytes.Reader) error {
	flag, err := r.ReadByte()
	if err != nil {
		return err
	}
	switch flag {
	case 0x00: // one result type present
		if _, err := decodeValType(r); err != nil {
			return err
		}
	case 0x01: // no result
		if _, err := r.ReadByte(); err != nil { // consume 0x00
			return err
		}
	default:
		return fmt.Errorf("invalid result list flag 0x%02x", flag)
	}
	return nil
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
		case component.CanonicalOptionKind(0x06): // async
			// No payload.
		case component.CanonicalOptionKind(0x07): // callback
			val, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, err
			}
			result[i].Value = val
		case component.CanonicalOptionKind(0x08): // core-type
			val, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, err
			}
			result[i].Value = val
		case component.CanonicalOptionKind(0x09): // gc
			// No payload.
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
		// Alias format: sort target
		// sort: 0x00 <core_sort> for core sorts, or single byte for component sorts
		// target: 0x00 = instance-export, 0x01 = core-instance-export, 0x02 = outer
		sortByte, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read alias sort: %w", err)
		}
		if sortByte == 0x00 {
			// Core sort: read the second byte
			coreSortByte, err := r.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("read alias core sort: %w", err)
			}
			result[i].Target.ExternKind = component.ExternKind(coreSortByte)
		} else {
			result[i].Target.ExternKind = component.ExternKind(sortByte)
		}

		targetByte, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read alias target kind: %w", err)
		}
		result[i].Kind = component.AliasKind(targetByte)

		switch result[i].Kind {
		case component.AliasKindInstanceExport:
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

		case component.AliasKindCoreInstanceExport:
			instIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read core alias instance index: %w", err)
			}
			result[i].Target.InstanceIndex = instIdx

			name, err := decodeName(r)
			if err != nil {
				return nil, fmt.Errorf("read core alias name: %w", err)
			}
			result[i].Target.Name = name
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

// decodeInstanceType decodes a component instance type definition.
// An instance type is a sequence of instance type declarations (exports).
func decodeInstanceType(r *bytes.Reader) (*component.InstanceType, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read decl count: %w", err)
	}

	it := &component.InstanceType{}
	for i := uint32(0); i < count; i++ {
		declByte, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read decl byte: %w", err)
		}
		switch declByte {
		case 0x00: // core type
			if err := skipCoreType(r); err != nil {
				return nil, fmt.Errorf("skip core type in instance: %w", err)
			}
		case 0x01: // type
			if err := skipComponentTypeInline(r); err != nil {
				return nil, fmt.Errorf("skip inline type in instance: %w", err)
			}
		case 0x02: // alias
			if err := skipAlias(r); err != nil {
				return nil, fmt.Errorf("skip alias in instance: %w", err)
			}
		case 0x04: // export
			name, err := decodeExternName(r)
			if err != nil {
				return nil, fmt.Errorf("read export name: %w", err)
			}
			if err := skipExternDesc(r); err != nil {
				return nil, fmt.Errorf("skip export desc: %w", err)
			}
			it.Exports = append(it.Exports, component.ExportType{
				Name: name,
			})
		default:
			return nil, fmt.Errorf("unknown instance type decl 0x%02x at decl %d", declByte, i)
		}
	}
	return it, nil
}

// decodeExternName decodes an externname which can be a plain kebab-name
// or an interface URL.
//
//	externname ::= 0x00 n:<name>  => kebab-name n
//	             | 0x01 n:<name>  => interface n
func decodeExternName(r *bytes.Reader) (string, error) {
	kindByte, err := r.ReadByte()
	if err != nil {
		return "", err
	}
	switch kindByte {
	case 0x00, 0x01: // plain name or interface name
		return decodeName(r)
	default:
		// In some versions, the name kind byte is omitted and the
		// first byte is the name length directly.
		if err := r.UnreadByte(); err != nil {
			return "", err
		}
		return decodeName(r)
	}
}

// skipExternDesc skips an extern descriptor (type bound) in imports/exports.
//
//	externdesc ::= 0x00 0x11 i:<typeidx>    => core module (type i)
//	             | 0x01 i:<typeidx>          => func (type i)
//	             | 0x02 t:<valtype>          => value (type t) [gated]
//	             | 0x03 b:<typebound>        => type (bound b)
//	             | 0x04 i:<typeidx>          => component (type i)
//	             | 0x05 i:<typeidx>          => instance (type i)
func skipExternDesc(r *bytes.Reader) error {
	kindByte, err := r.ReadByte()
	if err != nil {
		return err
	}
	switch kindByte {
	case 0x00: // core module
		subKind, err := r.ReadByte()
		if err != nil {
			return err
		}
		_ = subKind // should be 0x11
		if _, _, err := leb128.DecodeUint32(r); err != nil {
			return err
		}
	case 0x01, 0x04, 0x05: // func, component, instance -> type index
		if _, _, err := leb128.DecodeUint32(r); err != nil {
			return err
		}
	case 0x02: // value
		if _, err := decodeValType(r); err != nil {
			return err
		}
	case 0x03: // type bound
		boundByte, err := r.ReadByte()
		if err != nil {
			return err
		}
		switch boundByte {
		case 0x00: // eq bound - followed by type index
			if _, _, err := leb128.DecodeUint32(r); err != nil {
				return err
			}
		case 0x01: // sub resource - no following data
			// This is a fresh resource type with no type index.
		}
	default:
		return fmt.Errorf("unknown extern desc kind 0x%02x", kindByte)
	}
	return nil
}

// decodeResourceType decodes a resource type definition.
func decodeResourceType(r *bytes.Reader) (*component.ResourceType, error) {
	rt := &component.ResourceType{}
	hasDtor, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	if hasDtor == 0x01 {
		idx, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return nil, err
		}
		rt.DestructorIndex = &idx
	}
	return rt, nil
}

// skipCoreType skips a core type definition within a component type section.
// Core types in the component model binary format contain function types,
// module types, etc.
func skipCoreType(r *bytes.Reader) error {
	typeByte, err := r.ReadByte()
	if err != nil {
		return err
	}
	switch typeByte {
	case 0x60: // core function type
		// Skip params.
		paramCount, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return err
		}
		for j := uint32(0); j < paramCount; j++ {
			if _, err := r.ReadByte(); err != nil {
				return err
			}
		}
		// Skip results.
		resultCount, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return err
		}
		for j := uint32(0); j < resultCount; j++ {
			if _, err := r.ReadByte(); err != nil {
				return err
			}
		}
	case 0x50: // core module type
		// A core module type is a sequence of module type declarations.
		declCount, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return err
		}
		for j := uint32(0); j < declCount; j++ {
			if err := skipModuleTypeDecl(r); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unknown core type 0x%02x", typeByte)
	}
	return nil
}

// skipModuleTypeDecl skips a single module type declaration.
func skipModuleTypeDecl(r *bytes.Reader) error {
	declByte, err := r.ReadByte()
	if err != nil {
		return err
	}
	switch declByte {
	case 0x00: // import
		// module name
		if _, err := decodeName(r); err != nil {
			return err
		}
		// field name
		if _, err := decodeName(r); err != nil {
			return err
		}
		// import desc
		if err := skipImportDesc(r); err != nil {
			return err
		}
	case 0x01: // type (inline core type def)
		if err := skipCoreType(r); err != nil {
			return err
		}
	case 0x02: // alias
		if err := skipAlias(r); err != nil {
			return err
		}
	case 0x03: // export
		if _, err := decodeName(r); err != nil {
			return err
		}
		if err := skipImportDesc(r); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown module type decl 0x%02x", declByte)
	}
	return nil
}

// skipImportDesc skips a core import/export descriptor.
func skipImportDesc(r *bytes.Reader) error {
	descByte, err := r.ReadByte()
	if err != nil {
		return err
	}
	switch descByte {
	case 0x00: // func type index
		if _, _, err := leb128.DecodeUint32(r); err != nil {
			return err
		}
	case 0x01: // table type
		if _, err := r.ReadByte(); err != nil { // reftype
			return err
		}
		if err := skipLimits(r); err != nil {
			return err
		}
	case 0x02: // memory type
		if err := skipLimits(r); err != nil {
			return err
		}
	case 0x03: // global type
		if _, err := r.ReadByte(); err != nil { // valtype
			return err
		}
		if _, err := r.ReadByte(); err != nil { // mut
			return err
		}
	default:
		return fmt.Errorf("unknown import desc 0x%02x", descByte)
	}
	return nil
}

// skipLimits skips a limits encoding (used in table and memory types).
func skipLimits(r *bytes.Reader) error {
	flag, err := r.ReadByte()
	if err != nil {
		return err
	}
	if _, _, err := leb128.DecodeUint32(r); err != nil { // min
		return err
	}
	if flag == 0x01 {
		if _, _, err := leb128.DecodeUint32(r); err != nil { // max
			return err
		}
	}
	return nil
}

// skipComponentTypeInline skips an inline component type definition within
// instance or component type declarations.
func skipComponentTypeInline(r *bytes.Reader) error {
	typeByte, err := r.ReadByte()
	if err != nil {
		return err
	}
	switch typeByte {
	case 0x40, 0x43: // func type (0x40 = sync, 0x43 = async)
		if _, err := decodeFuncType(r); err != nil {
			return err
		}
	case 0x42: // instance type
		if _, err := decodeInstanceType(r); err != nil {
			return err
		}
	case 0x41: // core type
		if err := skipCoreType(r); err != nil {
			return err
		}
	case 0x3f: // resource type
		if _, err := decodeResourceType(r); err != nil {
			return err
		}
	case 0x72: // record
		if _, err := decodeRecordType(r); err != nil {
			return err
		}
	case 0x71: // variant
		if _, err := decodeVariantType(r); err != nil {
			return err
		}
	case 0x70: // list
		if _, err := decodeListType(r); err != nil {
			return err
		}
	case 0x6f: // tuple
		if _, err := decodeTupleType(r); err != nil {
			return err
		}
	case 0x6e: // flags
		if _, err := decodeFlagsType(r); err != nil {
			return err
		}
	case 0x6d: // enum
		if _, err := decodeEnumType(r); err != nil {
			return err
		}
	case 0x6b: // option
		if _, err := decodeOptionType(r); err != nil {
			return err
		}
	case 0x6a: // result
		if _, err := decodeResultType(r); err != nil {
			return err
		}
	case 0x69: // own
		if _, _, err := leb128.DecodeUint32(r); err != nil {
			return err
		}
	case 0x68: // borrow
		if _, _, err := leb128.DecodeUint32(r); err != nil {
			return err
		}
	case 0x67: // fixed-length-list
		if _, err := decodeListType(r); err != nil {
			return err
		}
		if _, _, err := leb128.DecodeUint32(r); err != nil { // fixed length
			return err
		}
	case 0x66: // stream
		if err := skipOptionalValType(r); err != nil {
			return err
		}
	case 0x65: // future
		if err := skipOptionalValType(r); err != nil {
			return err
		}
	default:
		// Primitive or type index reference - no additional data to skip for primitives.
		if !isPrimitiveValType(typeByte) {
			// Type index - already consumed the byte, might need to unread
			if err := r.UnreadByte(); err != nil {
				return err
			}
			if _, _, err := leb128.DecodeUint32(r); err != nil {
				return err
			}
		}
	}
	return nil
}

// skipComponentTypeDecl skips a component type definition (0x44).
func skipComponentTypeDecl(r *bytes.Reader) error {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		declByte, err := r.ReadByte()
		if err != nil {
			return err
		}
		switch declByte {
		case 0x00: // core type
			if err := skipCoreType(r); err != nil {
				return err
			}
		case 0x01: // type
			if err := skipComponentTypeInline(r); err != nil {
				return err
			}
		case 0x02: // alias
			if err := skipAlias(r); err != nil {
				return err
			}
		case 0x03: // import
			if _, err := decodeName(r); err != nil {
				return err
			}
			kindByte, err := r.ReadByte()
			if err != nil {
				return err
			}
			_ = kindByte
			if _, _, err := leb128.DecodeUint32(r); err != nil {
				return err
			}
		case 0x04: // export
			if _, err := decodeName(r); err != nil {
				return err
			}
			if _, err := r.ReadByte(); err != nil {
				return err
			}
			if _, _, err := leb128.DecodeUint32(r); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown component type decl 0x%02x", declByte)
		}
	}
	return nil
}

// skipAlias skips an alias definition within a type declaration.
// Format: sort target
//
//	sort = extern kind byte
//	target = 0x00 instanceidx name | 0x01 count idx
func skipAlias(r *bytes.Reader) error {
	// Read sort: 0x00 <core_sort> or single byte for component sorts.
	sortByte, err := r.ReadByte()
	if err != nil {
		return err
	}
	if sortByte == 0x00 {
		if _, err := r.ReadByte(); err != nil { // core sort byte
			return err
		}
	}
	targetByte, err := r.ReadByte()
	if err != nil {
		return err
	}
	switch component.AliasKind(targetByte) {
	case component.AliasKindInstanceExport:
		if _, _, err := leb128.DecodeUint32(r); err != nil { // instance index
			return err
		}
		if _, err := decodeName(r); err != nil { // name
			return err
		}
	case component.AliasKindOuter:
		if _, _, err := leb128.DecodeUint32(r); err != nil { // outer count
			return err
		}
		if _, _, err := leb128.DecodeUint32(r); err != nil { // index
			return err
		}
	case component.AliasKindCoreInstanceExport:
		if _, _, err := leb128.DecodeUint32(r); err != nil { // core instance index
			return err
		}
		if _, err := decodeName(r); err != nil { // name
			return err
		}
	default:
		// Unknown target kind: try to skip by assuming outer-like format (count + index).
		if _, _, err := leb128.DecodeUint32(r); err != nil {
			return err
		}
		if _, _, err := leb128.DecodeUint32(r); err != nil {
			return err
		}
	}
	return nil
}
