// Package binary implements the binary format decoder for WebAssembly Component Model.
// See https://github.com/WebAssembly/component-model/blob/main/design/mvp/Binary.md
package binary

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/leb128"
	"github.com/tetratelabs/wazero/internal/wasm"
	coreBinary "github.com/tetratelabs/wazero/internal/wasm/binary"
	"github.com/tetratelabs/wazero/internal/wasm/component"
)

// componentVersion is the version bytes for a component binary (version 13, layer 1).
var componentVersion = []byte{0x0d, 0x00, 0x01, 0x00}

// DecodeComponent decodes a WebAssembly Component from its binary representation.
func DecodeComponent(
	data []byte,
	enabledFeatures api.CoreFeatures,
	memoryLimitPages uint32,
	memoryCapacityFromMax bool,
) (*component.Component, error) {
	r := bytes.NewReader(data)

	// Magic number.
	buf := make([]byte, 4)
	if _, err := io.ReadFull(r, buf); err != nil || !bytes.Equal(buf, coreBinary.Magic) {
		return nil, fmt.Errorf("invalid magic number")
	}

	// Version + layer.
	if _, err := io.ReadFull(r, buf); err != nil || !bytes.Equal(buf, componentVersion) {
		return nil, fmt.Errorf("invalid component version header")
	}

	c := &component.Component{}
	c.ID = sha256.Sum256(data)

	for {
		sectionID, err := r.ReadByte()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("read section id: %w", err)
		}

		sectionSize, _, err := leb128.DecodeUint32(r)
		if err != nil {
			return nil, fmt.Errorf("get size of section %s: %v",
				component.SectionIDName(sectionID), err)
		}

		// Read the section data into a sub-reader to enforce size limits.
		sectionData := make([]byte, sectionSize)
		if _, err := io.ReadFull(r, sectionData); err != nil {
			return nil, fmt.Errorf("read section %s data: %w",
				component.SectionIDName(sectionID), err)
		}
		sr := bytes.NewReader(sectionData)

		switch sectionID {
		case component.SectionIDCustom:
			cs, err := decodeCustomSection(sr, sectionSize)
			if err != nil {
				return nil, fmt.Errorf("section custom: %w", err)
			}
			c.CustomSections = append(c.CustomSections, cs)

		case component.SectionIDCoreModule:
			mod, err := decodeCoreModule(sectionData, enabledFeatures, memoryLimitPages, memoryCapacityFromMax)
			if err != nil {
				return nil, fmt.Errorf("section core-module: %w", err)
			}
			c.CoreModules = append(c.CoreModules, mod)

		case component.SectionIDCoreInstance:
			instances, err := decodeCoreInstances(sr)
			if err != nil {
				return nil, fmt.Errorf("section core-instance: %w", err)
			}
			c.CoreInstances = append(c.CoreInstances, instances...)

		case component.SectionIDCoreType:
			types, err := decodeCoreTypes(sr)
			if err != nil {
				return nil, fmt.Errorf("section core-type: %w", err)
			}
			c.CoreTypes = append(c.CoreTypes, types...)

		case component.SectionIDComponent:
			nested, err := DecodeComponent(sectionData, enabledFeatures, memoryLimitPages, memoryCapacityFromMax)
			if err != nil {
				return nil, fmt.Errorf("section component: %w", err)
			}
			c.Components = append(c.Components, nested)

		case component.SectionIDInstance:
			instances, err := decodeInstances(sr)
			if err != nil {
				return nil, fmt.Errorf("section instance: %w", err)
			}
			c.Instances = append(c.Instances, instances...)

		case component.SectionIDAlias:
			aliases, err := decodeAliases(sr)
			if err != nil {
				return nil, fmt.Errorf("section alias: %w", err)
			}
			c.Aliases = append(c.Aliases, aliases...)

		case component.SectionIDType:
			types, err := decodeComponentTypes(sr)
			if err != nil {
				return nil, fmt.Errorf("section type: %w", err)
			}
			c.Types = append(c.Types, types...)

		case component.SectionIDCanon:
			canons, err := decodeCanons(sr)
			if err != nil {
				return nil, fmt.Errorf("section canon: %w", err)
			}
			c.Canons = append(c.Canons, canons...)

		case component.SectionIDStart:
			start, err := decodeComponentStart(sr)
			if err != nil {
				return nil, fmt.Errorf("section start: %w", err)
			}
			c.Start = start

		case component.SectionIDImport:
			imports, err := decodeComponentImports(sr)
			if err != nil {
				return nil, fmt.Errorf("section import: %w", err)
			}
			c.Imports = append(c.Imports, imports...)

		case component.SectionIDExport:
			exports, err := decodeComponentExports(sr)
			if err != nil {
				return nil, fmt.Errorf("section export: %w", err)
			}
			c.Exports = append(c.Exports, exports...)

		default:
			return nil, fmt.Errorf("unknown component section id: %#x", sectionID)
		}
	}
	return c, nil
}

// IsComponent returns true if the binary data starts with the component model magic + version.
func IsComponent(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	return bytes.Equal(data[0:4], coreBinary.Magic) &&
		bytes.Equal(data[4:8], componentVersion)
}

// decodeCoreModule decodes an embedded core wasm module.
func decodeCoreModule(data []byte, features api.CoreFeatures, memLimit uint32, memCapFromMax bool) (*wasm.Module, error) {
	return coreBinary.DecodeModule(data, features, memLimit, memCapFromMax, false, false)
}

// decodeCustomSection decodes a custom section.
func decodeCustomSection(r *bytes.Reader, sectionSize uint32) (*wasm.CustomSection, error) {
	name, nameSize, err := decodeUTF8(r, "custom section name")
	if err != nil {
		return nil, err
	}

	dataSize := sectionSize - nameSize
	data := make([]byte, dataSize)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("read custom section data: %w", err)
	}

	return &wasm.CustomSection{
		Name: name,
		Data: data,
	}, nil
}
