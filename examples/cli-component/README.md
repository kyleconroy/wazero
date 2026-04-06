## CLI Component example

This example shows how to run a WASI P3 CLI component programmatically using
wazero's component model support.

Unlike core WebAssembly modules, components use the [WebAssembly Component
Model](https://component-model.bytecodealliance.org/) which provides typed
interfaces (WIT), resource handles, and the canonical ABI. wazero handles all
of this transparently.

### Usage

```bash
$ go run cli_component.go path/to/component.wasm [args...]
```

### How it works

The key API surface is:

1. **`wasip3.NewComponentHost`** — creates a host that implements all WASI
   interfaces (CLI args/env, stdio, clocks, random, filesystem).

2. **`wasip3.InstantiateComponentWithHost`** — decodes the component binary,
   registers host functions, instantiates core modules, and runs the
   component's entry point.

```go
host := wasip3.NewComponentHost(os.Stdin, os.Stdout, os.Stderr, os.Args, nil)
mod, err := wasip3.InstantiateComponentWithHost(ctx, rt, wasmBytes,
    wazero.NewModuleConfig().WithName("").WithStartFunctions(), host)
```

### Building a component

Components can be built from Rust (targeting `wasm32-wasip2`) or from Go
(targeting `GOOS=wasip1 GOARCH=wasm`) and then converted with `wasm-tools`:

```bash
# Rust
cargo build --target wasm32-wasip2

# Go + wasm-tools
GOARCH=wasm GOOS=wasip1 go build -o app.wasm
wasm-tools component new app.wasm -o app.component.wasm
```

### Running the test

The test uses a component from the
[wasi-testsuite](https://github.com/WebAssembly/wasi-testsuite). To set it up:

```bash
cd /path/to/wazero
git clone https://github.com/WebAssembly/wasi-testsuite.git
cd wasi-testsuite && git checkout prod/testsuite-all
```

Then run:

```bash
go test -v ./examples/cli-component/
```
