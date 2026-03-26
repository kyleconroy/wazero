package binary

import (
	"bytes"
	"fmt"

	"github.com/tetratelabs/wazero/internal/leb128"
	"github.com/tetratelabs/wazero/internal/wasm/component"
)

// decodeComponentTypes decodes a vector of component-level type definitions.
func decodeComponentTypes(r *bytes.Reader) ([]component.ComponentDefinedType, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read count: %w", err)
	}

	types := make([]component.ComponentDefinedType, count)
	for i := uint32(0); i < count; i++ {
		t, err := decodeComponentDefinedType(r)
		if err != nil {
			return nil, fmt.Errorf("type[%d]: %w", i, err)
		}
		types[i] = t
	}
	return types, nil
}

// decodeComponentDefinedType decodes a single component type definition.
func decodeComponentDefinedType(r *bytes.Reader) (component.ComponentDefinedType, error) {
	tag, err := r.ReadByte()
	if err != nil {
		return component.ComponentDefinedType{}, fmt.Errorf("read type tag: %w", err)
	}

	switch {
	case tag == byte(component.DefinedTypeKindFunc):
		ft, err := decodeFuncType(r)
		if err != nil {
			return component.ComponentDefinedType{}, err
		}
		return component.ComponentDefinedType{
			Kind: component.DefinedTypeKindFunc,
			Func: ft,
		}, nil

	case tag == byte(component.DefinedTypeKindComponent):
		ct, err := decodeComponentTypeDecl(r)
		if err != nil {
			return component.ComponentDefinedType{}, err
		}
		return component.ComponentDefinedType{
			Kind:      component.DefinedTypeKindComponent,
			Component: ct,
		}, nil

	case tag == byte(component.DefinedTypeKindInstance):
		it, err := decodeInstanceTypeDecl(r)
		if err != nil {
			return component.ComponentDefinedType{}, err
		}
		return component.ComponentDefinedType{
			Kind:     component.DefinedTypeKindInstance,
			Instance: it,
		}, nil

	case tag == byte(component.DefinedTypeKindResource):
		rt, err := decodeResourceType(r)
		if err != nil {
			return component.ComponentDefinedType{}, err
		}
		return component.ComponentDefinedType{
			Kind:     component.DefinedTypeKindResource,
			Resource: rt,
		}, nil

	default:
		// It's a value type definition. Unread the byte and decode as a component type.
		if err := r.UnreadByte(); err != nil {
			return component.ComponentDefinedType{}, err
		}
		vt, err := decodeComponentValType(r)
		if err != nil {
			return component.ComponentDefinedType{}, fmt.Errorf("value type: %w", err)
		}
		return component.ComponentDefinedType{
			Kind: component.DefinedTypeKindType,
			Type: &vt,
		}, nil
	}
}

// decodeFuncType decodes a component function type.
func decodeFuncType(r *bytes.Reader) (*component.FuncType, error) {
	// Parameters
	paramCount, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read param count: %w", err)
	}

	params := make([]component.NamedType, paramCount)
	for i := uint32(0); i < paramCount; i++ {
		name, _, err := decodeUTF8(r, "param name")
		if err != nil {
			return nil, err
		}
		paramType, err := decodeComponentValType(r)
		if err != nil {
			return nil, fmt.Errorf("param type: %w", err)
		}
		params[i] = component.NamedType{Name: name, Type: paramType}
	}

	// Results - can be a single unnamed type or a named list.
	resultTag, err := r.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("read result tag: %w", err)
	}

	var results []component.NamedType

	switch resultTag {
	case 0x00:
		// Single unnamed result type
		resultType, err := decodeComponentValType(r)
		if err != nil {
			return nil, fmt.Errorf("result type: %w", err)
		}
		results = []component.NamedType{{Type: resultType}}

	case 0x01:
		// Named result list
		resultCount, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return nil, fmt.Errorf("read result count: %w", err)
		}
		results = make([]component.NamedType, resultCount)
		for i := uint32(0); i < resultCount; i++ {
			name, _, err := decodeUTF8(r, "result name")
			if err != nil {
				return nil, err
			}
			resultType, err := decodeComponentValType(r)
			if err != nil {
				return nil, fmt.Errorf("result type: %w", err)
			}
			results[i] = component.NamedType{Name: name, Type: resultType}
		}

	default:
		return nil, fmt.Errorf("unknown result tag: %#x", resultTag)
	}

	return &component.FuncType{
		Params:  params,
		Results: results,
	}, nil
}

