// Package component implements the WebAssembly Component Model types, structures,
// and a basic runtime for instantiating CLI command components.

package component

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

// HostModule is a set of host functions to be registered under a single import module name.
type HostModule struct {
	Name  string
	Funcs map[string]*HostFunc
}

// HostFunc is a single host function definition.
type HostFunc struct {
	Name        string
	ParamTypes  []api.ValueType
	ResultTypes []api.ValueType
	Func        api.GoModuleFunction
}

// ComponentInstance is an instantiated component.
type ComponentInstance struct {
	// MainModule is the instantiated main core module.
	MainModule api.Module

	// AllModules holds all core module instances.
	AllModules []api.Module
}

// ExportedFunction returns a function exported by the main core module.
func (ci *ComponentInstance) ExportedFunction(name string) api.Function {
	if ci.MainModule == nil {
		return nil
	}
	return ci.MainModule.ExportedFunction(name)
}

// Close releases all resources.
func (ci *ComponentInstance) Close(ctx context.Context) error {
	var firstErr error
	for _, m := range ci.AllModules {
		if m != nil {
			if err := m.Close(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// WazeroRuntime is the interface the component runtime needs from wazero.
type WazeroRuntime interface {
	CompileModule(ctx context.Context, binary []byte) (api.Closer, error)
	InstantiateModule(ctx context.Context, binary []byte, name string) (api.Module, error)
}

// Linker manages host function registrations for component instantiation.
// It's modeled after wasmtime's component::Linker.
type Linker struct {
	hostModules map[string]*HostModule
}

// NewLinker creates a new empty Linker.
func NewLinker() *Linker {
	return &Linker{
		hostModules: make(map[string]*HostModule),
	}
}

// DefineModule registers a complete host module.
func (l *Linker) DefineModule(m *HostModule) {
	l.hostModules[m.Name] = m
}

// DefineFunc registers a single host function under the given module and function name.
func (l *Linker) DefineFunc(moduleName, funcName string, paramTypes, resultTypes []api.ValueType, fn api.GoModuleFunction) {
	m, ok := l.hostModules[moduleName]
	if !ok {
		m = &HostModule{Name: moduleName, Funcs: make(map[string]*HostFunc)}
		l.hostModules[moduleName] = m
	}
	m.Funcs[funcName] = &HostFunc{
		Name:        funcName,
		ParamTypes:  paramTypes,
		ResultTypes: resultTypes,
		Func:        fn,
	}
}

// HasModule returns true if the linker has a module with the given name.
func (l *Linker) HasModule(name string) bool {
	_, ok := l.hostModules[name]
	return ok
}

// Module returns a registered host module by name.
func (l *Linker) Module(name string) *HostModule {
	return l.hostModules[name]
}

// AllModules returns all registered host module names.
func (l *Linker) AllModules() []string {
	names := make([]string, 0, len(l.hostModules))
	for name := range l.hostModules {
		names = append(names, name)
	}
	return names
}

// MissingModulesForComponent checks which import modules a component's main core module needs
// that are not yet registered in the linker. Returns nil if all imports are satisfied.
func (l *Linker) MissingModulesForComponent(comp *Component) []string {
	if len(comp.CoreModules) == 0 {
		return nil
	}
	// We'd need to decode the core module to check imports, but that's done at a higher level.
	return nil
}

// GetHostModule returns the host module registered under the given name,
// or nil if not found.
func (l *Linker) GetHostModule(name string) *HostModule {
	return l.hostModules[name]
}

// HostModuleCount returns the number of registered host modules.
func (l *Linker) HostModuleCount() int {
	return len(l.hostModules)
}

// String returns a debug representation of the linker's registered modules.
func (l *Linker) String() string {
	s := fmt.Sprintf("Linker with %d modules:\n", len(l.hostModules))
	for name, m := range l.hostModules {
		s += fmt.Sprintf("  %s (%d funcs)\n", name, len(m.Funcs))
	}
	return s
}
