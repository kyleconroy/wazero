// Package component implements the WebAssembly Component Model.
// See https://github.com/WebAssembly/component-model
package component

import "github.com/tetratelabs/wazero/internal/wasm"

// ValType represents a component model value type.
type ValType byte

const (
	// Primitive value types
	ValTypeBool   ValType = 0x7f
	ValTypeS8     ValType = 0x7e
	ValTypeU8     ValType = 0x7d
	ValTypeS16    ValType = 0x7c
	ValTypeU16    ValType = 0x7b
	ValTypeS32    ValType = 0x7a
	ValTypeU32    ValType = 0x79
	ValTypeS64    ValType = 0x78
	ValTypeU64    ValType = 0x77
	ValTypeF32    ValType = 0x76
	ValTypeF64    ValType = 0x75
	ValTypeChar   ValType = 0x74
	ValTypeString ValType = 0x73
)

// ValTypeName returns the name of a component model value type.
func ValTypeName(v ValType) string {
	switch v {
	case ValTypeBool:
		return "bool"
	case ValTypeS8:
		return "s8"
	case ValTypeU8:
		return "u8"
	case ValTypeS16:
		return "s16"
	case ValTypeU16:
		return "u16"
	case ValTypeS32:
		return "s32"
	case ValTypeU32:
		return "u32"
	case ValTypeS64:
		return "s64"
	case ValTypeU64:
		return "u64"
	case ValTypeF32:
		return "f32"
	case ValTypeF64:
		return "f64"
	case ValTypeChar:
		return "char"
	case ValTypeString:
		return "string"
	default:
		return "unknown"
	}
}

// TypeKind represents the kind of a compound type defined in the component type section.
type TypeKind byte

const (
	TypeKindRecord  TypeKind = 0x72
	TypeKindVariant TypeKind = 0x71
	TypeKindList    TypeKind = 0x70
	TypeKindTuple   TypeKind = 0x6f
	TypeKindFlags   TypeKind = 0x6e
	TypeKindEnum    TypeKind = 0x6d
	TypeKindOption  TypeKind = 0x6b
	TypeKindResult  TypeKind = 0x6a
	TypeKindOwn     TypeKind = 0x69
	TypeKindBorrow  TypeKind = 0x68
	TypeKindStream  TypeKind = 0x66
	TypeKindFuture  TypeKind = 0x65
)

// ComponentType represents a type in the component model type system.
// This is a tagged union covering both primitive value types and compound types.
type ComponentType struct {
	// Kind indicates whether this is a primitive or compound type.
	Kind ComponentTypeKind

	// For primitive types:
	Primitive ValType

	// For type index references:
	Index uint32

	// For compound types, one of the following is set:
	Record  *RecordType
	Variant *VariantType
	List    *ListType
	Tuple   *TupleType
	Flags   *FlagsType
	Enum    *EnumType
	Option  *OptionType
	Result  *ResultType
	Own     *OwnType
	Borrow  *BorrowType
	Stream  *StreamType
	Future  *FutureType
}

// ComponentTypeKind indicates what kind of component type this is.
type ComponentTypeKind byte

const (
	ComponentTypeKindPrimitive ComponentTypeKind = iota
	ComponentTypeKindIndex
	ComponentTypeKindRecord
	ComponentTypeKindVariant
	ComponentTypeKindList
	ComponentTypeKindTuple
	ComponentTypeKindFlags
	ComponentTypeKindEnum
	ComponentTypeKindOption
	ComponentTypeKindResult
	ComponentTypeKindOwn
	ComponentTypeKindBorrow
	ComponentTypeKindStream
	ComponentTypeKindFuture
)

// RecordType represents a record (struct) type.
type RecordType struct {
	Fields []RecordField
}

// RecordField represents a single field in a record type.
type RecordField struct {
	Name string
	Type ComponentType
}

// VariantType represents a variant (tagged union) type.
type VariantType struct {
	Cases []VariantCase
}

// VariantCase represents a single case in a variant type.
type VariantCase struct {
	Name    string
	Type    *ComponentType // nil if the case has no payload
	Refines *uint32        // optional refinement index
}

// ListType represents a list<T> type.
type ListType struct {
	Element ComponentType
}

// TupleType represents a tuple type.
type TupleType struct {
	Types []ComponentType
}

// FlagsType represents a flags type (named booleans).
type FlagsType struct {
	Names []string
}

