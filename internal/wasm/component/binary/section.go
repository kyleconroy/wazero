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
		ft, err := decodeSingleCoreType(r)
		if err != nil {
			return nil, fmt.Errorf("core type[%d]: %w", i, err)
		}
		types[i] = ft
	}
	return types, nil
}

// decodeSingleCoreType decodes a single core function type definition.
func decodeSingleCoreType(r *bytes.Reader) (wasm.FunctionType, error) {
	tag, err := r.ReadByte()
	if err != nil {
		return wasm.FunctionType{}, fmt.Errorf("read core type tag: %w", err)
	}
	if tag != 0x60 {
		return wasm.FunctionType{}, fmt.Errorf("unsupported core type tag: %#x (expected function type 0x60)", tag)
	}

	paramCount, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return wasm.FunctionType{}, fmt.Errorf("read param count: %w", err)
	}
	params := make([]wasm.ValueType, paramCount)
	if _, err := io.ReadFull(r, params); err != nil {
		return wasm.FunctionType{}, fmt.Errorf("read param types: %w", err)
	}

	resultCount, _, err := leb128.DecodeUint32(r)
	if err != nil {
		return wasm.FunctionType{}, fmt.Errorf("read result count: %w", err)
	}
	results := make([]wasm.ValueType, resultCount)
	if _, err := io.ReadFull(r, results); err != nil {
		return wasm.FunctionType{}, fmt.Errorf("read result types: %w", err)
	}

	return wasm.FunctionType{
		Params:  params,
		Results: results,
	}, nil
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
		a, err := decodeSingleAlias(r)
		if err != nil {
			return nil, fmt.Errorf("alias[%d]: %w", i, err)
		}
		aliases[i] = a
	}
	return aliases, nil
}

