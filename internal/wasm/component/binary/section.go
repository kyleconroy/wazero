package binary

import (
	"bytes"
	"fmt"
	"io"

	"github.com/tetratelabs/wazero/internal/leb128"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
	"github.com/tetratelabs/wazero/internal/wasm/component"
)

// decodeCoreInstances decodes a vector of core instance definitions.
func decodeCoreInstances(r *bytes.Reader) ([]component.CoreInstance, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read count: %w", err)
	}

	instances := make([]component.CoreInstance, count)
	for i := uint32(0); i < count; i++ {
		kind, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read core instance kind: %w", err)
		}

		switch component.CoreInstanceKind(kind) {
		case component.CoreInstanceKindInstantiate:
			moduleIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read module index: %w", err)
			}

			argCount, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read arg count: %w", err)
			}

			args := make([]component.CoreInstantiationArg, argCount)
			for j := uint32(0); j < argCount; j++ {
				name, _, err := decodeUTF8(r, "core instantiation arg name")
				if err != nil {
					return nil, err
				}
				argKind, err := r.ReadByte()
				if err != nil {
					return nil, fmt.Errorf("read arg kind: %w", err)
				}
				argIdx, _, err := leb128.DecodeUint32(r)
				if err != nil {
					return nil, fmt.Errorf("read arg index: %w", err)
				}
				args[j] = component.CoreInstantiationArg{
					Name:  name,
					Kind:  component.CoreInstantiationArgKind(argKind),
					Index: argIdx,
				}
			}

			instances[i] = component.CoreInstance{
				Kind:        component.CoreInstanceKindInstantiate,
				ModuleIndex: moduleIdx,
				Args:        args,
			}

		case component.CoreInstanceKindFromExports:
			exportCount, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read export count: %w", err)
			}

			exports := make([]component.CoreInstanceExport, exportCount)
			for j := uint32(0); j < exportCount; j++ {
				name, _, err := decodeUTF8(r, "core instance export name")
				if err != nil {
					return nil, err
				}
				exportKind, err := r.ReadByte()
				if err != nil {
					return nil, fmt.Errorf("read export kind: %w", err)
				}
				exportIdx, _, err := leb128.DecodeUint32(r)
				if err != nil {
					return nil, fmt.Errorf("read export index: %w", err)
				}
				exports[j] = component.CoreInstanceExport{
					Name:  name,
					Kind:  api.ExternType(exportKind),
					Index: exportIdx,
				}
			}

			instances[i] = component.CoreInstance{
				Kind:    component.CoreInstanceKindFromExports,
				Exports: exports,
			}

		default:
			return nil, fmt.Errorf("unknown core instance kind: %#x", kind)
		}
	}
	return instances, nil
}

// decodeCoreTypes decodes a vector of core type definitions.
func decodeCoreTypes(r *bytes.Reader) ([]wasm.FunctionType, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read count: %w", err)
	}

	types := make([]wasm.FunctionType, count)
	for i := uint32(0); i < count; i++ {
		// Core types in components are wrapped in a 0x00 tag for "core type",
		// followed by the standard function type encoding.
		tag, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read core type tag: %w", err)
		}
		if tag != 0x60 {
			return nil, fmt.Errorf("unsupported core type tag: %#x (expected function type 0x60)", tag)
		}

		paramCount, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return nil, fmt.Errorf("read param count: %w", err)
		}
		params := make([]wasm.ValueType, paramCount)
		if _, err := io.ReadFull(r, params); err != nil {
			return nil, fmt.Errorf("read param types: %w", err)
		}

		resultCount, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return nil, fmt.Errorf("read result count: %w", err)
		}
		results := make([]wasm.ValueType, resultCount)
		if _, err := io.ReadFull(r, results); err != nil {
			return nil, fmt.Errorf("read result types: %w", err)
		}

		types[i] = wasm.FunctionType{
			Params:  params,
			Results: results,
		}
	}
	return types, nil
}

