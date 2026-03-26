// Package p2 provides WebAssembly System Interface (WASI) Preview 2 support
// for the Component Model. WASI P2 is the stable WASI specification built
// on the Component Model with poll-based synchronous I/O.
//
// WASI P2 defines the following interface groups:
//   - wasi:cli - Command-line interface (stdin, stdout, stderr, args, env)
//   - wasi:io - I/O streams and polling (input-stream, output-stream, pollable)
//   - wasi:clocks - Monotonic and wall clocks
//   - wasi:filesystem - Filesystem access (types, preopens)
//   - wasi:random - Random number generation
//   - wasi:sockets - TCP/UDP socket support
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
//	p2.MustInstantiate(ctx, r)
//	compiled, _ := r.CompileComponent(ctx, componentWasm)
//	instance, _ := r.InstantiateComponent(ctx, compiled)
//
// See https://github.com/WebAssembly/WASI/blob/main/wasip2/README.md
package p2

import (
	"context"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// Interfaces defined by WASI Preview 2.
const (
	IoStreams    = "wasi:io/streams@0.2.0"
	IoPoll      = "wasi:io/poll@0.2.0"
	IoError     = "wasi:io/error@0.2.0"
	CliStdin    = "wasi:cli/stdin@0.2.0"
	CliStdout   = "wasi:cli/stdout@0.2.0"
	CliStderr   = "wasi:cli/stderr@0.2.0"
	CliEnv      = "wasi:cli/environment@0.2.0"
	CliExit     = "wasi:cli/exit@0.2.0"
	ClocksWall  = "wasi:clocks/wall-clock@0.2.0"
	ClocksMono  = "wasi:clocks/monotonic-clock@0.2.0"
	RandomRandom = "wasi:random/random@0.2.0"
	FsTypes     = "wasi:filesystem/types@0.2.0"
	FsPreopens  = "wasi:filesystem/preopens@0.2.0"
)

// MustInstantiate calls Instantiate or panics on error.
func MustInstantiate(ctx context.Context, r wazero.Runtime) {
	if _, err := Instantiate(ctx, r); err != nil {
		panic(err)
	}
}

// Instantiate registers the WASI P2 host functions into the runtime.
func Instantiate(ctx context.Context, r wazero.Runtime) (api.Closer, error) {
	return NewBuilder(r).Instantiate(ctx)
}
