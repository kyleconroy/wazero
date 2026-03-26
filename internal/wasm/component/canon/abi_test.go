package canon

import (
	"math"
	"testing"

	"github.com/tetratelabs/wazero/internal/wasm/component"
)

func TestAlignment(t *testing.T) {
	tests := []struct {
		name     string
		typ      component.ComponentType
		expected uint32
	}{
		{
			name:     "bool",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeBool},
			expected: 1,
		},
		{
			name:     "u8",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeU8},
			expected: 1,
		},
		{
			name:     "u16",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeU16},
			expected: 2,
		},
		{
			name:     "u32",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeU32},
			expected: 4,
		},
		{
			name:     "u64",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeU64},
			expected: 8,
		},
		{
			name:     "f32",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeF32},
			expected: 4,
		},
		{
			name:     "f64",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeF64},
			expected: 8,
		},
		{
			name:     "string",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeString},
			expected: 4,
		},
		{
			name:     "char",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeChar},
			expected: 4,
		},
		{
			name: "list",
			typ: component.ComponentType{Kind: component.ComponentTypeKindList, List: &component.ListType{
				Element: component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeU8},
			}},
			expected: 4,
		},
		{
			name:     "own handle",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindOwn, Own: &component.OwnType{TypeIndex: 0}},
			expected: 4,
		},
		{
			name:     "borrow handle",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindBorrow, Borrow: &component.BorrowType{TypeIndex: 0}},
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Alignment(&tt.typ); got != tt.expected {
				t.Errorf("Alignment() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestSize(t *testing.T) {
	tests := []struct {
		name     string
		typ      component.ComponentType
		expected uint32
	}{
		{
			name:     "bool",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeBool},
			expected: 1,
		},
		{
			name:     "u32",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeU32},
			expected: 4,
		},
		{
			name:     "u64",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeU64},
			expected: 8,
		},
		{
			name:     "string",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeString},
			expected: 8,
		},
		{
			name: "list",
			typ: component.ComponentType{Kind: component.ComponentTypeKindList, List: &component.ListType{
				Element: component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeU8},
			}},
			expected: 8,
		},
		{
			name:     "own handle",
			typ:      component.ComponentType{Kind: component.ComponentTypeKindOwn, Own: &component.OwnType{TypeIndex: 0}},
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Size(&tt.typ); got != tt.expected {
				t.Errorf("Size() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestRecordLayout(t *testing.T) {
	// record { a: u8, b: u32, c: u8 } should have size 12 (1 + 3 pad + 4 + 1 + 3 pad = 12)
	// and alignment 4
	recordType := component.ComponentType{
		Kind: component.ComponentTypeKindRecord,
		Record: &component.RecordType{
			Fields: []component.RecordField{
				{Name: "a", Type: component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeU8}},
				{Name: "b", Type: component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeU32}},
				{Name: "c", Type: component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeU8}},
			},
		},
	}

	if got := Alignment(&recordType); got != 4 {
		t.Errorf("record alignment = %d, want 4", got)
	}
	if got := Size(&recordType); got != 12 {
		t.Errorf("record size = %d, want 12", got)
	}
}

func TestFlagsLayout(t *testing.T) {
	tests := []struct {
		numFlags     int
		expectedSize uint32
		expectedAlign uint32
	}{
		{0, 0, 1},
		{1, 1, 1},
		{8, 1, 1},
		{9, 2, 2},
		{16, 2, 2},
		{17, 4, 4},
		{32, 4, 4},
		{33, 8, 4},
	}

	for _, tt := range tests {
		names := make([]string, tt.numFlags)
		for i := range names {
			names[i] = "flag"
		}

		typ := component.ComponentType{
			Kind:  component.ComponentTypeKindFlags,
			Flags: &component.FlagsType{Names: names},
		}

		if got := Size(&typ); got != tt.expectedSize {
			t.Errorf("flags(%d) size = %d, want %d", tt.numFlags, got, tt.expectedSize)
		}
		if got := Alignment(&typ); got != tt.expectedAlign {
			t.Errorf("flags(%d) alignment = %d, want %d", tt.numFlags, got, tt.expectedAlign)
		}
	}
}

