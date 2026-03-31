// Package component implements the WebAssembly Component Model types and structures.
//
// The Component Model extends WebAssembly with higher-level types, interfaces, and
// composition capabilities. Components encapsulate one or more core WebAssembly modules
// and provide strongly-typed imports and exports defined using WIT (WebAssembly Interface Types).
//
// See https://github.com/WebAssembly/component-model
package component

// Component is a WebAssembly Component Model binary representation.
// Unlike core modules, components can contain nested modules and components,
// use rich interface types, and participate in interface-based linking.
//
// See https://github.com/WebAssembly/component-model/blob/main/design/mvp/Binary.md
type Component struct {
	// CoreModules contains the core WebAssembly modules embedded in this component.
	CoreModules []CoreModule

	// CoreInstances contains core module instances created within the component.
	CoreInstances []CoreInstance

	// ComponentTypes contains the component-level type definitions.
	ComponentTypes []ComponentType

	// Imports contains the component's imports, defined by interface and function names.
	Imports []Import

	// Exports contains the component's exports.
	Exports []Export

	// CanonicalFunctions contains canonical function definitions that lift/lower
	// between core wasm values and component model values.
	CanonicalFunctions []CanonicalFunction

	// Instances contains component instances.
	Instances []Instance

	// Aliases contains alias definitions that refer to exports of other instances.
	Aliases []Alias

	// CustomSections are the inlined custom sections in the component.
	CustomSections []CustomSection
}

// CoreModule represents an embedded core WebAssembly module within a component.
type CoreModule struct {
	// Data is the raw binary of the core module.
	Data []byte
}

// CoreInstance represents an instantiation of a core module within the component.
type CoreInstance struct {
	// Kind indicates how this instance was created.
	Kind CoreInstanceKind

	// ModuleIndex is the index of the core module to instantiate (for InstantiateKind).
	ModuleIndex uint32

	// Args are the instantiation arguments.
	Args []InstantiationArg

	// Exports are used for CoreInstanceKindFromExports.
	Exports []CoreExportItem
}

// CoreInstanceKind describes how a core instance is created.
type CoreInstanceKind byte

const (
	// CoreInstanceKindInstantiate creates an instance by instantiating a core module.
	CoreInstanceKindInstantiate CoreInstanceKind = 0x00

	// CoreInstanceKindFromExports creates an instance from a set of exports.
	CoreInstanceKindFromExports CoreInstanceKind = 0x01
)

// InstantiationArg is a named argument for core module instantiation.
type InstantiationArg struct {
	// Name is the import module name.
	Name string

	// Kind is the sort of the argument (always instance for core instantiation).
	Kind ExternKind

	// Index is the index of the instance providing the imports.
	Index uint32
}

// CoreExportItem is a named export within a synthesized core instance.
type CoreExportItem struct {
	// Name is the export name.
	Name string

	// Kind is the extern kind (func, table, memory, global).
	Kind ExternKind

	// Index is the index of the exported item.
	Index uint32
}

// ExternKind categorizes component or core extern items.
type ExternKind byte

const (
	ExternKindCoreModule  ExternKind = 0x00
	ExternKindFunc        ExternKind = 0x01
	ExternKindValue       ExternKind = 0x02
	ExternKindType        ExternKind = 0x03
	ExternKindComponent   ExternKind = 0x04
	ExternKindInstance    ExternKind = 0x05
	ExternKindCoreFunc    ExternKind = 0x10
	ExternKindCoreTable   ExternKind = 0x11
	ExternKindCoreMemory  ExternKind = 0x12
	ExternKindCoreGlobal  ExternKind = 0x13
)

// Import is a component-level import.
type Import struct {
	// Name is the import name, which uses the kebab-case interface naming convention.
	// e.g., "wasi:filesystem/types@0.2.0"
	Name string

	// Kind is the type of the import.
	Kind ExternKind

	// TypeIndex references a type in ComponentTypes for the expected shape.
	TypeIndex uint32
}