// decodeComponentTypeDecl decodes an inline component type declaration.
func decodeComponentTypeDecl(r *bytes.Reader) (*component.ComponentTypeDecl, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read decl count: %w", err)
	}

	var imports []component.ComponentImport
	var exports []component.ComponentExport

	for i := uint32(0); i < count; i++ {
		declKind, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read decl kind: %w", err)
		}

		switch declKind {
		case 0x03: // import
			name, _, err := decodeUTF8(r, "component type import name")
			if err != nil {
				return nil, err
			}
			url, _, err := decodeUTF8(r, "component type import URL")
			if err != nil {
				return nil, err
			}
			desc, err := decodeExternDesc(r)
			if err != nil {
				return nil, err
			}
			imports = append(imports, component.ComponentImport{
				Name: name,
				URL:  url,
				Desc: desc,
			})

		case 0x04: // export
			name, _, err := decodeUTF8(r, "component type export name")
			if err != nil {
				return nil, err
			}
			kind, err := r.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("read export kind: %w", err)
			}
			idx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read export index: %w", err)
			}
			exports = append(exports, component.ComponentExport{
				Name:  name,
				Kind:  component.ExternDescKind(kind),
				Index: idx,
			})

		default:
			return nil, fmt.Errorf("unknown component type decl kind: %#x", declKind)
		}
	}

	return &component.ComponentTypeDecl{
		Imports: imports,
		Exports: exports,
	}, nil
}

// decodeInstanceTypeDecl decodes an inline instance type declaration.
func decodeInstanceTypeDecl(r *bytes.Reader) (*component.InstanceTypeDecl, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read decl count: %w", err)
	}

	exports := make([]component.ComponentExport, 0, count)
	for i := uint32(0); i < count; i++ {
		declKind, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read decl kind: %w", err)
		}

		switch declKind {
		case 0x04: // export
			name, _, err := decodeUTF8(r, "instance type export name")
			if err != nil {
				return nil, err
			}
			kind, err := r.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("read export kind: %w", err)
			}
			idx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read export index: %w", err)
			}
			exports = append(exports, component.ComponentExport{
				Name:  name,
				Kind:  component.ExternDescKind(kind),
				Index: idx,
			})

		default:
			return nil, fmt.Errorf("unknown instance type decl kind: %#x", declKind)
		}
	}

	return &component.InstanceTypeDecl{
		Exports: exports,
	}, nil
}

// decodeResourceType decodes a resource type.
func decodeResourceType(r *bytes.Reader) (*component.ResourceType, error) {
	rep, err := r.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("read representation: %w", err)
	}

	rt := &component.ResourceType{
		Representation: component.ValType(rep),
	}

	// Optional destructor
	hasDtor, err := r.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("read destructor flag: %w", err)
	}
	if hasDtor == 0x01 {
		dtorIdx, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return nil, fmt.Errorf("read destructor index: %w", err)
		}
		rt.Destructor = &dtorIdx
	}

	return rt, nil
}

