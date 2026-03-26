// Package runtime implements the WebAssembly Component Model instantiation engine.
// It processes decoded components, resolves imports, instantiates core modules,
// and wires up the canonical ABI to connect component-level and core-level functions.
package runtime

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
	"github.com/tetratelabs/wazero/internal/wasm/component"
	"github.com/tetratelabs/wazero/sys"
)

// HostFunc is a named function in a host instance.
type HostFunc struct {
	Name string
	Func api.GoModuleFunc
}

// HostInstance represents a host-provided component instance that satisfies an import.
type HostInstance struct {
	// FuncList is the ordered list of functions. The order must match the
	// instance type's function export order in the component type definition.
	FuncList []HostFunc
	// Funcs maps export names to host function implementations (for lookup).
	Funcs map[string]api.GoModuleFunc
	// Resources maps resource names to resource type IDs in the resource table.
	Resources map[string]uint32
}

// HostImports maps component import names to host-provided instances.
type HostImports map[string]*HostInstance

// Engine instantiates a component with the given host imports and runs it.
type Engine struct {
	rt  wazero.Runtime
	ctx context.Context

	// Index spaces - grow as sections are processed.
	coreModules   []*coreModuleEntry
	coreInstances []*coreInstanceEntry
	coreFuncs     []*coreFuncEntry
	coreMemories  []*coreMemoryEntry
	coreTables    []*coreTableEntry
	coreGlobals   []*coreGlobalEntry
	compInstances []*compInstanceEntry
	compFuncs     []*compFuncEntry
	compTypes     []interface{} // type index space
	resources     *ResourceTable

	// hostFuncMap maps function names to host-provided goFuncs.
	// Used to bypass shim modules whose indirect call tables can't be populated.
	hostFuncMap map[string]api.GoModuleFunc

	// preRegistered tracks module names already registered as host modules.
	preRegistered map[string]bool

	// Configuration.
	modConfig wazero.ModuleConfig

	// Module counter for unique names.
	moduleCounter int
}

type coreModuleEntry struct {
	decoded *wasm.Module
	raw     []byte
}

type coreInstanceEntry struct {
	// mod is the instantiated module (for Instantiate kind).
	mod api.Module
	// exports maps names to items for FromExports kind.
	exports map[string]*coreItemRef
}

type coreItemRef struct {
	kind     api.ExternType
	fn       api.Function
	goFunc   api.GoModuleFunc // for host-provided functions
	mem      api.Memory
	global   api.Global
	tableIdx int32 // index in coreTables, -1 if not set
}

type coreFuncEntry struct {
	goFunc api.GoModuleFunc
	fn     api.Function
}

type coreMemoryEntry struct {
	mem api.Memory
}

type coreTableEntry struct {
	// Table from a real module instance.
	fromMod     api.Module
	fromModName string // export name in the module
}

type coreGlobalEntry struct {
	global api.Global
}

type compInstanceEntry struct {
	exports map[string]*compItemRef
}

type compItemRef struct {
	kind    component.ExternDescKind
	funcIdx uint32
	typeIdx uint32
	instIdx uint32
}

type compFuncEntry struct {
	goFunc api.GoModuleFunc
	// For canon lift: the lifted core function.
	coreFuncIdx uint32
	typeIdx     uint32
	isLift      bool
}

// NewEngine creates a new component instantiation engine.
func NewEngine(ctx context.Context, rt wazero.Runtime, config wazero.ModuleConfig) *Engine {
	return &Engine{
		rt:        rt,
		ctx:       ctx,
		modConfig: config,
		resources: NewResourceTable(),
	}
}

// buildHostFuncMap builds a map of function name → goFunc from all host imports.
// This is used to resolve shim-wrapped functions directly to host implementations.
func (e *Engine) buildHostFuncMap(imports HostImports) {
	e.hostFuncMap = make(map[string]api.GoModuleFunc)
	for _, inst := range imports {
		for _, hf := range inst.FuncList {
			e.hostFuncMap[hf.Name] = hf.Func
		}
	}
}

