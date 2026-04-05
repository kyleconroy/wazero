package component

// ComponentType represents a type definition in the component model.
// The component model extends the core WebAssembly type system with
// rich types including records, variants, enums, flags, lists, options,
// results, tuples, and resources.
type ComponentType struct {
	// Kind identifies which variant of type this is.
	Kind TypeKind

	// Func is set when Kind is TypeKindFunc.
	Func *FuncType

	// Record is set when Kind is TypeKindRecord.
	Record *RecordType

	// Variant is set when Kind is TypeKindVariant.
	Variant *VariantType

	// List is set when Kind is TypeKindList.
	List *ListType

	// Tuple is set when Kind is TypeKindTuple.
	Tuple *TupleType

	// Flags is set when Kind is TypeKindFlags.
	Flags *FlagsType

	// Enum is set when Kind is TypeKindEnum.
	Enum *EnumType

	// Option is set when Kind is TypeKindOption.
	Option *OptionType

	// Result is set when Kind is TypeKindResult.
	Result *ResultType

	// Resource is set when Kind is TypeKindResource.
	Resource *ResourceType

	// Own is set when Kind is TypeKindOwn.
	Own *OwnType

	// Borrow is set when Kind is TypeKindBorrow.
	Borrow *BorrowType

	// Primitive is set when Kind is a primitive type kind.
	Primitive PrimitiveValType

	// ComponentDefined is set for component-level defined types.
	ComponentDefined *ComponentDefinedType

	// Instance is set when Kind is TypeKindInstance.
	Instance *InstanceType
}

// TypeKind classifies component model types.
type TypeKind byte

const (
	TypeKindPrimitive TypeKind = iota
	TypeKindRecord
	TypeKindVariant
	TypeKindList
	TypeKindTuple
	TypeKindFlags
	TypeKindEnum
	TypeKindOption
	TypeKindResult
	TypeKindOwn
	TypeKindBorrow
	TypeKindFunc
	TypeKindResource
	TypeKindInstance
	TypeKindComponent
)

// PrimitiveValType represents a primitive value type in the component model.
type PrimitiveValType byte

const (
	PrimitiveValTypeBool    PrimitiveValType = 0x7f
	PrimitiveValTypeS8      PrimitiveValType = 0x7e
	PrimitiveValTypeU8      PrimitiveValType = 0x7d
	PrimitiveValTypeS16     PrimitiveValType = 0x7c
	PrimitiveValTypeU16     PrimitiveValType = 0x7b
	PrimitiveValTypeS32     PrimitiveValType = 0x7a
	PrimitiveValTypeU32     PrimitiveValType = 0x79
	PrimitiveValTypeS64     PrimitiveValType = 0x78
	PrimitiveValTypeU64     PrimitiveValType = 0x77
	PrimitiveValTypeF32     PrimitiveValType = 0x76
	PrimitiveValTypeF64     PrimitiveValType = 0x75
	PrimitiveValTypeChar    PrimitiveValType = 0x74
	PrimitiveValTypeString  PrimitiveValType = 0x73
)

// PrimitiveValTypeName returns a human-readable name for the primitive type.
func PrimitiveValTypeName(p PrimitiveValType) string {
	switch p {
	case PrimitiveValTypeBool:
		return "bool"
	case PrimitiveValTypeS8:
		return "s8"
	case PrimitiveValTypeU8:
		return "u8"
	case PrimitiveValTypeS16:
		return "s16"
	case PrimitiveValTypeU16:
		return "u16"
	case PrimitiveValTypeS32:
		return "s32"
	case PrimitiveValTypeU32:
		return "u32"
	case PrimitiveValTypeS64:
		return "s64"
	case PrimitiveValTypeU64:
		return "u64"
	case PrimitiveValTypeF32:
		return "f32"
	case PrimitiveValTypeF64:
		return "f64"
	case PrimitiveValTypeChar:
		return "char"
	case PrimitiveValTypeString:
		return "string"
	default:
		return "unknown"
	}
}

// ValType represents a component-model value type, which can be either
// a primitive or a reference to a defined type by index.
type ValType struct {
	// Primitive is set if this is a primitive type.
	Primitive *PrimitiveValType

	// TypeIndex is set if this references a defined type.
	TypeIndex *uint32
}

// FuncType represents a component-level function type.
// Unlike core WebAssembly function types, these use named parameters
// and rich component model types.
type FuncType struct {
	// Params contains named, typed parameters.
	Params []NamedType

	// Results contains the function's result types.
	// A function can return a single unnamed type or multiple named types.
	Results *FuncResult
}

// FuncResult represents the result of a component function.
type FuncResult struct {
	// Named is set when the function returns multiple named values.
	Named []NamedType

	// Type is set when the function returns a single unnamed value.
	Type *ValType
}

// NamedType pairs a name with a value type.
type NamedType struct {
	Name string
	Type ValType
}

// RecordType is an ordered sequence of named, typed fields.
type RecordType struct {
	Fields []NamedType
}

// VariantType is a tagged union of named, typed cases.
type VariantType struct {
	Cases []VariantCase
}

// VariantCase is one case of a variant type.
type VariantCase struct {
	Name string
	// Type is optional; some cases carry no payload.
	Type *ValType
	// Refines optionally specifies the index of a refined case.
	Refines *uint32
}

// ListType represents a variable-length sequence of a uniform element type.
type ListType struct {
	ElementType ValType
}

// TupleType is a fixed-length, heterogeneous sequence of types.
type TupleType struct {
	Types []ValType
}

// FlagsType is a set of named boolean flags, stored as a bitfield.
type FlagsType struct {
	Names []string
}

// EnumType is a set of named cases without payloads.
type EnumType struct {
	Names []string
}

// OptionType represents an optional value, analogous to Option<T>.
type OptionType struct {
	Type ValType
}

// ResultType represents a result value, analogous to Result<T, E>.
type ResultType struct {
	// Ok is the success type (nil if none).
	Ok *ValType
	// Err is the error type (nil if none).
	Err *ValType
}

// ResourceType represents a resource type with a destructor.
type ResourceType struct {
	// DestructorIndex is the optional canonical function index of the destructor.
	DestructorIndex *uint32
}

// OwnType represents an owned handle to a resource.
type OwnType struct {
	// TypeIndex references the resource type.
	TypeIndex uint32
}

// BorrowType represents a borrowed handle to a resource.
type BorrowType struct {
	// TypeIndex references the resource type.
	TypeIndex uint32
}

// ComponentDefinedType wraps a component-level defined type.
type ComponentDefinedType struct {
	Kind TypeKind
}

// InstanceType represents a component instance type with named exports.
type InstanceType struct {
	Exports []ExportType
}

// ExportType is a named, typed export within an instance type.
type ExportType struct {
	Name string
	Kind ExternKind
	Type uint32
}