// decodeInstances decodes a vector of component-level instance definitions.
func decodeInstances(r *bytes.Reader) ([]component.Instance, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read count: %w", err)
	}

	instances := make([]component.Instance, count)
	for i := uint32(0); i < count; i++ {
		kind, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read instance kind: %w", err)
		}

		switch component.InstanceKind(kind) {
		case component.InstanceKindInstantiate:
			compIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read component index: %w", err)
			}

			argCount, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read arg count: %w", err)
			}

			args := make([]component.InstantiationArg, argCount)
			for j := uint32(0); j < argCount; j++ {
				name, _, err := decodeUTF8(r, "instantiation arg name")
				if err != nil {
					return nil, err
				}
				argKind, err := r.ReadByte()
				if err != nil {
					return nil, fmt.Errorf("read arg kind: %w", err)
				}
				argIdx, _, err := leb128.DecodeUint32(r)
				if err != nil {
					return nil, fmt.Errorf("read arg index: %w", err)
				}
				args[j] = component.InstantiationArg{
					Name:  name,
					Kind:  component.ExternDescKind(argKind),
					Index: argIdx,
				}
			}

			instances[i] = component.Instance{
				Kind:           component.InstanceKindInstantiate,
				ComponentIndex: compIdx,
				Args:           args,
			}

		case component.InstanceKindFromExports:
			exportCount, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read export count: %w", err)
			}

			exports := make([]component.ComponentExportItem, exportCount)
			for j := uint32(0); j < exportCount; j++ {
				name, _, err := decodeUTF8(r, "instance export name")
				if err != nil {
					return nil, err
				}
				exportKind, err := r.ReadByte()
				if err != nil {
					return nil, fmt.Errorf("read export kind: %w", err)
				}
				exportIdx, _, err := leb128.DecodeUint32(r)
				if err != nil {
					return nil, fmt.Errorf("read export index: %w", err)
				}
				exports[j] = component.ComponentExportItem{
					Name:  name,
					Kind:  component.ExternDescKind(exportKind),
					Index: exportIdx,
				}
			}

			instances[i] = component.Instance{
				Kind:    component.InstanceKindFromExports,
				Exports: exports,
			}

		default:
			return nil, fmt.Errorf("unknown instance kind: %#x", kind)
		}
	}
	return instances, nil
}

// decodeAliases decodes a vector of alias definitions.
func decodeAliases(r *bytes.Reader) ([]component.Alias, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read count: %w", err)
	}

	aliases := make([]component.Alias, count)
	for i := uint32(0); i < count; i++ {
		sort, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read alias sort: %w", err)
		}

		// Determine if this is a core sort or component sort.
		var aliasSort component.AliasSort
		if sort == 0x00 {
			// Core sort follows
			coreSortByte, err := r.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("read core sort: %w", err)
			}
			aliasSort = component.AliasSort(0x10 + coreSortByte)
		} else {
			aliasSort = component.AliasSort(sort)
		}

		// Read the alias target kind.
		targetKind, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read alias kind: %w", err)
		}

		switch component.AliasKind(targetKind) {
		case component.AliasKindInstanceExport:
			instanceIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read instance index: %w", err)
			}
			name, _, err := decodeUTF8(r, "alias export name")
			if err != nil {
				return nil, err
			}
			aliases[i] = component.Alias{
				Kind:          component.AliasKindInstanceExport,
				Sort:          aliasSort,
				InstanceIndex: instanceIdx,
				Name:          name,
			}

		case component.AliasKindOuter:
			outerCount, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read outer count: %w", err)
			}
			outerIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read outer index: %w", err)
			}
			aliases[i] = component.Alias{
				Kind:       component.AliasKindOuter,
				Sort:       aliasSort,
				OuterCount: outerCount,
				OuterIndex: outerIdx,
			}

		default:
			return nil, fmt.Errorf("unknown alias kind: %#x", targetKind)
		}
	}
	return aliases, nil
}

// decodeCanons decodes a vector of canonical function definitions.
func decodeCanons(r *bytes.Reader) ([]component.Canon, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read count: %w", err)
	}

	canons := make([]component.Canon, count)
	for i := uint32(0); i < count; i++ {
		kind, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read canon kind: %w", err)
		}

		switch component.CanonKind(kind) {
		case component.CanonKindLift:
			coreFuncIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read core func index: %w", err)
			}

			opts, err := decodeCanonOptions(r)
			if err != nil {
				return nil, err
			}

			typeIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read type index: %w", err)
			}

			canons[i] = component.Canon{
				Kind:          component.CanonKindLift,
				CoreFuncIndex: coreFuncIdx,
				TypeIndex:     typeIdx,
				Options:       opts,
			}

		case component.CanonKindLower:
			funcIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read func index: %w", err)
			}

			opts, err := decodeCanonOptions(r)
			if err != nil {
				return nil, err
			}

			canons[i] = component.Canon{
				Kind:      component.CanonKindLower,
				FuncIndex: funcIdx,
				Options:   opts,
			}

		case component.CanonKindResourceNew:
			typeIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read resource type index: %w", err)
			}
			canons[i] = component.Canon{
				Kind:              component.CanonKindResourceNew,
				ResourceTypeIndex: typeIdx,
			}

		case component.CanonKindResourceDrop:
			typeIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read resource type index: %w", err)
			}
			canons[i] = component.Canon{
				Kind:              component.CanonKindResourceDrop,
				ResourceTypeIndex: typeIdx,
			}

		case component.CanonKindResourceRep:
			typeIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read resource type index: %w", err)
			}
			canons[i] = component.Canon{
				Kind:              component.CanonKindResourceRep,
				ResourceTypeIndex: typeIdx,
			}

		default:
			return nil, fmt.Errorf("unknown canon kind: %#x", kind)
		}
	}
	return canons, nil
}

