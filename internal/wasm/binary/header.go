package binary

import "bytes"

// Magic is the 4 byte preamble (literally "\0asm") of the binary format
// See https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#binary-magic
var Magic = []byte{0x00, 0x61, 0x73, 0x6D}

// version is format version and doesn't change between known specification versions
// See https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#binary-version
var version = []byte{0x01, 0x00, 0x00, 0x00}

// ComponentVersion is the version bytes for a WebAssembly Component (version 13, layer 1).
// The 4-byte version field encodes: u16 LE version (0x0d = 13), u16 LE layer (0x01 = component).
var ComponentVersion = []byte{0x0d, 0x00, 0x01, 0x00}

// IsComponent returns true if the given binary data represents a WebAssembly Component
// rather than a core WebAssembly module. It checks the magic number and version/layer bytes.
func IsComponent(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	return bytes.Equal(data[0:4], Magic) && bytes.Equal(data[4:8], ComponentVersion)
}