// registerHostModule registers a host module to satisfy imports from a core module.
// If the from-exports instance contains table references, it builds a bridge wasm module
// that imports functions from a helper host module and the table from its owning module,
// then re-exports everything. This is necessary because NewHostModuleBuilder can't export tables.
func (e *Engine) registerHostModule(moduleName string, coreModEntry *coreModuleEntry, coreInst *coreInstanceEntry) error {
	if e.preRegistered[moduleName] {
		return nil
	}

	// Check if the from-exports instance has table references.
	hasTable := false
	if coreInst != nil && coreInst.exports != nil {
		for _, ref := range coreInst.exports {
			if ref.kind == api.ExternTypeTable && ref.tableIdx >= 0 {
				hasTable = true
				break
			}
		}
	}

	if hasTable {
		return e.registerBridgeModule(moduleName, coreModEntry, coreInst)
	}
	return e.registerFuncOnlyHostModule(moduleName, coreModEntry, coreInst)
}

// registerFuncOnlyHostModule registers a host module with only function exports.
func (e *Engine) registerFuncOnlyHostModule(moduleName string, coreModEntry *coreModuleEntry, coreInst *coreInstanceEntry) error {
	builder := e.rt.NewHostModuleBuilder(moduleName)
	hasExports := false

	if coreInst != nil && coreInst.exports != nil {
		for name, ref := range coreInst.exports {
			if ref.kind != api.ExternTypeFunc {
				continue
			}
			params, results := e.findImportSig(coreModEntry.decoded, moduleName, name)
			goFunc := e.resolveGoFunc(name, ref, params)

			builder.NewFunctionBuilder().
				WithGoModuleFunction(goFunc, params, results).
				Export(name)
			hasExports = true
		}
	}

	// Also add stubs for functions imported but not in from-exports.
	if coreModEntry != nil {
		for i := range coreModEntry.decoded.ImportSection {
			imp := &coreModEntry.decoded.ImportSection[i]
			if imp.Type != 0 || imp.Module != moduleName {
				continue
			}
			if coreInst != nil && coreInst.exports != nil {
				if _, exists := coreInst.exports[imp.Name]; exists {
					continue
				}
			}
			if int(imp.DescFunc) < len(coreModEntry.decoded.TypeSection) {
				ft := &coreModEntry.decoded.TypeSection[imp.DescFunc]
				var goFunc api.GoModuleFunc
				if hf, ok := e.hostFuncMap[imp.Name]; ok {
					goFunc = hf
				} else {
					goFunc = func(_ context.Context, _ api.Module, _ []uint64) {}
				}
				builder.NewFunctionBuilder().
					WithGoModuleFunction(goFunc, ft.Params, ft.Results).
					Export(imp.Name)
				hasExports = true
			}
		}
	}

	if !hasExports {
		return nil
	}

	if _, err := builder.Instantiate(e.ctx); err != nil {
		return err
	}
	e.preRegistered[moduleName] = true
	return nil
}

// resolveGoFunc resolves a Go function for a core item reference.
func (e *Engine) resolveGoFunc(name string, ref *coreItemRef, params []api.ValueType) api.GoModuleFunc {
	if hf, ok := e.hostFuncMap[name]; ok {
		return hf
	}
	if ref.goFunc != nil {
		return ref.goFunc
	}
	if ref.fn != nil {
		origFn := ref.fn
		nParams := len(params)
		return func(ctx context.Context, mod api.Module, stack []uint64) {
			retVals, err := origFn.Call(ctx, stack[:nParams]...)
			if err != nil {
				panic(err)
			}
			copy(stack, retVals)
		}
	}
	return func(_ context.Context, _ api.Module, _ []uint64) {}
}

// bridgeFuncInfo describes a function to include in a bridge module.
type bridgeFuncInfo struct {
	name    string
	params  []api.ValueType
	results []api.ValueType
	goFunc  api.GoModuleFunc
}