// decodeCanonOptions decodes canonical function options.
func decodeCanonOptions(r *bytes.Reader) ([]component.CanonOption, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read option count: %w", err)
	}

	if count == 0 {
		return nil, nil
	}

	opts := make([]component.CanonOption, count)
	for i := uint32(0); i < count; i++ {
		optKind, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read option kind: %w", err)
		}

		opt := component.CanonOption{Kind: component.CanonOptionKind(optKind)}

		switch component.CanonOptionKind(optKind) {
		case component.CanonOptionKindUTF8, component.CanonOptionKindUTF16, component.CanonOptionKindLatin1:
			// No additional data
		case component.CanonOptionKindMemory, component.CanonOptionKindRealloc, component.CanonOptionKindPostReturn:
			val, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read option value: %w", err)
			}
			opt.Value = val
		default:
			return nil, fmt.Errorf("unknown canon option kind: %#x", optKind)
		}

		opts[i] = opt
	}
	return opts, nil
}

// decodeComponentStart decodes the component start section.
func decodeComponentStart(r *bytes.Reader) (*component.ComponentStart, error) {
	funcIdx, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read func index: %w", err)
	}

	argCount, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read arg count: %w", err)
	}

	args := make([]uint32, argCount)
	for i := uint32(0); i < argCount; i++ {
		args[i], _, err = leb128.DecodeUint32(r)
		if err != nil {
			return nil, fmt.Errorf("read arg %d: %w", i, err)
		}
	}

	results, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read result count: %w", err)
	}

	return &component.ComponentStart{
		FuncIndex: funcIdx,
		Args:      args,
		Results:   results,
	}, nil
}

// decodeComponentImports decodes a vector of component imports.
func decodeComponentImports(r *bytes.Reader) ([]component.ComponentImport, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read count: %w", err)
	}

	imports := make([]component.ComponentImport, count)
	for i := uint32(0); i < count; i++ {
		name, _, err := decodeUTF8(r, "import name")
		if err != nil {
			return nil, err
		}

		// Optional URL
		url, _, err := decodeUTF8(r, "import URL")
		if err != nil {
			return nil, err
		}

		desc, err := decodeExternDesc(r)
		if err != nil {
			return nil, fmt.Errorf("read import desc: %w", err)
		}

		imports[i] = component.ComponentImport{
			Name: name,
			URL:  url,
			Desc: desc,
		}
	}
	return imports, nil
}

// decodeComponentExports decodes a vector of component exports.
func decodeComponentExports(r *bytes.Reader) ([]component.ComponentExport, error) {
	count, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return nil, fmt.Errorf("read count: %w", err)
	}

	exports := make([]component.ComponentExport, count)
	for i := uint32(0); i < count; i++ {
		name, _, err := decodeUTF8(r, "export name")
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

		export := component.ComponentExport{
			Name:  name,
			Kind:  component.ExternDescKind(kind),
			Index: idx,
		}

		// Optional URL.
		url, _, err := decodeUTF8(r, "export URL")
		if err != nil {
			return nil, err
		}
		export.URL = url

		// Optional type ascription.
		hasType, err := r.ReadByte()
		if err == nil && hasType == 0x01 {
			typeIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read export type index: %w", err)
			}
			export.TypeIndex = &typeIdx
		} else if err == nil && hasType == 0x00 {
			// No type ascription.
		} else if err != nil {
			// End of data is OK here.
		}

		exports[i] = export
	}
	return exports, nil
}

// decodeExternDesc decodes a component external description (import/export type).
func decodeExternDesc(r *bytes.Reader) (component.ExternDesc, error) {
	kind, err := r.ReadByte()
	if err != nil {
		return component.ExternDesc{}, fmt.Errorf("read extern desc kind: %w", err)
	}

	desc := component.ExternDesc{Kind: component.ExternDescKind(kind)}

	switch component.ExternDescKind(kind) {
	case component.ExternDescKindModule,
		component.ExternDescKindFunc,
		component.ExternDescKindValue,
		component.ExternDescKindType,
		component.ExternDescKindComponent,
		component.ExternDescKindInstance:
		idx, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return desc, fmt.Errorf("read extern desc index: %w", err)
		}
		desc.TypeIndex = idx

	default:
		return desc, fmt.Errorf("unknown extern desc kind: %#x", kind)
	}

	return desc, nil
}