// decodeSingleAlias decodes one alias definition.
// Format: sort target
//   - sort: 0x00 + core_sort_byte for core sorts, or component sort byte directly
//   - target: 0x00 = component instance export (idx, name)
//     0x01 = core instance export (idx, name)
//     0x02 = outer (count, idx)
func decodeSingleAlias(r *bytes.Reader) (component.Alias, error) {
	sort, err := r.ReadByte()
	if err != nil {
		return component.Alias{}, fmt.Errorf("read alias sort: %w", err)
	}

	var aliasSort component.AliasSort
	if sort == 0x00 {
		coreSortByte, err := r.ReadByte()
		if err != nil {
			return component.Alias{}, fmt.Errorf("read core sort: %w", err)
		}
		aliasSort = component.AliasSort(0x10 + coreSortByte)
	} else {
		aliasSort = component.AliasSort(sort)
	}

	targetKind, err := r.ReadByte()
	if err != nil {
		return component.Alias{}, fmt.Errorf("read alias target kind: %w", err)
	}

	switch component.AliasKind(targetKind) {
	case component.AliasKindInstanceExport, component.AliasKindCoreInstanceExport:
		instanceIdx, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return component.Alias{}, fmt.Errorf("read instance index: %w", err)
		}
		name, _, err := decodeUTF8(r, "alias export name")
		if err != nil {
			return component.Alias{}, err
		}
		return component.Alias{
			Kind:          component.AliasKind(targetKind),
			Sort:          aliasSort,
			InstanceIndex: instanceIdx,
			Name:          name,
		}, nil

	case component.AliasKindOuter:
		outerCount, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return component.Alias{}, fmt.Errorf("read outer count: %w", err)
		}
		outerIdx, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return component.Alias{}, fmt.Errorf("read outer index: %w", err)
		}
		return component.Alias{
			Kind:       component.AliasKindOuter,
			Sort:       aliasSort,
			OuterCount: outerCount,
			OuterIndex: outerIdx,
		}, nil

	default:
		return component.Alias{}, fmt.Errorf("unknown alias target kind: %#x", targetKind)
	}
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
			// canon lift has a reserved second byte (0x00) per the binary format spec.
			if reserved, err := r.ReadByte(); err != nil {
				return nil, fmt.Errorf("read canon lift reserved byte: %w", err)
			} else if reserved != 0x00 {
				return nil, fmt.Errorf("expected 0x00 reserved byte after canon lift, got %#x", reserved)
			}

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
			// canon lower has a reserved second byte (0x00) per the binary format spec.
			if reserved, err := r.ReadByte(); err != nil {
				return nil, fmt.Errorf("read canon lower reserved byte: %w", err)
			} else if reserved != 0x00 {
				return nil, fmt.Errorf("expected 0x00 reserved byte after canon lower, got %#x", reserved)
			}

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

		case component.CanonKindTaskCancel,
			component.CanonKindBackpressureSet,
			component.CanonKindSubtaskDrop,
			component.CanonKindErrorContextDrop,
			component.CanonKindWaitableSetNew,
			component.CanonKindWaitableSetDrop,
			component.CanonKindWaitableJoin:
			// No arguments.
			canons[i] = component.Canon{Kind: component.CanonKind(kind)}

		case component.CanonKindSubtaskCancel,
			component.CanonKindYield,
			component.CanonKindWaitableSetWait,
			component.CanonKindWaitableSetPoll:
			// Single async flag byte.
			asyncByte, err := r.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("read async flag: %w", err)
			}
			canons[i] = component.Canon{
				Kind:      component.CanonKind(kind),
				AsyncFlag: asyncByte != 0,
			}

		case component.CanonKindTaskReturn:
			// task.return: result?:typeidx? opts:vec(canonopt)
			// Optional type uses 0x00 idx (some) or 0x01 (none).
			hasResult, err := r.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("read task.return has-result: %w", err)
			}
			c := component.Canon{Kind: component.CanonKindTaskReturn}
			if hasResult == 0x00 {
				idx, _, err := leb128.DecodeUint32(r)
				if err != nil {
					return nil, fmt.Errorf("read task.return type index: %w", err)
				}
				c.ResultType = &idx
			}
			opts, err := decodeCanonOptions(r)
			if err != nil {
				return nil, err
			}
			c.Options = opts
			canons[i] = c

		case component.CanonKindContextGet, component.CanonKindContextSet:
			// context.get/set: core_valtype:byte index:u32
			vt, err := r.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("read context valtype: %w", err)
			}
			idx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read context index: %w", err)
			}
			canons[i] = component.Canon{
				Kind:         component.CanonKind(kind),
				CoreValType:  vt,
				ContextIndex: idx,
			}

		case component.CanonKindStreamNew,
			component.CanonKindStreamDropReadable,
			component.CanonKindStreamDropWritable,
			component.CanonKindFutureNew,
			component.CanonKindFutureDropReadable,
			component.CanonKindFutureDropWritable:
			// Single type index.
			typeIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read stream/future type index: %w", err)
			}
			canons[i] = component.Canon{
				Kind:                  component.CanonKind(kind),
				StreamFutureTypeIndex: typeIdx,
			}

		case component.CanonKindStreamRead,
			component.CanonKindStreamWrite,
			component.CanonKindFutureRead,
			component.CanonKindFutureWrite:
			// Type index followed by options.
			typeIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read stream/future type index: %w", err)
			}
			opts, err := decodeCanonOptions(r)
			if err != nil {
				return nil, err
			}
			canons[i] = component.Canon{
				Kind:                  component.CanonKind(kind),
				StreamFutureTypeIndex: typeIdx,
				Options:               opts,
			}

		case component.CanonKindStreamCancelRead,
			component.CanonKindStreamCancelWrite,
			component.CanonKindFutureCancelRead,
			component.CanonKindFutureCancelWrite:
			// Type index followed by async flag.
			typeIdx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read stream/future type index: %w", err)
			}
			asyncByte, err := r.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("read async flag: %w", err)
			}
			canons[i] = component.Canon{
				Kind:                  component.CanonKind(kind),
				StreamFutureTypeIndex: typeIdx,
				AsyncFlag:             asyncByte != 0,
			}

		case component.CanonKindErrorContextNew:
			// error-context.new: opts:vec(canonopt)
			opts, err := decodeCanonOptions(r)
			if err != nil {
				return nil, err
			}
			canons[i] = component.Canon{
				Kind:    component.CanonKindErrorContextNew,
				Options: opts,
			}

		case component.CanonKindErrorContextDebugMsg:
			// error-context.debug-message: opts:vec(canonopt)
			opts, err := decodeCanonOptions(r)
			if err != nil {
				return nil, err
			}
			canons[i] = component.Canon{
				Kind:    component.CanonKindErrorContextDebugMsg,
				Options: opts,
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
		case component.CanonOptionKindUTF8, component.CanonOptionKindUTF16,
			component.CanonOptionKindLatin1, component.CanonOptionKindAsync:
			// No additional data
		case component.CanonOptionKindMemory, component.CanonOptionKindRealloc,
			component.CanonOptionKindPostReturn, component.CanonOptionKindCallback:
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
		name, url, err := decodeExternName(r)
		if err != nil {
			return nil, fmt.Errorf("read import name: %w", err)
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
		name, url, err := decodeExternName(r)
		if err != nil {
			return nil, fmt.Errorf("read export name: %w", err)
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
			URL:   url,
			Kind:  component.ExternDescKind(kind),
			Index: idx,
		}

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

// decodeExternName decodes a component model extern name (externname).
// Format: kind:byte name:string [url:string]
//   - 0x00: kebab-name (just a string)
//   - 0x01: URL (string is the URL)
//   - 0x02: kebab-name with URL integrity
//   - 0x03: hashed kebab-name
//   - 0x04: URL with integrity
func decodeExternName(r *bytes.Reader) (name string, url string, err error) {
	kind, err := r.ReadByte()
	if err != nil {
		return "", "", fmt.Errorf("read externname kind: %w", err)
	}

	switch kind {
	case 0x00: // kebab-name
		n, _, err := decodeUTF8(r, "externname")
		if err != nil {
			return "", "", err
		}
		return n, "", nil

	case 0x01: // URL
		u, _, err := decodeUTF8(r, "externname URL")
		if err != nil {
			return "", "", err
		}
		return u, u, nil

	case 0x02: // kebab-name + URL integrity
		n, _, err := decodeUTF8(r, "externname")
		if err != nil {
			return "", "", err
		}
		u, _, err := decodeUTF8(r, "externname URL")
		if err != nil {
			return "", "", err
		}
		return n, u, nil

	case 0x03: // hashed kebab-name
		n, _, err := decodeUTF8(r, "externname")
		if err != nil {
			return "", "", err
		}
		return n, "", nil

	case 0x04: // URL with integrity
		u, _, err := decodeUTF8(r, "externname URL")
		if err != nil {
			return "", "", err
		}
		integrity, _, err := decodeUTF8(r, "externname integrity")
		if err != nil {
			return "", "", err
		}
		_ = integrity
		return u, u, nil

	default:
		return "", "", fmt.Errorf("unknown externname kind: %#x", kind)
	}
}

// decodeExternDesc decodes a component external description (import/export type).
func decodeExternDesc(r *bytes.Reader) (component.ExternDesc, error) {
	kind, err := r.ReadByte()
	if err != nil {
		return component.ExternDesc{}, fmt.Errorf("read extern desc kind: %w", err)
	}

	desc := component.ExternDesc{Kind: component.ExternDescKind(kind)}

	switch component.ExternDescKind(kind) {
	case component.ExternDescKindModule:
		// Module extern desc has a sub-byte (0x11 for module type index).
		subKind, err := r.ReadByte()
		if err != nil {
			return desc, fmt.Errorf("read module extern desc sub-kind: %w", err)
		}
		_ = subKind // typically 0x11
		idx, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return desc, fmt.Errorf("read extern desc index: %w", err)
		}
		desc.TypeIndex = idx

	case component.ExternDescKindFunc,
		component.ExternDescKindValue,
		component.ExternDescKindComponent,
		component.ExternDescKindInstance:
		idx, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return desc, fmt.Errorf("read extern desc index: %w", err)
		}
		desc.TypeIndex = idx

	case component.ExternDescKindType:
		// Type extern desc uses type bounds: 0x00 idx (eq) or 0x01 (sub resource).
		bound, err := r.ReadByte()
		if err != nil {
			return desc, fmt.Errorf("read type bound: %w", err)
		}
		desc.TypeBound = component.TypeBound(bound)
		switch component.TypeBound(bound) {
		case component.TypeBoundEq:
			idx, _, err := leb128.DecodeUint32(r)
			if err != nil {
				return desc, fmt.Errorf("read type bound eq index: %w", err)
			}
			desc.TypeIndex = idx
		case component.TypeBoundSub:
			desc.IsSubResource = true
		default:
			return desc, fmt.Errorf("unknown type bound: %#x", bound)
		}

	default:
		return desc, fmt.Errorf("unknown extern desc kind: %#x", kind)
	}

	return desc, nil
}
