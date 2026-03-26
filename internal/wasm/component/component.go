package component

import (
	"crypto/sha256"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
)

// SectionID identifies component-level sections in the binary format.
type SectionID = byte

const (
	SectionIDCustom       SectionID = 0x00
	SectionIDCoreModule   SectionID = 0x01
	SectionIDCoreInstance SectionID = 0x02
	SectionIDCoreType     SectionID = 0x03
	SectionIDComponent    SectionID = 0x04
	SectionIDInstance     SectionID = 0x05
	SectionIDAlias        SectionID = 0x06
	SectionIDType         SectionID = 0x07
	SectionIDCanon        SectionID = 0x08
	SectionIDStart        SectionID = 0x09
	SectionIDImport       SectionID = 0x0A
	SectionIDExport       SectionID = 0x0B
)

// SectionIDName returns a human-readable name for a section ID.
func SectionIDName(id SectionID) string {
	switch id {
	case SectionIDCustom:
		return "custom"
	case SectionIDCoreModule:
		return "core-module"
	case SectionIDCoreInstance:
		return "core-instance"
	case SectionIDCoreType:
		return "core-type"
	case SectionIDComponent:
		return "component"
	case SectionIDInstance:
		return "instance"
	case SectionIDAlias:
		return "alias"
	case SectionIDType:
		return "type"
	case SectionIDCanon:
		return "canon"
	case SectionIDStart:
		return "start"
	case SectionIDImport:
		return "import"
	case SectionIDExport:
		return "export"
	default:
		return "unknown"
	}
}

// ComponentID is a sha256 hash of the component binary used for caching.
type ComponentID = [sha256.Size]byte

// Component is a WebAssembly Component Model binary representation.
// Unlike core wasm modules, components can contain nested modules and components,
// and use a richer type system with interface types.
type Component struct {
	// CoreModules contains the core WebAssembly modules embedded in this component.
	CoreModules []*wasm.Module

	// CoreInstances contains core module instantiation instructions.
	CoreInstances []CoreInstance

	// CoreTypes contains core type definitions scoped to this component.
	CoreTypes []wasm.FunctionType

	// Components contains nested components.
	Components []*Component

	// Instances contains component-level instantiation instructions.
	Instances []Instance

	// Aliases contains alias definitions that reference items from other instances.
	Aliases []Alias

	// Types contains component-level type definitions.
	Types []ComponentDefinedType

	// Canons contains canonical function definitions (lift/lower/resource operations).
	Canons []Canon

	// Start contains the component start function, if any.
	Start *ComponentStart

	// Imports contains the component's imports.
	Imports []ComponentImport

	// Exports contains the component's exports.
	Exports []ComponentExport

	// CustomSections contains any custom sections.
	CustomSections []*wasm.CustomSection

	// ID is the sha256 hash of the binary for caching.
	ID ComponentID
}

// CoreInstance represents a core module instantiation.
type CoreInstance struct {
	Kind CoreInstanceKind

	// For InstantiateKind:
	ModuleIndex uint32
	Args        []CoreInstantiationArg

	// For FromExportsKind:
	Exports []CoreInstanceExport
}

// CoreInstanceKind indicates how a core instance is created.
type CoreInstanceKind byte

const (
	CoreInstanceKindInstantiate  CoreInstanceKind = 0x00
	CoreInstanceKindFromExports  CoreInstanceKind = 0x01
)

// CoreInstantiationArg is a named argument for core module instantiation.
type CoreInstantiationArg struct {
	Name     string
	Kind     CoreInstantiationArgKind
	Index    uint32
}

// CoreInstantiationArgKind indicates what kind of core item is being passed.
type CoreInstantiationArgKind byte

const (
	CoreInstantiationArgKindInstance CoreInstantiationArgKind = 0x12
)

// CoreInstanceExport packages a name and index for exporting from a core instance.
type CoreInstanceExport struct {
	Name  string
	Kind  api.ExternType
	Index uint32
}

// Instance represents a component-level instantiation.
type Instance struct {
	Kind InstanceKind

	// For Instantiate:
	ComponentIndex uint32
	Args           []InstantiationArg

	// For FromExports:
	Exports []ComponentExportItem
}

// InstanceKind indicates how an instance is created.
type InstanceKind byte

const (
	InstanceKindInstantiate InstanceKind = 0x00
	InstanceKindFromExports InstanceKind = 0x01
)

// InstantiationArg is a named argument for component instantiation.
type InstantiationArg struct {
	Name  string
	Kind  ExternDescKind
	Index uint32
}

