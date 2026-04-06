// Package wasip3 contains constants and types for WASI Preview 3 (0.3.x).
//
// WASI Preview 3 builds on Preview 2 by adding native async support to the
// Component Model. This enables composable concurrency, where multiple
// components can execute concurrent I/O without the limitations of the
// poll-based approach in Preview 2.
//
// Key additions over Preview 2:
//   - Asynchronous function ABI: components can import and export functions
//     using sync or async ABIs interchangeably.
//   - Built-in stream and future types: generic, efficiently composable
//     cross-component communication primitives.
//   - Avoids the function coloring problem: async imports can be seamlessly
//     connected to sync exports and vice versa.
//
// See https://github.com/WebAssembly/WASI/blob/main/wasip3/README.md
package wasip3

// Interface names for WASI Preview 3.
// Preview 3 uses the same interface structure as Preview 2 but with updated versions
// and new async-aware interfaces.
const (
	// WASINamespace is the namespace for all WASI interfaces.
	WASINamespace = "wasi"

	// Version is the version suffix for WASI p3 interfaces.
	Version = "0.3.0"

	// Clocks interfaces.
	ClocksMonotonicClockName = "wasi:clocks/monotonic-clock@0.3.0"
	ClocksWallClockName      = "wasi:clocks/wall-clock@0.3.0"

	// IO interfaces.
	IOErrorName   = "wasi:io/error@0.3.0"
	IOStreamsName = "wasi:io/streams@0.3.0"
	IOPollName    = "wasi:io/poll@0.3.0"

	// Filesystem interfaces.
	FilesystemTypesName    = "wasi:filesystem/types@0.3.0"
	FilesystemPreopensName = "wasi:filesystem/preopens@0.3.0"

	// Random interfaces.
	RandomRandomName       = "wasi:random/random@0.3.0"
	RandomInsecureName     = "wasi:random/insecure@0.3.0"
	RandomInsecureSeedName = "wasi:random/insecure-seed@0.3.0"

	// CLI interfaces.
	CLIStdinName       = "wasi:cli/stdin@0.3.0"
	CLIStdoutName      = "wasi:cli/stdout@0.3.0"
	CLIStderrName      = "wasi:cli/stderr@0.3.0"
	CLIEnvironmentName = "wasi:cli/environment@0.3.0"
	CLIExitName        = "wasi:cli/exit@0.3.0"

	// Sockets interfaces.
	SocketsTCPName             = "wasi:sockets/tcp@0.3.0"
	SocketsTCPCreateSocketName = "wasi:sockets/tcp-create-socket@0.3.0"
	SocketsUDPName             = "wasi:sockets/udp@0.3.0"
	SocketsUDPCreateSocketName = "wasi:sockets/udp-create-socket@0.3.0"
	SocketsNetworkName         = "wasi:sockets/network@0.3.0"
	SocketsInstanceNetworkName = "wasi:sockets/instance-network@0.3.0"
	SocketsIPNameLookupName    = "wasi:sockets/ip-name-lookup@0.3.0"

	// HTTP interfaces.
	HTTPTypesName           = "wasi:http/types@0.3.0"
	HTTPOutgoingHandlerName = "wasi:http/outgoing-handler@0.3.0"
	HTTPIncomingHandlerName = "wasi:http/incoming-handler@0.3.0"
)

// World names for WASI Preview 3.
const (
	CLICommandWorld = "wasi:cli/command@0.3.0"
	HTTPProxyWorld  = "wasi:http/proxy@0.3.0"
)

// FutureState represents the state of an async future.
type FutureState uint8

const (
	// FutureStatePending indicates the future has not yet completed.
	FutureStatePending FutureState = 0

	// FutureStateReady indicates the future has completed and the value is available.
	FutureStateReady FutureState = 1

	// FutureStateClosed indicates the future has been consumed or cancelled.
	FutureStateClosed FutureState = 2
)

// StreamState represents the state of an async stream.
type StreamState uint8

const (
	// StreamStateOpen indicates the stream is open and can produce/consume values.
	StreamStateOpen StreamState = 0

	// StreamStateClosed indicates the stream has been closed normally.
	StreamStateClosed StreamState = 1

	// StreamStateError indicates the stream was closed due to an error.
	StreamStateError StreamState = 2
)

// Future represents an async future handle in the Component Model.
// In WASI p3, futures are built-in types that enable efficient cross-component
// async communication without the poll-based approach of p2.
type Future uint32

// Stream represents an async stream handle in the Component Model.
// Streams are the p3 replacement for p2's input-stream and output-stream
// resources, providing generic, composable cross-component data flow.
type Stream uint32

// Subtask represents a handle to an in-progress async operation.
// When a component calls an async import, it receives a subtask handle
// that can be used to check completion status or await the result.
type Subtask uint32

// SubtaskState represents the state of a subtask.
type SubtaskState uint8

const (
	// SubtaskStateStarted indicates the subtask has been started but not yet returned.
	SubtaskStateStarted SubtaskState = 0

	// SubtaskStateReturned indicates the subtask has returned its result.
	SubtaskStateReturned SubtaskState = 1

	// SubtaskStateCancelled indicates the subtask was cancelled.
	SubtaskStateCancelled SubtaskState = 2
)

// AsyncCanonicalOptions extends the canonical ABI options for async functions.
type AsyncCanonicalOptions struct {
	// Async indicates this function uses the async ABI.
	Async bool

	// Callback is the optional callback function index for notification of
	// async completion events.
	Callback *uint32
}
