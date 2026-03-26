package wazero

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm/component"
	componentBinary "github.com/tetratelabs/wazero/internal/wasm/component/binary"
)

// CompiledComponent is a WebAssembly Component that has been decoded and validated.
// Unlike core modules, components use the Component Model type system and can
// contain nested modules, canonical ABI functions, and interface-typed imports/exports.
type CompiledComponent interface {
	// Name returns the component's name, if set.
	Name() string

	// Imports returns the component's imports.
	Imports() []ComponentImportType

	// Exports returns the component's exports.
	Exports() []ComponentExportType

	// Close releases any resources associated with this compiled component.
	Close(context.Context) error
}

// ComponentImportType describes a component import.
type ComponentImportType struct {
	// Name is the import name (may be a kebab-case name or an interface URL).
	Name string

	// Kind is the kind of item being imported.
	Kind string
}

// ComponentExportType describes a component export.
type ComponentExportType struct {
	// Name is the export name.
	Name string

	// Kind is the kind of item being exported.
	Kind string
}

// ComponentInstance represents an instantiated component.
type ComponentInstance interface {
	// Name returns the instance name.
	Name() string

	// Close closes this component instance and all its resources.
	Close(context.Context) error
}

// CompileComponent decodes and validates a WebAssembly Component binary.
// This is analogous to Runtime.CompileModule but for the Component Model.
func (r *runtime) CompileComponent(ctx context.Context, binary []byte) (CompiledComponent, error) {
	if err := r.failIfClosed(); err != nil {
		return nil, err
	}

	comp, err := componentBinary.DecodeComponent(binary, r.enabledFeatures,
		r.memoryLimitPages, r.memoryCapacityFromMax)
	if err != nil {
		return nil, fmt.Errorf("component decode: %w", err)
	}

	return &compiledComponent{component: comp}, nil
}

// InstantiateComponent instantiates a compiled component with default configuration.
func (r *runtime) InstantiateComponent(ctx context.Context, compiled CompiledComponent) (ComponentInstance, error) {
	return r.InstantiateComponentWithConfig(ctx, compiled, NewModuleConfig())
}

// InstantiateComponentWithConfig instantiates a compiled component with the given configuration.
func (r *runtime) InstantiateComponentWithConfig(
	ctx context.Context,
	compiled CompiledComponent,
	config ModuleConfig,
) (ComponentInstance, error) {
	if err := r.failIfClosed(); err != nil {
		return nil, err
	}

	cc := compiled.(*compiledComponent)
	comp := cc.component

	inst := &componentInstance{
		name:      cc.name,
		component: comp,
		modules:   make(map[string]api.Module),
	}

	// Instantiate all core modules contained in the component.
	for i, coreMod := range comp.CoreModules {
		// Build memory definitions before compilation.
		coreMod.BuildMemoryDefinitions()

		// Get type IDs for the module.
		typeIDs, err := r.store.GetFunctionTypeIDs(coreMod.TypeSection)
		if err != nil {
			return nil, fmt.Errorf("core module %d type IDs: %w", i, err)
		}

		// Compile the core module.
		cm := &compiledModule{module: coreMod, compiledEngine: r.store.Engine, typeIDs: typeIDs}
		coreMod.AssignModuleID(nil, nil, r.ensureTermination)
		if err := r.store.Engine.CompileModule(ctx, coreMod, nil, r.ensureTermination); err != nil {
			return nil, fmt.Errorf("compile core module %d: %w", i, err)
		}

		// Set module name.
		moduleName := fmt.Sprintf("%s$core%d", inst.name, i)

		mc := config.(*moduleConfig)
		modConfig := mc.clone()
		modConfig.name = moduleName

		// Instantiate the core module.
		mod, err := r.InstantiateModule(ctx, cm, modConfig)
		if err != nil {
			return nil, fmt.Errorf("instantiate core module %d: %w", i, err)
		}

		inst.modules[moduleName] = mod
	}

	return inst, nil
}

// compiledComponent implements CompiledComponent.
type compiledComponent struct {
	component *component.Component
	name      string
}

func (c *compiledComponent) Name() string { return c.name }

func (c *compiledComponent) Imports() []ComponentImportType {
	result := make([]ComponentImportType, len(c.component.Imports))
	for i, imp := range c.component.Imports {
		result[i] = ComponentImportType{
			Name: imp.Name,
			Kind: component.ExternDescKindName(imp.Desc.Kind),
		}
	}
	return result
}

func (c *compiledComponent) Exports() []ComponentExportType {
	result := make([]ComponentExportType, len(c.component.Exports))
	for i, exp := range c.component.Exports {
		result[i] = ComponentExportType{
			Name: exp.Name,
			Kind: component.ExternDescKindName(exp.Kind),
		}
	}
	return result
}

func (c *compiledComponent) Close(_ context.Context) error { return nil }

// componentInstance implements ComponentInstance.
type componentInstance struct {
	name      string
	component *component.Component
	modules   map[string]api.Module
}

func (i *componentInstance) Name() string { return i.name }

func (i *componentInstance) Close(ctx context.Context) error {
	var firstErr error
	for _, mod := range i.modules {
		if err := mod.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