func TestLiftLowerBool(t *testing.T) {
	if LiftBool(0) != false {
		t.Error("LiftBool(0) should be false")
	}
	if LiftBool(1) != true {
		t.Error("LiftBool(1) should be true")
	}
	if LiftBool(42) != true {
		t.Error("LiftBool(42) should be true")
	}
	if LowerBool(false) != 0 {
		t.Error("LowerBool(false) should be 0")
	}
	if LowerBool(true) != 1 {
		t.Error("LowerBool(true) should be 1")
	}
}

func TestLiftLowerChar(t *testing.T) {
	r, err := LiftChar(0x41) // 'A'
	if err != nil {
		t.Fatalf("LiftChar error: %v", err)
	}
	if r != 'A' {
		t.Errorf("LiftChar(0x41) = %c, want A", r)
	}

	// Surrogate pair should fail
	_, err = LiftChar(0xD800)
	if err == nil {
		t.Error("LiftChar(0xD800) should fail for surrogate")
	}

	// Too large should fail
	_, err = LiftChar(0x110000)
	if err == nil {
		t.Error("LiftChar(0x110000) should fail for out of range")
	}

	if LowerChar('Z') != 0x5A {
		t.Errorf("LowerChar('Z') = %d, want 90", LowerChar('Z'))
	}
}

func TestLiftLowerFloat(t *testing.T) {
	// Normal values
	if LiftF32(math.Float32bits(1.5)) != 1.5 {
		t.Error("LiftF32(1.5) failed")
	}
	if LiftF64(math.Float64bits(2.5)) != 2.5 {
		t.Error("LiftF64(2.5) failed")
	}

	// NaN canonicalization
	if LowerF32(float32(math.NaN())) != 0x7fc00000 {
		t.Error("LowerF32(NaN) should produce canonical NaN")
	}
	if LowerF64(math.NaN()) != 0x7ff8000000000000 {
		t.Error("LowerF64(NaN) should produce canonical NaN")
	}
}

func TestLiftString(t *testing.T) {
	mem := make([]byte, 256)
	copy(mem[10:], "hello")

	s, err := LiftString(mem, 10, 5, StringEncodingUTF8)
	if err != nil {
		t.Fatalf("LiftString error: %v", err)
	}
	if s != "hello" {
		t.Errorf("LiftString = %q, want %q", s, "hello")
	}

	// Out of bounds
	_, err = LiftString(mem, 250, 100, StringEncodingUTF8)
	if err == nil {
		t.Error("expected out of bounds error")
	}
}

func TestLowerString(t *testing.T) {
	data, err := LowerString("hello", StringEncodingUTF8)
	if err != nil {
		t.Fatalf("LowerString error: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("LowerString = %q, want %q", string(data), "hello")
	}
}

func TestOptionLayout(t *testing.T) {
	// option<u32> should be: 1 byte discriminant + 3 pad + 4 byte payload = 8
	optType := component.ComponentType{
		Kind:   component.ComponentTypeKindOption,
		Option: &component.OptionType{Type: component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeU32}},
	}

	if got := Size(&optType); got != 8 {
		t.Errorf("option<u32> size = %d, want 8", got)
	}
	if got := Alignment(&optType); got != 4 {
		t.Errorf("option<u32> alignment = %d, want 4", got)
	}
}

func TestResultLayout(t *testing.T) {
	u32Type := component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeU32}
	u8Type := component.ComponentType{Kind: component.ComponentTypeKindPrimitive, Primitive: component.ValTypeU8}

	// result<u32, u8> should have max payload size of 4 (u32), alignment 4
	resultType := component.ComponentType{
		Kind:   component.ComponentTypeKindResult,
		Result: &component.ResultType{Ok: &u32Type, Err: &u8Type},
	}

	if got := Alignment(&resultType); got != 4 {
		t.Errorf("result<u32,u8> alignment = %d, want 4", got)
	}
	if got := Size(&resultType); got != 8 {
		t.Errorf("result<u32,u8> size = %d, want 8", got)
	}
}