// decodeComponentValType decodes a component value type.
func decodeComponentValType(r *bytes.Reader) (component.ComponentType, error) {
	tag, err := r.ReadByte()
	if err != nil {
		return component.ComponentType{}, fmt.Errorf("read val type tag: %w", err)
	}

	switch {
	// Primitive types
	case tag >= 0x73 && tag <= 0x7f:
		return component.ComponentType{
			Kind:      component.ComponentTypeKindPrimitive,
			Primitive: component.ValType(tag),
		}, nil

	// Type index reference
	case tag < 0x68:
		// Unread the byte and decode as LEB128 index.
		if err := r.UnreadByte(); err != nil {
			return component.ComponentType{}, err
		}
		idx, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return component.ComponentType{}, fmt.Errorf("read type index: %w", err)
		}
		return component.ComponentType{
			Kind:  component.ComponentTypeKindIndex,
			Index: idx,
		}, nil

	// Record type
	case tag == byte(component.TypeKindRecord):
		rt, err := decodeRecordType(r)
		if err != nil {
			return component.ComponentType{}, err
		}
		return component.ComponentType{
			Kind:   component.ComponentTypeKindRecord,
			Record: rt,
		}, nil

	// Variant type
	case tag == byte(component.TypeKindVariant):
		vt, err := decodeVariantType(r)
		if err != nil {
			return component.ComponentType{}, err
		}
		return component.ComponentType{
			Kind:    component.ComponentTypeKindVariant,
			Variant: vt,
		}, nil

	// List type
	case tag == byte(component.TypeKindList):
		lt, err := decodeListType(r)
		if err != nil {
			return component.ComponentType{}, err
		}
		return component.ComponentType{
			Kind: component.ComponentTypeKindList,
			List: lt,
		}, nil

	// Tuple type
	case tag == byte(component.TypeKindTuple):
		tt, err := decodeTupleType(r)
		if err != nil {
			return component.ComponentType{}, err
		}
		return component.ComponentType{
			Kind:  component.ComponentTypeKindTuple,
			Tuple: tt,
		}, nil

	// Flags type
	case tag == byte(component.TypeKindFlags):
		ft, err := decodeFlagsType(r)
		if err != nil {
			return component.ComponentType{}, err
		}
		return component.ComponentType{
			Kind:  component.ComponentTypeKindFlags,
			Flags: ft,
		}, nil

	// Enum type
	case tag == byte(component.TypeKindEnum):
		et, err := decodeEnumType(r)
		if err != nil {
			return component.ComponentType{}, err
		}
		return component.ComponentType{
			Kind: component.ComponentTypeKindEnum,
			Enum: et,
		}, nil

	// Option type
	case tag == byte(component.TypeKindOption):
		ot, err := decodeOptionType(r)
		if err != nil {
			return component.ComponentType{}, err
		}
		return component.ComponentType{
			Kind:   component.ComponentTypeKindOption,
			Option: ot,
		}, nil

	// Result type
	case tag == byte(component.TypeKindResult):
		rt, err := decodeResultType(r)
		if err != nil {
			return component.ComponentType{}, err
		}
		return component.ComponentType{
			Kind:   component.ComponentTypeKindResult,
			Result: rt,
		}, nil

	// Own handle
	case tag == byte(component.TypeKindOwn):
		idx, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return component.ComponentType{}, fmt.Errorf("read own index: %w", err)
		}
		return component.ComponentType{
			Kind: component.ComponentTypeKindOwn,
			Own:  &component.OwnType{TypeIndex: idx},
		}, nil

	// Borrow handle
	case tag == byte(component.TypeKindBorrow):
		idx, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return component.ComponentType{}, fmt.Errorf("read borrow index: %w", err)
		}
		return component.ComponentType{
			Kind:   component.ComponentTypeKindBorrow,
			Borrow: &component.BorrowType{TypeIndex: idx},
		}, nil

	default:
		return component.ComponentType{}, fmt.Errorf("unknown val type tag: %#x", tag)
	}
}

func decodeRecordType(r *bytes.Reader) (*component.RecordType, error) {
	fieldCount, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read field count: %w", err)
	}

	fields := make([]component.RecordField, fieldCount)
	for i := uint32(0); i < fieldCount; i++ {
		name, _, err := decodeUTF8(r, "record field name")
		if err != nil {
			return nil, err
		}
		fieldType, err := decodeComponentValType(r)
		if err != nil {
			return nil, fmt.Errorf("field type: %w", err)
		}
		fields[i] = component.RecordField{Name: name, Type: fieldType}
	}
	return &component.RecordType{Fields: fields}, nil
}