// registerBridgeModule populates a table directly with host function references,
// bypassing the need for the fixup module which can't be instantiated because
// wazero rejects empty module names in imports.
func (e *Engine) registerBridgeModule(moduleName string, coreModEntry *coreModuleEntry, coreInst *coreInstanceEntry) error {
	// Collect functions from the from-exports instance.
	var funcs []bridgeFuncInfo
	for name, ref := range coreInst.exports {
		if ref.kind != api.ExternTypeFunc {
			continue
		}
		params, results := e.findImportSig(coreModEntry.decoded, moduleName, name)
		goFunc := e.resolveGoFunc(name, ref, params)
		funcs = append(funcs, bridgeFuncInfo{name: name, params: params, results: results, goFunc: goFunc})
	}

	// Wrap each goFunc to substitute the memory module when the caller has no memory.
	// This is needed because call_indirect through the shim passes the shim's
	// module instance (no memory) as mod, but host functions need memory access.
	for idx := range funcs {
		origGoFunc := funcs[idx].goFunc
		funcs[idx].goFunc = func(ctx context.Context, mod api.Module, stack []uint64) {
			callMod := mod
			if !moduleHasMemory(mod) {
				if memMod := e.findModuleWithMemory(); memMod != nil {
					callMod = memMod
				}
			}
			origGoFunc(ctx, callMod, stack)
		}
	}

	// Sort functions by name for deterministic ordering.
	sort.Slice(funcs, func(i, j int) bool {
		ni, ei := strconv.Atoi(funcs[i].name)
		nj, ej := strconv.Atoi(funcs[j].name)
		if ei == nil && ej == nil {
			return ni < nj
		}
		return funcs[i].name < funcs[j].name
	})

	// Find the table.
	var tableEntry *coreTableEntry
	for _, ref := range coreInst.exports {
		if ref.kind == api.ExternTypeTable && ref.tableIdx >= 0 {
			if int(ref.tableIdx) < len(e.coreTables) {
				tableEntry = e.coreTables[ref.tableIdx]
			}
			break
		}
	}
	if tableEntry == nil || tableEntry.fromMod == nil {
		return e.registerFuncOnlyHostModule(moduleName, coreModEntry, coreInst)
	}

	// Create a helper host module with the functions so they get compiled into
	// callable wasm function references.
	e.moduleCounter++
	helperName := fmt.Sprintf("__bridge_%d", e.moduleCounter)
	builder := e.rt.NewHostModuleBuilder(helperName)
	for _, f := range funcs {
		builder.NewFunctionBuilder().
			WithGoModuleFunction(f.goFunc, f.params, f.results).
			Export(f.name)
	}
	helperMod, err := builder.Instantiate(e.ctx)
	if err != nil {
		return fmt.Errorf("instantiate bridge helper %q: %w", helperName, err)
	}

	// Get the helper module's internal instance to access function references.
	helperModInst := toModuleInstance(helperMod)

	// Get the shim module's internal table.
	shimModInst := toModuleInstance(tableEntry.fromMod)
	tableExport, ok := shimModInst.Exports[tableEntry.fromModName]
	if !ok {
		return fmt.Errorf("table export %q not found in module %s", tableEntry.fromModName, tableEntry.fromMod.Name())
	}
	table := shimModInst.Tables[tableExport.Index]

	fmt.Printf("[DEBUG] registerBridgeModule: helperModInst=%v shimModInst=%v table=%v (size=%d)\n",
		helperModInst != nil, shimModInst != nil, table != nil, len(table.References))

	// Write function references directly into the table at numeric slot positions.
	written := 0
	for _, f := range funcs {
		slot, err := strconv.Atoi(f.name)
		if err != nil {
			continue // skip non-numeric names (like "$imports")
		}
		exp, ok := helperModInst.Exports[f.name]
		if !ok || exp.Type != api.ExternTypeFunc {
			fmt.Printf("[DEBUG] registerBridgeModule: func %q not found in helper exports\n", f.name)
			continue
		}
		ref := helperModInst.Engine.FunctionInstanceReference(exp.Index)
		if slot >= 0 && slot < len(table.References) {
			table.References[slot] = ref
			written++
			fmt.Printf("[DEBUG] registerBridgeModule: wrote slot %d ref=%d\n", slot, ref)
		}
	}
	fmt.Printf("[DEBUG] registerBridgeModule: wrote %d references into table\n", written)

	// Mark as pre-registered. The fixup module instantiation will fail (empty module name
	// rejected by wazero), but that's OK since we've already populated the table.
	e.preRegistered[moduleName] = true
	return nil
}

// Instantiate processes the component's sections in order, resolves imports,
// instantiates core modules, and returns the component instance.
func (e *Engine) Instantiate(comp *component.Component, imports HostImports) error {
	e.buildHostFuncMap(imports)
	e.preRegistered = make(map[string]bool)

	for _, entry := range comp.SectionOrder {
		var err error
		switch entry.SectionID {
		case component.SectionIDCoreModule:
			err = e.processCoreModule(comp, entry)
		case component.SectionIDImport:
			err = e.processImports(comp, entry, imports)
		case component.SectionIDType:
			err = e.processTypes(comp, entry)
		case component.SectionIDAlias:
			err = e.processAliases(comp, entry)
		case component.SectionIDCanon:
			err = e.processCanons(comp, entry)
		case component.SectionIDCoreInstance:
			err = e.processCoreInstances(comp, entry)
		case component.SectionIDInstance:
			err = e.processInstances(comp, entry)
		case component.SectionIDExport:
			// Exports don't add to index spaces used during instantiation.
		case component.SectionIDComponent:
			// Nested components stored for later.
		case component.SectionIDCoreType:
			// Core types extend type space.
		case component.SectionIDStart:
			// Processed after all sections.
		}
		if err != nil {
			return fmt.Errorf("section %s: %w",
				component.SectionIDName(entry.SectionID), err)
		}
	}
	return nil
}

