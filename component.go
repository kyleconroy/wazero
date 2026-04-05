package wazero

import (
	"context"
	"fmt"
	"strings"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/component"
	compbin "github.com/tetratelabs/wazero/internal/component/binary"
	"github.com/tetratelabs/wazero/internal/wasm"
	binaryformat "github.com/tetratelabs/wazero/internal/wasm/binary"
)

// IsComponent checks whether the given binary is a WebAssembly component
// (as opposed to a core module).
func IsComponent(binary []byte) bool {
	return compbin.IsComponent(binary)
}

// ComponentLinker manages host function registrations for component model instantiation.
// Host functions are registered by their flattened import module and function names
// as they appear in the component's core modules.
type ComponentLinker struct {
	linker *component.Linker
	// preCallbackHook is called before each async callback invocation.
	// Used to restore context slots that the guest clears between callbacks.
	preCallbackHook func()
}

// NewComponentLinker creates a new ComponentLinker.
func NewComponentLinker() *ComponentLinker {
	return &ComponentLinker{
		linker: component.NewLinker(),
	}
}

// DefineFunc registers a host function under the given import module and function name.
func (cl *ComponentLinker) DefineFunc(moduleName, funcName string, paramTypes, resultTypes []api.ValueType, fn api.GoModuleFunction) {
	cl.linker.DefineFunc(moduleName, funcName, paramTypes, resultTypes, fn)
}

// SetPreCallbackHook sets a function to call before each async callback invocation.
func (cl *ComponentLinker) SetPreCallbackHook(hook func()) {
	cl.preCallbackHook = hook
}

// InstantiateComponent decodes a component binary, extracts its core modules,
// satisfies their imports from registered host functions, and instantiates them.
//
// For CLI command components (the common case for wasi:cli/command), this:
// 1. Decodes the component to find embedded core modules
// 2. Registers host modules for all imports the core modules need
// 3. Instantiates the main core module
// 4. Returns the module so the caller can invoke exported functions
func (cl *ComponentLinker) InstantiateComponent(
	ctx context.Context,
	rt Runtime,
	binary []byte,
	config ModuleConfig,
) (api.Module, error) {
	// Decode the component.
	comp, err := compbin.DecodeComponent(binary)
	if err != nil {
		return nil, fmt.Errorf("decode component: %w", err)
	}

	if len(comp.CoreModules) == 0 {
		return nil, fmt.Errorf("component contains no core modules")
	}

	r := rt.(*runtime)

	// Decode the main core module to discover its imports.
	mainModule, err := binaryformat.DecodeModule(comp.CoreModules[0].Data, r.enabledFeatures,
		r.memoryLimitPages, r.memoryCapacityFromMax, !r.dwarfDisabled, r.storeCustomSections)
	if err != nil {
		return nil, fmt.Errorf("decode main core module: %w", err)
	}

	// Group imports by module name.
	importModules := make(map[string][]wasm.Import)
	for _, imp := range mainModule.ImportSection {
		importModules[imp.Module] = append(importModules[imp.Module], imp)
	}

	// For each import module, register a host module if we have matching host functions.
	for moduleName := range importModules {
		hm := cl.linker.GetHostModule(moduleName)
		if hm == nil {
			// Check if we can auto-stub this module.
			if !shouldAutoStub(moduleName) {
				return nil, fmt.Errorf("missing host module: %q", moduleName)
			}
			hm = &component.HostModule{
				Name:  moduleName,
				Funcs: make(map[string]*component.HostFunc),
			}
		}

		// Check if this module is already instantiated in the runtime.
		if existing := rt.Module(moduleName); existing != nil {
			continue
		}

		builder := rt.NewHostModuleBuilder(moduleName)
		exporter := builder.(wasm.HostFuncExporter)

		for _, imp := range importModules[moduleName] {
			if imp.Type != wasm.ExternTypeFunc {
				continue
			}
			funcName := imp.Name

			hf, ok := hm.Funcs[funcName]
			if !ok {
				// Auto-stub: create a no-op function matching the import signature.
				funcType := mainModule.TypeSection[imp.DescFunc]
				paramTypes := make([]api.ValueType, len(funcType.Params))
				copy(paramTypes, funcType.Params)
				resultTypes := make([]api.ValueType, len(funcType.Results))
				copy(resultTypes, funcType.Results)

				hf = &component.HostFunc{
					Name:        funcName,
					ParamTypes:  paramTypes,
					ResultTypes: resultTypes,
					Func: api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
						// No-op stub: zero-initialize results.
						for i := range stack[:len(resultTypes)] {
							stack[i] = 0
						}
					}),
				}
			}

			exporter.ExportHostFunc(&wasm.HostFunc{
				ExportName:  funcName,
				Name:        moduleName + "." + funcName,
				ParamTypes:  hf.ParamTypes,
				ParamNames:  makeParamNames(len(hf.ParamTypes)),
				ResultTypes: hf.ResultTypes,
				ResultNames: makeResultNames(len(hf.ResultTypes)),
				Code:        wasm.Code{GoFunc: hf.Func},
			})
		}

		if _, err := builder.Instantiate(ctx); err != nil {
			return nil, fmt.Errorf("instantiate host module %q: %w", moduleName, err)
		}
	}

	// Compile and instantiate the main core module.
	compiled, err := rt.CompileModule(ctx, comp.CoreModules[0].Data)
	if err != nil {
		return nil, fmt.Errorf("compile main core module: %w", err)
	}

	mod, err := rt.InstantiateModule(ctx, compiled, config)
	if err != nil {
		return nil, err
	}

	// For component model modules, we need to call the appropriate entry point.
	// P3 components use the async-lift entry point with a callback-based
	// event loop. P2 components use wasi:cli/run@0.2.0#run.
	if err := cl.runComponentEntry(ctx, mod); err != nil {
		return mod, err
	}

	return mod, nil
}

