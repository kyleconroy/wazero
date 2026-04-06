package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasip3"
)

// main demonstrates how to run a WASI P3 CLI component using wazero's
// component model support.
//
// A component differs from a core Wasm module in that it uses the
// WebAssembly Component Model, which provides typed interfaces (WIT),
// resource handles, and canonical ABI lowering/lifting. wazero handles
// all of this transparently via ComponentHost and InstantiateComponentWithHost.
//
// Usage:
//
//	go run cli_component.go <path-to-component.wasm> [args...]
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <component.wasm> [args...]\n", os.Args[0])
		os.Exit(1)
	}

	wasmPath := os.Args[1]
	wasmArgs := os.Args[1:] // pass wasm path as argv[0], like a real CLI

	// Read the component binary.
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		log.Fatalf("error reading wasm: %v", err)
	}

	// Verify it's a component (not a core module).
	if !wazero.IsComponent(wasmBytes) {
		log.Fatalf("%s is not a WebAssembly component (it may be a core module)", wasmPath)
	}

	ctx := context.Background()

	// Create a new WebAssembly Runtime.
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Create a ComponentHost that provides WASI host function implementations.
	// This configures stdio streams, command-line arguments, and environment variables.
	host := wasip3.NewComponentHost(
		os.Stdin,  // stdin
		os.Stdout, // stdout
		os.Stderr, // stderr
		wasmArgs,  // command-line arguments passed to the component
		nil,       // environment variables as [][2]string{{"KEY", "VALUE"}}
	)

	// Optionally mount host directories into the component's filesystem:
	// fsConfig := wasip3.NewFSConfig().WithDirMount("/path/on/host", "/path/in/guest")
	// host.WithFSConfig(fsConfig)

	// Instantiate the component. This:
	// 1. Decodes the component binary and extracts its core modules
	// 2. Registers all WASI host functions (CLI, clocks, random, I/O, filesystem)
	// 3. Instantiates the core module with satisfied imports
	// 4. Runs the component's entry point (P3 async or P2 sync)
	mod, err := wasip3.InstantiateComponentWithHost(ctx, rt, wasmBytes,
		wazero.NewModuleConfig().
			WithName("").         // empty name avoids module name conflicts
			WithStartFunctions(), // don't auto-call _start; the component entry is called separately
		host,
	)
	if mod != nil {
		defer mod.Close(ctx)
	}

	if err != nil {
		log.Fatalf("error running component: %v", err)
	}

	// Check the exit code (some components call exit(0) which is not an error).
	if exitCode, exited := host.ExitCode(); exited && exitCode != 0 {
		os.Exit(int(exitCode))
	}
}
