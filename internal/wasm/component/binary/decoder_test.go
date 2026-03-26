package binary

import (
	"testing"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm/component"
)

func TestIsComponent(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected bool
	}{
		{
			name:     "empty",
			data:     nil,
			expected: false,
		},
		{
			name:     "too short",
			data:     []byte{0x00, 0x61, 0x73, 0x6D},
			expected: false,
		},
		{
			name:     "core module",
			data:     []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00},
			expected: false,
		},
		{
			name:     "component",
			data:     []byte{0x00, 0x61, 0x73, 0x6D, 0x0d, 0x00, 0x01, 0x00},
			expected: true,
		},
		{
			name:     "invalid magic",
			data:     []byte{0x00, 0x00, 0x00, 0x00, 0x0d, 0x00, 0x01, 0x00},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsComponent(tt.data); got != tt.expected {
				t.Errorf("IsComponent() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDecodeComponent_Empty(t *testing.T) {
	// A minimal component with no sections: just magic + version.
	data := []byte{0x00, 0x61, 0x73, 0x6D, 0x0d, 0x00, 0x01, 0x00}

	comp, err := DecodeComponent(data, api.CoreFeaturesV2, 65536, false)
	if err != nil {
		t.Fatalf("DecodeComponent() error = %v", err)
	}
	if comp == nil {
		t.Fatal("DecodeComponent() returned nil")
	}
	if len(comp.CoreModules) != 0 {
		t.Errorf("expected 0 core modules, got %d", len(comp.CoreModules))
	}
	if len(comp.Imports) != 0 {
		t.Errorf("expected 0 imports, got %d", len(comp.Imports))
	}
	if len(comp.Exports) != 0 {
		t.Errorf("expected 0 exports, got %d", len(comp.Exports))
	}
}

func TestDecodeComponent_InvalidMagic(t *testing.T) {
	data := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x0d, 0x00, 0x01, 0x00}
	_, err := DecodeComponent(data, api.CoreFeaturesV2, 65536, false)
	if err == nil {
		t.Fatal("expected error for invalid magic number")
	}
}

func TestDecodeComponent_CoreModuleVersion(t *testing.T) {
	// Passing a core module version should fail.
	data := []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}
	_, err := DecodeComponent(data, api.CoreFeaturesV2, 65536, false)
	if err == nil {
		t.Fatal("expected error for core module version")
	}
}

func TestDecodeComponent_UnknownSection(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, 0x0d, 0x00, 0x01, 0x00, // header
		0xFF, // unknown section ID
		0x00, // section size = 0
	}
	_, err := DecodeComponent(data, api.CoreFeaturesV2, 65536, false)
	if err == nil {
		t.Fatal("expected error for unknown section ID")
	}
}

func TestDecodeComponent_CustomSection(t *testing.T) {
	data := []byte{
		0x00, 0x61, 0x73, 0x6D, 0x0d, 0x00, 0x01, 0x00, // header
		0x00,             // custom section ID
		0x08,             // section size = 8
		0x04,             // name length = 4
		't', 'e', 's', 't', // name = "test"
		0xDE, 0xAD, 0xBE, // custom data (3 bytes)
	}
	comp, err := DecodeComponent(data, api.CoreFeaturesV2, 65536, false)
	if err != nil {
		t.Fatalf("DecodeComponent() error = %v", err)
	}
	if len(comp.CustomSections) != 1 {
		t.Fatalf("expected 1 custom section, got %d", len(comp.CustomSections))
	}
	if comp.CustomSections[0].Name != "test" {
		t.Errorf("custom section name = %q, want %q", comp.CustomSections[0].Name, "test")
	}
	if len(comp.CustomSections[0].Data) != 3 {
		t.Errorf("custom section data length = %d, want 3", len(comp.CustomSections[0].Data))
	}
}

func TestSectionIDName(t *testing.T) {
	tests := []struct {
		id   component.SectionID
		name string
	}{
		{component.SectionIDCustom, "custom"},
		{component.SectionIDCoreModule, "core-module"},
		{component.SectionIDCoreInstance, "core-instance"},
		{component.SectionIDCoreType, "core-type"},
		{component.SectionIDComponent, "component"},
		{component.SectionIDInstance, "instance"},
		{component.SectionIDAlias, "alias"},
		{component.SectionIDType, "type"},
		{component.SectionIDCanon, "canon"},
		{component.SectionIDStart, "start"},
		{component.SectionIDImport, "import"},
		{component.SectionIDExport, "export"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := component.SectionIDName(tt.id); got != tt.name {
				t.Errorf("SectionIDName(%d) = %q, want %q", tt.id, got, tt.name)
			}
		})
	}
}
