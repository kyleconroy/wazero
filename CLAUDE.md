# CLAUDE.md

## Project Overview

wazero is a zero-dependency WebAssembly runtime for Go, with support for WASI Preview 1 (wasip1) and experimental WASI Preview 2/3 (wasip3) component model.

## Building

```bash
go build ./...
```

## Running Tests

### All tests
```bash
go test ./...
```

### WASI P3 filesystem tests (wasi-testsuite)

The WASI P3 tests require pre-compiled wasm binaries from the wasi-testsuite repository.

**Setup:**
```bash
# Clone the wasi-testsuite into the repo root (already gitignored)
cd /path/to/wazero
git clone https://github.com/WebAssembly/wasi-testsuite.git
cd wasi-testsuite
git checkout prod/testsuite-all
```

The wasm binaries are at `wasi-testsuite/tests/rust/testsuite/wasm32-wasip3/`.

**Run all 14 filesystem tests:**
```bash
go test -v -run 'TestFilesystem' ./imports/wasip3/
```

**Run a specific filesystem test:**
```bash
go test -v -run 'TestFilesystemIO' ./imports/wasip3/
```

**Available filesystem tests:**
- TestFilesystemStat
- TestFilesystemFlagsAndType
- TestFilesystemIO
- TestFilesystemIsSameObject
- TestFilesystemOpenErrors
- TestFilesystemReadDirectory
- TestFilesystemMetadataHash
- TestFilesystemAdvise
- TestFilesystemMkdirRmdir
- TestFilesystemRename
- TestFilesystemSetSize
- TestFilesystemUnlinkErrors
- TestFilesystemHardLinks
- TestFilesystemDotdot

**Run other P3 tests (CLI, clocks, etc.):**
```bash
go test -v -run 'TestCliStdio' ./imports/wasip3/
```

### WASI P1 tests
```bash
go test ./imports/wasi_snapshot_preview1/...
```

## Key Files

- `component.go` — Component model linker, async entry point protocol
- `imports/wasip3/component_host.go` — P3 component host (CLI, clocks, streams, resources)
- `imports/wasip3/component_host_fs.go` — P3 filesystem host functions
- `imports/wasip3/component_host_test.go` — P3 test harness

## P3 Async Protocol Notes

The wit-bindgen async return code encoding (important for stream/future operations):
- `BLOCKED = 0xFFFFFFFF`
- `COMPLETED(n) = (n << 4) | 0x0` — n items transferred, stream open
- `DROPPED(n) = (n << 4) | 0x1` — n items transferred, other end dropped/EOF
- `CANCELLED(n) = (n << 4) | 0x2` ��� n items transferred, cancelled

Functions returning `tuple<stream, future>` (like `read-via-stream`, `read-directory`) write handles to retPtr at offsets 0 and 4 with no result discriminant.