// EnumType represents an enum type (named variants with no payloads).
type EnumType struct {
	Names []string
}

// OptionType represents an option<T> type.
type OptionType struct {
	Type ComponentType
}

// ResultType represents a result<T, E> type.
type ResultType struct {
	Ok  *ComponentType // nil if no ok payload
	Err *ComponentType // nil if no error payload
}

// OwnType represents an own<T> handle type.
type OwnType struct {
	TypeIndex uint32 // index of the resource type
}

// BorrowType represents a borrow<T> handle type.
type BorrowType struct {
	TypeIndex uint32 // index of the resource type
}

// StreamType represents a stream<T> type (WASI P3 async).
type StreamType struct {
	Element *ComponentType // nil if no element type
}

// FutureType represents a future<T> type (WASI P3 async).
type FutureType struct {
	Type *ComponentType // nil if no payload type
}

// FuncType represents a component-level function type.
type FuncType struct {
	Async   bool
	Params  []NamedType
	Results []NamedType
}

// NamedType is a name-type pair used for function parameters and results.
type NamedType struct {
	Name string
	Type ComponentType
}

// ComponentDefinedType represents a type definition in the component type section.
// This wraps the various kinds of defined types (func, component, instance, resource, etc.).
type ComponentDefinedType struct {
	Kind DefinedTypeKind

	Func      *FuncType
	Component *ComponentTypeDecl
	Instance  *InstanceTypeDecl
	Resource  *ResourceType
	Type      *ComponentType // for alias/defined value types
}

// DefinedTypeKind indicates the kind of a type definition.
type DefinedTypeKind byte

const (
	DefinedTypeKindResource  DefinedTypeKind = 0x3f
	DefinedTypeKindFunc      DefinedTypeKind = 0x40
	DefinedTypeKindComponent DefinedTypeKind = 0x41
	DefinedTypeKindInstance  DefinedTypeKind = 0x42
	DefinedTypeKindAsyncFunc DefinedTypeKind = 0x43
	DefinedTypeKindType      DefinedTypeKind = 0x00 // a value type definition
)

// ComponentTypeDecl describes a component type inline declaration.
type ComponentTypeDecl struct {
	CoreTypes []wasm.FunctionType
	Types     []ComponentDefinedType
	Aliases   []Alias
	Imports   []ComponentImport
	Exports   []ComponentExport
}

// InstanceTypeDecl describes an instance type inline declaration.
type InstanceTypeDecl struct {
	CoreTypes []wasm.FunctionType
	Types     []ComponentDefinedType
	Aliases   []Alias
	Exports   []ComponentExport
}

// ResourceType represents a resource type declaration.
type ResourceType struct {
	Representation ValType // the core representation type (i32)
	Destructor     *uint32 // optional destructor function index
}

// ExternDesc describes an imported or exported external item.
type ExternDesc struct {
	Kind ExternDescKind

	// One of the following depending on Kind:
	TypeIndex      uint32            // for Func, Type, Component, Instance
	CoreType       *wasm.FunctionType // for CoreFunc
	TypeBound      TypeBound         // for Type kind - eq or sub resource
	IsSubResource  bool              // true when TypeBound is sub(resource)
}

// TypeBound indicates how a type export is bound.
type TypeBound byte

const (
	TypeBoundEq  TypeBound = 0x00 // eq(idx) - equal to another type
	TypeBoundSub TypeBound = 0x01 // sub(resource) - subtype of resource
)

// ExternDescKind indicates what kind of external item this is.
type ExternDescKind byte

const (
	ExternDescKindModule    ExternDescKind = 0x00
	ExternDescKindFunc      ExternDescKind = 0x01
	ExternDescKindValue     ExternDescKind = 0x02
	ExternDescKindType      ExternDescKind = 0x03
	ExternDescKindComponent ExternDescKind = 0x04
	ExternDescKindInstance  ExternDescKind = 0x05
)

// ExternDescKindName returns a human-readable name.
func ExternDescKindName(k ExternDescKind) string {
	switch k {
	case ExternDescKindModule:
		return "module"
	case ExternDescKindFunc:
		return "func"
	case ExternDescKindValue:
		return "value"
	case ExternDescKindType:
		return "type"
	case ExternDescKindComponent:
		return "component"
	case ExternDescKindInstance:
		return "instance"
	default:
		return "unknown"
	}
}