func (e *Engine) processCoreModule(comp *component.Component, entry component.SectionOrderEntry) error {
	for i := 0; i < entry.Count; i++ {
		idx := entry.Index + i
		mod := comp.CoreModules[idx]
		var raw []byte
		if idx < len(comp.CoreModuleBytes) {
			raw = comp.CoreModuleBytes[idx]
		}
		e.coreModules = append(e.coreModules, &coreModuleEntry{decoded: mod, raw: raw})
	}
	return nil
}

func (e *Engine) processImports(comp *component.Component, entry component.SectionOrderEntry, imports HostImports) error {
	for i := 0; i < entry.Count; i++ {
		imp := comp.Imports[entry.Index+i]
		hostInst, ok := imports[imp.Name]
		if !ok {
			hostInst = &HostInstance{
				Funcs:     map[string]api.GoModuleFunc{},
				Resources: map[string]uint32{},
			}
		}

		switch imp.Desc.Kind {
		case component.ExternDescKindInstance:
			ci := &compInstanceEntry{
				exports: make(map[string]*compItemRef),
			}
			// Add functions in order from FuncList (order matters for comp func indices).
			for _, hf := range hostInst.FuncList {
				funcIdx := len(e.compFuncs)
				e.compFuncs = append(e.compFuncs, &compFuncEntry{goFunc: hf.Func})
				ci.exports[hf.Name] = &compItemRef{
					kind:    component.ExternDescKindFunc,
					funcIdx: uint32(funcIdx),
				}
			}
			for name, typeID := range hostInst.Resources {
				typeIdx := len(e.compTypes)
				e.compTypes = append(e.compTypes, typeID)
				ci.exports[name] = &compItemRef{
					kind:    component.ExternDescKindType,
					typeIdx: uint32(typeIdx),
				}
			}
			e.compInstances = append(e.compInstances, ci)

		case component.ExternDescKindFunc:
			if fn, ok := hostInst.Funcs[""]; ok {
				e.compFuncs = append(e.compFuncs, &compFuncEntry{goFunc: fn})
			} else if len(hostInst.FuncList) > 0 {
				e.compFuncs = append(e.compFuncs, &compFuncEntry{goFunc: hostInst.FuncList[0].Func})
			} else {
				e.compFuncs = append(e.compFuncs, &compFuncEntry{})
			}

		case component.ExternDescKindType:
			e.compTypes = append(e.compTypes, nil)

		default:
			e.compTypes = append(e.compTypes, nil)
		}
	}
	return nil
}

func (e *Engine) processTypes(comp *component.Component, entry component.SectionOrderEntry) error {
	for i := 0; i < entry.Count; i++ {
		e.compTypes = append(e.compTypes, &comp.Types[entry.Index+i])
	}
	return nil
}

