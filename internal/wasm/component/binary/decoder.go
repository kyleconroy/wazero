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
			idx := len(c.CoreModules)
			c.CoreModules = append(c.CoreModules, mod)
			// Store the raw bytes for re-compilation during instantiation.
			rawBytes := make([]byte, len(sectionData))
			copy(rawBytes, sectionData)
			c.CoreModuleBytes = append(c.CoreModuleBytes, rawBytes)
			c.SectionOrder = append(c.SectionOrder, component.SectionOrderEntry{SectionID: sectionID, Index: idx, Count: 1})

		case component.SectionIDCoreInstance:
			instances, err := decodeCoreInstances(sr)
			if err != nil {
				return nil, fmt.Errorf("section core-instance: %w", err)
			}
			idx := len(c.CoreInstances)
			c.CoreInstances = append(c.CoreInstances, instances...)
			c.SectionOrder = append(c.SectionOrder, component.SectionOrderEntry{SectionID: sectionID, Index: idx, Count: len(instances)})

		case component.SectionIDCoreType:
			types, err := decodeCoreTypes(sr)
			if err != nil {
				return nil, fmt.Errorf("section core-type: %w", err)
			}
			idx := len(c.CoreTypes)
			c.CoreTypes = append(c.CoreTypes, types...)
			c.SectionOrder = append(c.SectionOrder, component.SectionOrderEntry{SectionID: sectionID, Index: idx, Count: len(types)})

		case component.SectionIDComponent:
			nested, err := DecodeComponent(sectionData, enabledFeatures, memoryLimitPages, memoryCapacityFromMax)
			if err != nil {
				return nil, fmt.Errorf("section component: %w", err)
			}
			idx := len(c.Components)
			c.Components = append(c.Components, nested)
			c.SectionOrder = append(c.SectionOrder, component.SectionOrderEntry{SectionID: sectionID, Index: idx, Count: 1})

		case component.SectionIDInstance:
			instances, err := decodeInstances(sr)
			if err != nil {
				return nil, fmt.Errorf("section instance: %w", err)
			}
			idx := len(c.Instances)
			c.Instances = append(c.Instances, instances...)
			c.SectionOrder = append(c.SectionOrder, component.SectionOrderEntry{SectionID: sectionID, Index: idx, Count: len(instances)})

		case component.SectionIDAlias:
			aliases, err := decodeAliases(sr)
			if err != nil {
				return nil, fmt.Errorf("section alias: %w", err)
			}
			idx := len(c.Aliases)
			c.Aliases = append(c.Aliases, aliases...)
			c.SectionOrder = append(c.SectionOrder, component.SectionOrderEntry{SectionID: sectionID, Index: idx, Count: len(aliases)})

		case component.SectionIDType:
			types, err := decodeComponentTypes(sr)
			if err != nil {
				return nil, fmt.Errorf("section type: %w", err)
			}
			idx := len(c.Types)
			c.Types = append(c.Types, types...)
			c.SectionOrder = append(c.SectionOrder, component.SectionOrderEntry{SectionID: sectionID, Index: idx, Count: len(types)})

		case component.SectionIDCanon:
			canons, err := decodeCanons(sr)
			if err != nil {
				return nil, fmt.Errorf("section canon: %w", err)
			}
			idx := len(c.Canons)
			c.Canons = append(c.Canons, canons...)
			c.SectionOrder = append(c.SectionOrder, component.SectionOrderEntry{SectionID: sectionID, Index: idx, Count: len(canons)})

		case component.SectionIDStart:
			start, err := decodeComponentStart(sr)
			if err != nil {
				return nil, fmt.Errorf("section start: %w", err)
			}
			c.Start = start
			c.SectionOrder = append(c.SectionOrder, component.SectionOrderEntry{SectionID: sectionID, Index: 0, Count: 1})

		case component.SectionIDImport:
			imports, err := decodeComponentImports(sr)
			if err != nil {
				return nil, fmt.Errorf("section import: %w", err)
			}
			idx := len(c.Imports)
			c.Imports = append(c.Imports, imports...)
			c.SectionOrder = append(c.SectionOrder, component.SectionOrderEntry{SectionID: sectionID, Index: idx, Count: len(imports)})

		case component.SectionIDExport:
			exports, err := decodeComponentExports(sr)
			if err != nil {
				return nil, fmt.Errorf("section export: %w", err)
			}
			idx := len(c.Exports)
			c.Exports = append(c.Exports, exports...)
			c.SectionOrder = append(c.SectionOrder, component.SectionOrderEntry{SectionID: sectionID, Index: idx, Count: len(exports)})

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
// Core modules in components require at least V2 features (multi-value, etc.).
func decodeCoreModule(data []byte, features api.CoreFeatures, memLimit uint32, memCapFromMax bool) (*wasm.Module, error) {
	// The Component Model requires core modules to support at least V2 features.
	features |= api.CoreFeaturesV2
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