func decodeVariantType(r *bytes.Reader) (*component.VariantType, error) {
	caseCount, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read case count: %w", err)
	}

	cases := make([]component.VariantCase, caseCount)
	for i := uint32(0); i < caseCount; i++ {
		name, _, err := decodeUTF8(r, "variant case name")
		if err != nil {
			return nil, err
		}

		// Optional payload type
		hasPayload, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read has payload: %w", err)
		}

		var payloadType *component.ComponentType
		if hasPayload == 0x01 {
			t, err := decodeComponentValType(r)
			if err != nil {
				return nil, fmt.Errorf("case type: %w", err)
			}
			payloadType = &t
		}

		// Optional refines
		hasRefines, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read has refines: %w", err)
		}

		var refines *uint32
		if hasRefines == 0x01 {
			idx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read refines index: %w", err)
			}
			refines = &idx
		}

		cases[i] = component.VariantCase{
			Name:    name,
			Type:    payloadType,
			Refines: refines,
		}
	}
	return &component.VariantType{Cases: cases}, nil
}

func decodeListType(r *bytes.Reader) (*component.ListType, error) {
	elemType, err := decodeComponentValType(r)
	if err != nil {
		return nil, fmt.Errorf("element type: %w", err)
	}
	return &component.ListType{Element: elemType}, nil
}

func decodeTupleType(r *bytes.Reader) (*component.TupleType, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read count: %w", err)
	}

	types := make([]component.ComponentType, count)
	for i := uint32(0); i < count; i++ {
		t, err := decodeComponentValType(r)
		if err != nil {
			return nil, fmt.Errorf("tuple type[%d]: %w", i, err)
		}
		types[i] = t
	}
	return &component.TupleType{Types: types}, nil
}

func decodeFlagsType(r *bytes.Reader) (*component.FlagsType, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read count: %w", err)
	}

	names := make([]string, count)
	for i := uint32(0); i < count; i++ {
		name, _, err := decodeUTF8(r, "flag name")
		if err != nil {
			return nil, err
		}
		names[i] = name
	}
	return &component.FlagsType{Names: names}, nil
}

func decodeEnumType(r *bytes.Reader) (*component.EnumType, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read count: %w", err)
	}

	names := make([]string, count)
	for i := uint32(0); i < count; i++ {
		name, _, err := decodeUTF8(r, "enum name")
		if err != nil {
			return nil, err
		}
		names[i] = name
	}
	return &component.EnumType{Names: names}, nil
}

func decodeOptionType(r *bytes.Reader) (*component.OptionType, error) {
	innerType, err := decodeComponentValType(r)
	if err != nil {
		return nil, fmt.Errorf("inner type: %w", err)
	}
	return &component.OptionType{Type: innerType}, nil
}

func decodeResultType(r *bytes.Reader) (*component.ResultType, error) {
	// Result encoding: tag byte followed by optional ok and error types.
	tag, err := r.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("read result tag: %w", err)
	}

	rt := &component.ResultType{}

	switch tag {
	case 0x00:
		// result<ok, err> - both present
		okType, err := decodeComponentValType(r)
		if err != nil {
			return nil, fmt.Errorf("ok type: %w", err)
		}
		rt.Ok = &okType

		errType, err := decodeComponentValType(r)
		if err != nil {
			return nil, fmt.Errorf("err type: %w", err)
		}
		rt.Err = &errType

	case 0x01:
		// result<ok> - only ok
		okType, err := decodeComponentValType(r)
		if err != nil {
			return nil, fmt.Errorf("ok type: %w", err)
		}
		rt.Ok = &okType

	case 0x02:
		// result<_, err> - only error
		errType, err := decodeComponentValType(r)
		if err != nil {
			return nil, fmt.Errorf("err type: %w", err)
		}
		rt.Err = &errType

	case 0x03:
		// result - neither

	default:
		return nil, fmt.Errorf("unknown result tag: %#x", tag)
	}

	return rt, nil
}