func (e *Engine) processAliases(comp *component.Component, entry component.SectionOrderEntry) error {
	for i := 0; i < entry.Count; i++ {
		alias := comp.Aliases[entry.Index+i]
		switch {
		case alias.Kind == component.AliasKindInstanceExport && alias.Sort == component.AliasSortFunc:
			if int(alias.InstanceIndex) < len(e.compInstances) {
				inst := e.compInstances[alias.InstanceIndex]
				if ref, ok := inst.exports[alias.Name]; ok && ref.kind == component.ExternDescKindFunc {
					e.compFuncs = append(e.compFuncs, e.compFuncs[ref.funcIdx])
				} else {
					e.compFuncs = append(e.compFuncs, &compFuncEntry{})
				}
			} else {
				e.compFuncs = append(e.compFuncs, &compFuncEntry{})
			}

		case alias.Kind == component.AliasKindInstanceExport && alias.Sort == component.AliasSortType:
			if int(alias.InstanceIndex) < len(e.compInstances) {
				inst := e.compInstances[alias.InstanceIndex]
				if ref, ok := inst.exports[alias.Name]; ok {
					e.compTypes = append(e.compTypes, ref)
				} else {
					e.compTypes = append(e.compTypes, nil)
				}
			} else {
				e.compTypes = append(e.compTypes, nil)
			}

		case alias.Kind == component.AliasKindCoreInstanceExport:
			if int(alias.InstanceIndex) < len(e.coreInstances) {
				inst := e.coreInstances[alias.InstanceIndex]
				switch alias.Sort {
				case component.AliasSortCoreFunc:
					if inst.mod != nil {
						fn := inst.mod.ExportedFunction(alias.Name)
						e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{fn: fn})
					} else if ref, ok := inst.exports[alias.Name]; ok {
						e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{fn: ref.fn, goFunc: ref.goFunc})
					} else {
						e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{})
					}
				case component.AliasSortCoreMemory:
					if inst.mod != nil {
						e.coreMemories = append(e.coreMemories, &coreMemoryEntry{mem: inst.mod.Memory()})
					} else {
						e.coreMemories = append(e.coreMemories, &coreMemoryEntry{})
					}
				case component.AliasSortCoreTable:
					if inst.mod != nil {
						e.coreTables = append(e.coreTables, &coreTableEntry{
							fromMod:     inst.mod,
							fromModName: alias.Name,
						})
					} else {
						e.coreTables = append(e.coreTables, &coreTableEntry{})
					}
				default:
					// Other core sorts.
				}
			}

		case alias.Kind == component.AliasKindOuter:
			switch alias.Sort {
			case component.AliasSortType:
				e.compTypes = append(e.compTypes, nil)
			default:
				// Other outer alias sorts.
			}

		case alias.Kind == component.AliasKindInstanceExport:
			switch alias.Sort {
			case component.AliasSortInstance:
				if int(alias.InstanceIndex) < len(e.compInstances) {
					inst := e.compInstances[alias.InstanceIndex]
					if ref, ok := inst.exports[alias.Name]; ok && ref.kind == component.ExternDescKindInstance {
						if int(ref.instIdx) < len(e.compInstances) {
							e.compInstances = append(e.compInstances, e.compInstances[ref.instIdx])
						} else {
							e.compInstances = append(e.compInstances, &compInstanceEntry{exports: make(map[string]*compItemRef)})
						}
					} else {
						e.compInstances = append(e.compInstances, &compInstanceEntry{exports: make(map[string]*compItemRef)})
					}
				}
			default:
				// Component/other alias sorts.
			}
		}
	}
	return nil
}