// ComponentExportItem is an item in a from-exports instance.
type ComponentExportItem struct {
	Name string
	Kind ExternDescKind
	Index uint32
}

// Alias references an item from another instance or outer scope.
type Alias struct {
	Kind AliasKind

	// For InstanceExport aliases:
	InstanceIndex uint32
	Name          string

	// For Outer aliases:
	OuterCount uint32
	OuterIndex uint32

	// Sort indicates what kind of item is aliased.
	Sort AliasSort
}

// AliasKind indicates the kind of alias.
type AliasKind byte

const (
	AliasKindInstanceExport     AliasKind = 0x00 // component instance export
	AliasKindCoreInstanceExport AliasKind = 0x01 // core instance export
	AliasKindOuter              AliasKind = 0x02 // outer alias
)

// AliasSort indicates the sort of item being aliased.
type AliasSort byte

const (
	AliasSortCoreModule    AliasSort = 0x00
	AliasSortFunc          AliasSort = 0x01
	AliasSortValue         AliasSort = 0x02
	AliasSortType          AliasSort = 0x03
	AliasSortComponent     AliasSort = 0x04
	AliasSortInstance      AliasSort = 0x05
	AliasSortCoreFunc      AliasSort = 0x10
	AliasSortCoreTable     AliasSort = 0x11
	AliasSortCoreMemory    AliasSort = 0x12
	AliasSortCoreGlobal    AliasSort = 0x13
	AliasSortCoreType      AliasSort = 0x14
	AliasSortCoreModule2   AliasSort = 0x15
	AliasSortCoreInstance  AliasSort = 0x16
)

// Canon represents a canonical function definition.
type Canon struct {
	Kind CanonKind

	// For Lift:
	CoreFuncIndex uint32
	TypeIndex     uint32
	Options       []CanonOption

	// For Lower:
	FuncIndex uint32
	// Options is shared with Lift

	// For ResourceNew/ResourceDrop/ResourceRep:
	ResourceTypeIndex uint32
}

// CanonKind indicates the canonical function kind.
type CanonKind byte

const (
	CanonKindLift            CanonKind = 0x00
	CanonKindLower           CanonKind = 0x01
	CanonKindResourceNew     CanonKind = 0x02
	CanonKindResourceDrop    CanonKind = 0x03
	CanonKindResourceRep     CanonKind = 0x04
	CanonKindTaskCancel      CanonKind = 0x05
	CanonKindSubtaskCancel   CanonKind = 0x06
	CanonKindBackpressureSet CanonKind = 0x08
	CanonKindTaskReturn      CanonKind = 0x09
	CanonKindThreadYield     CanonKind = 0x0c
	CanonKindSubtaskDrop     CanonKind = 0x0d
	CanonKindStreamNew       CanonKind = 0x0e
	CanonKindStreamRead      CanonKind = 0x0f
	CanonKindStreamWrite     CanonKind = 0x10
	CanonKindFutureNew       CanonKind = 0x11
	CanonKindFutureRead      CanonKind = 0x12
	CanonKindFutureWrite     CanonKind = 0x13
	CanonKindErrorContextNew CanonKind = 0x1c
)

// CanonOption represents an option on a canonical function.
type CanonOption struct {
	Kind  CanonOptionKind
	Value uint32 // meaning depends on Kind
}

// CanonOptionKind indicates the kind of canonical option.
type CanonOptionKind byte

const (
	CanonOptionKindUTF8      CanonOptionKind = 0x00
	CanonOptionKindUTF16     CanonOptionKind = 0x01
	CanonOptionKindLatin1    CanonOptionKind = 0x02
	CanonOptionKindMemory    CanonOptionKind = 0x03
	CanonOptionKindRealloc   CanonOptionKind = 0x04
	CanonOptionKindPostReturn CanonOptionKind = 0x05
	CanonOptionKindAsync     CanonOptionKind = 0x06
	CanonOptionKindCallback  CanonOptionKind = 0x07
)

// ComponentStart describes the start function for the component.
type ComponentStart struct {
	FuncIndex uint32
	Args      []uint32
	Results   uint32
}

// ComponentImport describes an import for the component.
type ComponentImport struct {
	Name string
	URL  string // optional
	Desc ExternDesc
}

// ComponentExport describes an export from the component.
type ComponentExport struct {
	Name      string
	URL       string // optional
	Kind      ExternDescKind
	Index     uint32
	TypeIndex *uint32 // optional type ascription
}