// Export is a component-level export.
type Export struct {
	// Name is the export name.
	Name string

	// Kind is the sort of the exported item.
	Kind ExternKind

	// Index is the index of the exported item.
	Index uint32

	// TypeIndex optionally references a type annotation for the export.
	TypeIndex *uint32
}

// CanonicalFunction defines a canonical function that bridges between core
// WebAssembly and the component model's higher-level types.
type CanonicalFunction struct {
	// Kind distinguishes lift from lower.
	Kind CanonicalFunctionKind

	// CoreFuncIndex is the core function index being lifted or lowered.
	CoreFuncIndex uint32

	// TypeIndex is the component function type index.
	TypeIndex uint32

	// Options contains canonical options (memory, realloc, string encoding, etc.).
	Options []CanonicalOption
}

// CanonicalFunctionKind indicates whether a canonical function lifts or lowers.
type CanonicalFunctionKind byte

const (
	// CanonicalFunctionLift lifts a core function to a component function.
	CanonicalFunctionLift CanonicalFunctionKind = 0x00

	// CanonicalFunctionLower lowers a component function to a core function.
	CanonicalFunctionLower CanonicalFunctionKind = 0x01

	// CanonicalFunctionResourceNew creates a new resource handle.
	CanonicalFunctionResourceNew CanonicalFunctionKind = 0x02

	// CanonicalFunctionResourceDrop drops a resource handle.
	CanonicalFunctionResourceDrop CanonicalFunctionKind = 0x03

	// CanonicalFunctionResourceRep gets the representation of a resource handle.
	CanonicalFunctionResourceRep CanonicalFunctionKind = 0x04
)

// CanonicalOption is an option for canonical function definitions.
type CanonicalOption struct {
	// Kind is the type of canonical option.
	Kind CanonicalOptionKind

	// Value is the associated value (e.g., memory index, function index).
	Value uint32
}

// CanonicalOptionKind identifies the kind of canonical option.
type CanonicalOptionKind byte

const (
	CanonicalOptionUTF8    CanonicalOptionKind = 0x00
	CanonicalOptionUTF16   CanonicalOptionKind = 0x01
	CanonicalOptionLatin1  CanonicalOptionKind = 0x02
	CanonicalOptionMemory  CanonicalOptionKind = 0x03
	CanonicalOptionRealloc CanonicalOptionKind = 0x04
	CanonicalOptionPostReturn CanonicalOptionKind = 0x05
)

// Instance is a component instance.
type Instance struct {
	// Kind indicates how the instance is created.
	Kind InstanceKind

	// ComponentIndex is used for InstanceKindInstantiate.
	ComponentIndex uint32

	// Args are instantiation arguments for InstanceKindInstantiate.
	Args []InstantiationArg

	// Exports are used for InstanceKindFromExports.
	Exports []Export
}

// InstanceKind describes how a component instance was created.
type InstanceKind byte

const (
	InstanceKindInstantiate  InstanceKind = 0x00
	InstanceKindFromExports  InstanceKind = 0x01
)

// Alias allows referencing items from other instances or the outer component.
type Alias struct {
	// Kind is the sort of the aliased item.
	Kind AliasKind

	// Target identifies where the alias comes from.
	Target AliasTarget
}

// AliasKind identifies the kind of alias.
type AliasKind byte

const (
	AliasKindInstanceExport AliasKind = 0x00
	AliasKindOuter          AliasKind = 0x01
)

// AliasTarget identifies the target of an alias.
type AliasTarget struct {
	// InstanceIndex is used for AliasKindInstanceExport.
	InstanceIndex uint32

	// Name is the export name for AliasKindInstanceExport.
	Name string

	// OuterCount is the nesting depth for AliasKindOuter.
	OuterCount uint32

	// Index is the item index in the target scope.
	Index uint32

	// ExternKind is the kind of item being aliased.
	ExternKind ExternKind
}

// CustomSection represents a custom section in a component.
type CustomSection struct {
	Name string
	Data []byte
}