func (e *Engine) processCanons(comp *component.Component, entry component.SectionOrderEntry) error {
	for i := 0; i < entry.Count; i++ {
		canon := comp.Canons[entry.Index+i]
		switch canon.Kind {
		case component.CanonKindLift:
			e.compFuncs = append(e.compFuncs, &compFuncEntry{
				coreFuncIdx: canon.CoreFuncIndex,
				typeIdx:     canon.TypeIndex,
				isLift:      true,
			})

		case component.CanonKindLower:
			e.coreFuncs = append(e.coreFuncs, e.lowerFunc(canon))

		case component.CanonKindResourceNew:
			typeIdx := canon.ResourceTypeIndex
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
					rep := api.DecodeI32(stack[0])
					handle := e.resources.New(typeIdx, rep)
					stack[0] = api.EncodeI32(handle)
				},
			})

		case component.CanonKindResourceDrop:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
					handle := api.DecodeI32(stack[0])
					e.resources.Drop(handle)
				},
			})

		case component.CanonKindResourceRep:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
					handle := api.DecodeI32(stack[0])
					rep := e.resources.Rep(handle)
					stack[0] = api.EncodeI32(rep)
				},
			})

		case component.CanonKindTaskReturn:
			e.coreFuncs = append(e.coreFuncs, e.makeTaskReturn())

		case component.CanonKindTaskCancel:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
					stack[0] = 0
				},
			})

		case component.CanonKindContextGet:
			idx := canon.ContextIndex
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
					stack[0] = api.EncodeI32(e.resources.ContextGet(idx))
				},
			})

		case component.CanonKindContextSet:
			idx := canon.ContextIndex
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
					val := api.DecodeI32(stack[0])
					e.resources.ContextSet(idx, val)
				},
			})

		case component.CanonKindYield:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, _ []uint64) {},
			})

		case component.CanonKindSubtaskDrop:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, _ []uint64) {},
			})

		case component.CanonKindSubtaskCancel:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
					stack[0] = 0
				},
			})

		case component.CanonKindBackpressureSet:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, _ []uint64) {},
			})

		case component.CanonKindWaitableSetNew:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
					stack[0] = api.EncodeI32(e.resources.WaitableSetNew())
				},
			})

		case component.CanonKindWaitableSetWait, component.CanonKindWaitableSetPoll:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
					stack[0] = 0
				},
			})

		case component.CanonKindWaitableSetDrop:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, _ []uint64) {},
			})

		case component.CanonKindWaitableJoin:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, _ []uint64) {},
			})

		case component.CanonKindStreamNew, component.CanonKindFutureNew:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
					stack[0] = api.EncodeI32(e.resources.StreamNew())
				},
			})

		case component.CanonKindStreamRead, component.CanonKindStreamWrite,
			component.CanonKindFutureRead, component.CanonKindFutureWrite:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
					stack[0] = 1 // BLOCKED
				},
			})

		case component.CanonKindStreamCancelRead, component.CanonKindStreamCancelWrite,
			component.CanonKindFutureCancelRead, component.CanonKindFutureCancelWrite:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
					stack[0] = 0
				},
			})

		case component.CanonKindStreamDropReadable, component.CanonKindStreamDropWritable,
			component.CanonKindFutureDropReadable, component.CanonKindFutureDropWritable:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, _ []uint64) {},
			})

		case component.CanonKindErrorContextNew:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
					stack[0] = api.EncodeI32(e.resources.ErrorContextNew())
				},
			})

		case component.CanonKindErrorContextDebugMsg:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
					stack[0] = 0
				},
			})

		case component.CanonKindErrorContextDrop:
			e.coreFuncs = append(e.coreFuncs, &coreFuncEntry{
				goFunc: func(_ context.Context, _ api.Module, _ []uint64) {},
			})

		default:
			return fmt.Errorf("unhandled canon kind: %#x", canon.Kind)
		}
	}
	return nil
}

func (e *Engine) lowerFunc(canon component.Canon) *coreFuncEntry {
	funcIdx := canon.FuncIndex
	return &coreFuncEntry{
		goFunc: func(ctx context.Context, mod api.Module, stack []uint64) {
			if int(funcIdx) < len(e.compFuncs) {
				cf := e.compFuncs[funcIdx]
				if cf.goFunc != nil {
					// Use the module with memory if mod doesn't have one.
					// This is needed because lowered functions may be called
					// via call_indirect through a shim module that has no memory.
					callMod := mod
					if !moduleHasMemory(mod) {
						if memMod := e.findModuleWithMemory(); memMod != nil {
							callMod = memMod
						}
					}
					cf.goFunc(ctx, callMod, stack)
				}
			}
		},
	}
}

func (e *Engine) makeTaskReturn() *coreFuncEntry {
	return &coreFuncEntry{
		goFunc: func(_ context.Context, _ api.Module, stack []uint64) {
			e.resources.SetTaskReturn(stack)
		},
	}
}

func (e *Engine) processCoreInstances(comp *component.Component, entry component.SectionOrderEntry) error {
	for i := 0; i < entry.Count; i++ {
		ci := comp.CoreInstances[entry.Index+i]
		switch ci.Kind {
		case component.CoreInstanceKindInstantiate:
			inst, err := e.instantiateCoreModule(comp, ci)
			if err != nil {
				fmt.Printf("[DEBUG] processCoreInstances: instantiate failed (module_idx=%d): %v\n", ci.ModuleIndex, err)
				// Non-fatal: some modules (e.g., indirect call shims) may require
				// features we don't support (tables). Add a placeholder and continue.
				e.coreInstances = append(e.coreInstances, &coreInstanceEntry{
					exports: make(map[string]*coreItemRef),
				})
				continue
			}
			e.coreInstances = append(e.coreInstances, inst)

		case component.CoreInstanceKindFromExports:
			inst := &coreInstanceEntry{
				exports: make(map[string]*coreItemRef),
			}
			for _, exp := range ci.Exports {
				ref := &coreItemRef{kind: exp.Kind, tableIdx: -1}
				switch exp.Kind {
				case api.ExternTypeFunc:
					if int(exp.Index) < len(e.coreFuncs) {
						cf := e.coreFuncs[exp.Index]
						ref.fn = cf.fn
						ref.goFunc = cf.goFunc
					}
				case api.ExternTypeMemory:
					if int(exp.Index) < len(e.coreMemories) {
						ref.mem = e.coreMemories[exp.Index].mem
					}
				case api.ExternTypeTable:
					ref.tableIdx = int32(exp.Index)
				case api.ExternTypeGlobal:
					if int(exp.Index) < len(e.coreGlobals) {
						ref.global = e.coreGlobals[exp.Index].global
					}
				}
				inst.exports[exp.Name] = ref
			}
			e.coreInstances = append(e.coreInstances, inst)
		}
	}
	return nil
}