// runComponentEntry calls the appropriate entry point for a component.
// For P3 async components, this uses the [async-lift] entry + callback loop.
// For P2 components, this calls wasi:cli/run@0.2.0#run.
func (cl *ComponentLinker) runComponentEntry(ctx context.Context, mod api.Module) error {
	// Try P3 async entry first.
	asyncRun := mod.ExportedFunction("[async-lift]wasi:cli/run@0.3.0-rc-2026-02-09#run")
	callback := mod.ExportedFunction("[callback][async-lift]wasi:cli/run@0.3.0-rc-2026-02-09#run")

	if asyncRun != nil && callback != nil {
		return cl.runAsyncEntry(ctx, mod, asyncRun, callback)
	}

	// Fall back to P2 synchronous entry.
	if runFn := mod.ExportedFunction("wasi:cli/run@0.2.0#run"); runFn != nil {
		_, err := runFn.Call(ctx)
		return err
	}

	// No entry point found - just return.
	return nil
}

// runAsyncEntry executes the P3 async entry point protocol.
// The async-lift function starts the task and returns an i32 status.
// If the task is not complete, we call the callback with events until it finishes.
func (cl *ComponentLinker) runAsyncEntry(ctx context.Context, mod api.Module, asyncRun, callback api.Function) error {
	// Call the async entry point. Returns an i32 encoding task state.
	results, err := asyncRun.Call(ctx)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		return nil
	}

	packed := uint32(results[0])

	// The callback returns a packed i32:
	//   low 4 bits: CallbackCode (EXIT=0, YIELD=1, WAIT=2)
	//   upper 28 bits: waitable-set index (used with WAIT)
	const (
		callbackExit  = 0
		callbackYield = 1
		callbackWait  = 2
	)

	code := packed & 0xf
	for code != callbackExit {
		if cl.preCallbackHook != nil {
			cl.preCallbackHook()
		}
		// The callback takes (event_code: i32, p1: i32, p2: i32) and returns packed i32.
		// EventCode: NONE=0, SUBTASK=1, STREAM_READ=2, STREAM_WRITE=3,
		//            FUTURE_READ=4, FUTURE_WRITE=5, TASK_CANCELLED=6.
		// For now, deliver NONE events (poll).
		results, err = callback.Call(ctx, 0, 0, 0)
		if err != nil {
			return err
		}
		if len(results) == 0 {
			break
		}
		packed = uint32(results[0])
		code = packed & 0xf
	}

	return nil
}

// shouldAutoStub returns true if a module should be auto-stubbed when no
// explicit host implementation is provided. This includes component model
// builtins, export callbacks, and any WASI interface modules.
func shouldAutoStub(moduleName string) bool {
	if moduleName == "$root" {
		return true
	}
	if strings.HasPrefix(moduleName, "[export]") {
		return true
	}
	// Auto-stub any WASI interface module that isn't explicitly provided.
	if strings.HasPrefix(moduleName, "wasi:") {
		return true
	}
	return false
}

func makeParamNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("p%d", i)
	}
	return names
}

func makeResultNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("r%d", i)
	}
	return names
}
