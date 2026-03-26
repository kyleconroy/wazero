// Package p3 provides WebAssembly System Interface (WASI) Preview 3 support
// for the Component Model. WASI P3 builds on WASI P2 and adds async support
// via futures and streams.
//
// WASI P3 defines the following interface groups:
//   - wasi:cli - Command-line interface (stdin, stdout, stderr, args, env)
//   - wasi:io - I/O streams (input-stream, output-stream, poll)
//   - wasi:clocks - Monotonic and wall clocks
//   - wasi:filesystem - Filesystem access (types, preopens)
//   - wasi:random - Random number generation
//   - wasi:sockets - TCP/UDP socket support
//   - wasi:http - HTTP client/server support
//
// This implementation provides the wasi:cli/command world, which is the
// standard entry point for command-line WebAssembly components.
//
// Usage:
//
//	ctx := context.Background()
//	r := wazero.NewRuntime(ctx)
//	defer r.Close(ctx)
//
//	p3.MustInstantiate(ctx, r)
//	compiled, _ := r.CompileComponent(ctx, componentWasm)
//	instance, _ := r.InstantiateComponent(ctx, compiled)
//
// See https://github.com/WebAssembly/WASI
package p3

import (
	"context"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// Interfaces defined by WASI P3.
const (
	IoStreams    = "wasi:io/streams@0.3.0"
	IoPoll      = "wasi:io/poll@0.3.0"
	IoError     = "wasi:io/error@0.3.0"
	CliStdin    = "wasi:cli/stdin@0.3.0"
	CliStdout   = "wasi:cli/stdout@0.3.0"
	CliStderr   = "wasi:cli/stderr@0.3.0"
	CliArgs     = "wasi:cli/environment@0.3.0"
	CliExit     = "wasi:cli/exit@0.3.0"
	ClocksWall  = "wasi:clocks/wall-clock@0.3.0"
	ClocksMono  = "wasi:clocks/monotonic-clock@0.3.0"
	RandomRandom = "wasi:random/random@0.3.0"
	FsTypes     = "wasi:filesystem/types@0.3.0"
	FsPreopens  = "wasi:filesystem/preopens@0.3.0"
)

// MustInstantiate calls Instantiate or panics on error.
func MustInstantiate(ctx context.Context, r wazero.Runtime) {
	if _, err := Instantiate(ctx, r); err != nil {
		panic(err)
	}
}

// Instantiate registers the WASI P3 host functions into the runtime.
// These are registered as host modules that a component's core modules can import from.
func Instantiate(ctx context.Context, r wazero.Runtime) (api.Closer, error) {
	return NewBuilder(r).Instantiate(ctx)
}