// findImportSig looks up a function's signature from the core module's import section.
func (e *Engine) findImportSig(mod *wasm.Module, moduleName, funcName string) (params, results []api.ValueType) {
	for i := range mod.ImportSection {
		imp := &mod.ImportSection[i]
		if imp.Module == moduleName && imp.Name == funcName && imp.Type == wasm.ExternTypeFunc {
			if int(imp.DescFunc) < len(mod.TypeSection) {
				ft := &mod.TypeSection[imp.DescFunc]
				return ft.Params, ft.Results
			}
		}
	}
	return nil, nil
}

func (e *Engine) instantiateCoreModule(comp *component.Component, ci component.CoreInstance) (*coreInstanceEntry, error) {
	if int(ci.ModuleIndex) >= len(e.coreModules) {
		return nil, fmt.Errorf("module index %d out of range", ci.ModuleIndex)
	}
	coreModEntry := e.coreModules[ci.ModuleIndex]

	// Register host modules for each import argument using from-exports instances.
	for _, arg := range ci.Args {
		if int(arg.Index) >= len(e.coreInstances) {
			continue
		}
		coreInst := e.coreInstances[arg.Index]
		if err := e.registerHostModule(arg.Name, coreModEntry, coreInst); err != nil {
			return nil, fmt.Errorf("register host module %q: %w", arg.Name, err)
		}
	}

	// Instantiate the core module from raw bytes.
	if coreModEntry.raw == nil {
		return nil, fmt.Errorf("no raw bytes for core module %d", ci.ModuleIndex)
	}

	e.moduleCounter++
	modName := fmt.Sprintf("core%d", e.moduleCounter)

	fmt.Printf("[DEBUG] instantiateCoreModule: module_idx=%d name=%s imports_from:", ci.ModuleIndex, modName)
	for _, imp := range coreModEntry.decoded.ImportSection {
		fmt.Printf(" %s.%s(%d)", imp.Module, imp.Name, imp.Type)
	}
	fmt.Println()

	mod, err := e.rt.InstantiateWithConfig(e.ctx, coreModEntry.raw,
		e.modConfig.WithName(modName).WithStartFunctions())
	if err != nil {
		return nil, fmt.Errorf("instantiate core module %d (%s): %w", ci.ModuleIndex, modName, err)
	}

	fmt.Printf("[DEBUG] instantiateCoreModule: module %d (%s) instantiated successfully\n", ci.ModuleIndex, modName)

	// Debug: check table state on shim module after fixup
	for _, cInst := range e.coreInstances {
		if cInst.mod != nil {
			mi := cInst.mod.(*wasm.ModuleInstance)
			for tIdx, t := range mi.Tables {
				nonZero := 0
				for _, ref := range t.References {
					if ref != 0 {
						nonZero++
					}
				}
				if len(t.References) > 0 {
					fmt.Printf("[DEBUG] table check: module %s table[%d] size=%d nonzero=%d\n",
						cInst.mod.Name(), tIdx, len(t.References), nonZero)
				}
			}
		}
	}

	return &coreInstanceEntry{mod: mod}, nil
}

func (e *Engine) processInstances(comp *component.Component, entry component.SectionOrderEntry) error {
	for i := 0; i < entry.Count; i++ {
		inst := comp.Instances[entry.Index+i]
		switch inst.Kind {
		case component.InstanceKindInstantiate:
			ci := &compInstanceEntry{exports: make(map[string]*compItemRef)}
			e.compInstances = append(e.compInstances, ci)

		case component.InstanceKindFromExports:
			ci := &compInstanceEntry{exports: make(map[string]*compItemRef)}
			for _, exp := range inst.Exports {
				ci.exports[exp.Name] = &compItemRef{kind: exp.Kind}
				switch exp.Kind {
				case component.ExternDescKindFunc:
					ci.exports[exp.Name].funcIdx = exp.Index
				case component.ExternDescKindInstance:
					ci.exports[exp.Name].instIdx = exp.Index
				case component.ExternDescKindType:
					ci.exports[exp.Name].typeIdx = exp.Index
				}
			}
			e.compInstances = append(e.compInstances, ci)
		}
	}
	return nil
}

// CallStart calls the entry point of the component.
// For P3 async components, it calls the [async-lift] entry point which drives
// the async state machine. For P2 components, it falls back to _start.
func (e *Engine) CallStart() error {
	// P3 async entry points have priority: they call start_task which
	// immediately drives the async state machine via callback(0,0,0).
	// The P2-compatible _start/__main_void in P3 components is a stub that panics.
	asyncEntryPoints := []string{
		"[async-lift]wasi:cli/run@0.3.0-rc-2026-02-09#run",
	}
	p2EntryPoints := []string{
		"_start",
		"__main_void",
		"wasi:cli/run@0.2.0#run",
	}

	// Debug: dump table state before calling entry point.
	for _, inst := range e.coreInstances {
		if inst.mod == nil {
			continue
		}
		mi := toModuleInstance(inst.mod)
		if mi != nil {
			for tIdx, t := range mi.Tables {
				nonZero := 0
				for _, ref := range t.References {
					if ref != 0 {
						nonZero++
					}
				}
				if len(t.References) > 0 {
					fmt.Printf("[DEBUG] CallStart table: module %s table[%d] size=%d nonzero=%d\n",
						inst.mod.Name(), tIdx, len(t.References), nonZero)
				}
			}
		}
	}

	// First try P3 async entry points.
	for _, inst := range e.coreInstances {
		if inst.mod == nil {
			continue
		}
		for _, name := range asyncEntryPoints {
			fn := inst.mod.ExportedFunction(name)
			if fn != nil {
				_, err := fn.Call(e.ctx)
				if err != nil {
					return err
				}
				// Check task-return values. For wasi:cli/run, a result<_, _>
				// has discriminant 0=ok, 1=err. If err, signal exit code 1.
				retVals := e.resources.GetTaskReturn()
				if len(retVals) > 0 && retVals[0] == 1 {
					return sys.NewExitError(1)
				}
				return nil
			}
		}
	}

	// Fall back to P2 entry points.
	for _, inst := range e.coreInstances {
		if inst.mod == nil {
			continue
		}
		for _, name := range p2EntryPoints {
			fn := inst.mod.ExportedFunction(name)
			if fn != nil {
				_, err := fn.Call(e.ctx)
				return err
			}
		}
	}
	return nil
}

// findModuleWithMemory returns the first core module instance that has
// linear memory and a cabi_realloc function. This is typically the main
// user code module. Falls back to any module with memory.
func (e *Engine) findModuleWithMemory() api.Module {
	for _, inst := range e.coreInstances {
		if moduleHasMemory(inst.mod) {
			if inst.mod.ExportedFunction("cabi_realloc") != nil {
				return inst.mod
			}
		}
	}
	for _, inst := range e.coreInstances {
		if moduleHasMemory(inst.mod) {
			return inst.mod
		}
	}
	return nil
}

// Close releases all resources.
func (e *Engine) Close() error {
	return nil
}

// moduleHasMemory checks whether a module has a usable linear memory.
// This handles the Go nil interface gotcha where Memory() returns a non-nil
// api.Memory wrapping a nil *MemoryInstance.
func moduleHasMemory(mod api.Module) bool {
	if mod == nil {
		return false
	}
	mem := mod.Memory()
	if mem == nil {
		return false
	}
	v := reflect.ValueOf(mem)
	return !v.IsNil()
}

// toModuleInstance unwraps an api.Module to the underlying *wasm.ModuleInstance.
// Host modules are wrapped in a hostModuleInstance struct (which embeds api.Module),
// so we use reflection to extract the embedded *wasm.ModuleInstance.
func toModuleInstance(m api.Module) *wasm.ModuleInstance {
	if mi, ok := m.(*wasm.ModuleInstance); ok {
		return mi
	}
	// Host modules are wrapped: hostModuleInstance{api.Module} where the
	// embedded field is *wasm.ModuleInstance. Use reflection to extract it.
	v := reflect.ValueOf(m)
	if v.Kind() == reflect.Struct && v.NumField() > 0 {
		inner := v.Field(0)
		if inner.CanInterface() {
			if mi, ok := inner.Interface().(*wasm.ModuleInstance); ok {
				return mi
			}
		}
	}
	return nil
}
