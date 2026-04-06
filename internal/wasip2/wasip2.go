// Package wasip2 contains constants and types for WASI Preview 2 (0.2.x).
//
// WASI Preview 2 is based on the WebAssembly Component Model and defines
// interfaces using WIT (WebAssembly Interface Types). Unlike WASI Preview 1,
// which uses a flat C-like ABI, Preview 2 uses the Canonical ABI to pass
// rich types between the host and guest.
//
// See https://github.com/WebAssembly/WASI/blob/main/wasip2/README.md
package wasip2

// Interface names for WASI Preview 2.
// These follow the WIT package naming convention: "wasi:<package>/<interface>@<version>".
const (
	// WASINamespace is the namespace for all WASI interfaces.
	WASINamespace = "wasi"

	// Clocks interfaces.
	ClocksMonotonicClockName = "wasi:clocks/monotonic-clock@0.2.0"
	ClocksWallClockName      = "wasi:clocks/wall-clock@0.2.0"

	// Filesystem interfaces.
	FilesystemTypesName    = "wasi:filesystem/types@0.2.0"
	FilesystemPreopensName = "wasi:filesystem/preopens@0.2.0"

	// IO interfaces.
	IOErrorName   = "wasi:io/error@0.2.0"
	IOStreamsName = "wasi:io/streams@0.2.0"
	IOPollName    = "wasi:io/poll@0.2.0"

	// Random interfaces.
	RandomRandomName       = "wasi:random/random@0.2.0"
	RandomInsecureName     = "wasi:random/insecure@0.2.0"
	RandomInsecureSeedName = "wasi:random/insecure-seed@0.2.0"

	// CLI interfaces.
	CLIStdinName          = "wasi:cli/stdin@0.2.0"
	CLIStdoutName         = "wasi:cli/stdout@0.2.0"
	CLIStderrName         = "wasi:cli/stderr@0.2.0"
	CLIEnvironmentName    = "wasi:cli/environment@0.2.0"
	CLIExitName           = "wasi:cli/exit@0.2.0"
	CLITerminalInputName  = "wasi:cli/terminal-input@0.2.0"
	CLITerminalOutputName = "wasi:cli/terminal-output@0.2.0"
	CLITerminalStdinName  = "wasi:cli/terminal-stdin@0.2.0"
	CLITerminalStdoutName = "wasi:cli/terminal-stdout@0.2.0"
	CLITerminalStderrName = "wasi:cli/terminal-stderr@0.2.0"

	// Sockets interfaces.
	SocketsTCPName             = "wasi:sockets/tcp@0.2.0"
	SocketsTCPCreateSocketName = "wasi:sockets/tcp-create-socket@0.2.0"
	SocketsUDPName             = "wasi:sockets/udp@0.2.0"
	SocketsUDPCreateSocketName = "wasi:sockets/udp-create-socket@0.2.0"
	SocketsNetworkName         = "wasi:sockets/network@0.2.0"
	SocketsInstanceNetworkName = "wasi:sockets/instance-network@0.2.0"
	SocketsIPNameLookupName    = "wasi:sockets/ip-name-lookup@0.2.0"

	// HTTP interfaces.
	HTTPTypesName           = "wasi:http/types@0.2.0"
	HTTPOutgoingHandlerName = "wasi:http/outgoing-handler@0.2.0"
	HTTPIncomingHandlerName = "wasi:http/incoming-handler@0.2.0"
)

// World names for WASI Preview 2.
const (
	// CLICommandWorld is the world for command-line programs.
	CLICommandWorld = "wasi:cli/command@0.2.0"

	// HTTPProxyWorld is the world for HTTP proxy components.
	HTTPProxyWorld = "wasi:http/proxy@0.2.0"
)

// Error codes for WASI p2 stream operations.
const (
	StreamErrorClosed              uint8 = 0
	StreamErrorLastOperationFailed uint8 = 1
)

// Pollable represents a pollable handle in the wasi:io/poll interface.
type Pollable uint32

// InputStream represents an input stream handle in the wasi:io/streams interface.
type InputStream uint32

// OutputStream represents an output stream handle in the wasi:io/streams interface.
type OutputStream uint32

// Descriptor represents a filesystem descriptor in the wasi:filesystem/types interface.
type Descriptor uint32

// DirectoryEntryStream represents a directory entry stream handle.
type DirectoryEntryStream uint32

// MonotonicClock represents the monotonic clock type.
type MonotonicClock uint64

// WallClock represents the wall clock time as seconds and nanoseconds.
type WallClock struct {
	Seconds     uint64
	Nanoseconds uint32
}

// Network represents a network handle in the wasi:sockets/network interface.
type Network uint32

// TCPSocket represents a TCP socket handle.
type TCPSocket uint32

// UDPSocket represents a UDP socket handle.
type UDPSocket uint32

// IPAddressFamily represents the IP address family.
type IPAddressFamily uint8

const (
	IPAddressFamilyIPv4 IPAddressFamily = 0
	IPAddressFamilyIPv6 IPAddressFamily = 1
)

// ErrorCode enumerates WASI p2 filesystem error codes.
type ErrorCode uint8

const (
	ErrorCodeAccess              ErrorCode = 0
	ErrorCodeWouldBlock          ErrorCode = 1
	ErrorCodeAlready             ErrorCode = 2
	ErrorCodeBadDescriptor       ErrorCode = 3
	ErrorCodeBusy                ErrorCode = 4
	ErrorCodeDeadlock            ErrorCode = 5
	ErrorCodeQuota               ErrorCode = 6
	ErrorCodeExist               ErrorCode = 7
	ErrorCodeFileTooLarge        ErrorCode = 8
	ErrorCodeIllegalByteSequence ErrorCode = 9
	ErrorCodeInProgress          ErrorCode = 10
	ErrorCodeInterrupted         ErrorCode = 11
	ErrorCodeInvalid             ErrorCode = 12
	ErrorCodeIO                  ErrorCode = 13
	ErrorCodeIsDirectory         ErrorCode = 14
	ErrorCodeLoop                ErrorCode = 15
	ErrorCodeTooManyLinks        ErrorCode = 16
	ErrorCodeMessageSize         ErrorCode = 17
	ErrorCodeNameTooLong         ErrorCode = 18
	ErrorCodeNoDevice            ErrorCode = 19
	ErrorCodeNoEntry             ErrorCode = 20
	ErrorCodeNoLock              ErrorCode = 21
	ErrorCodeInsufficientMemory  ErrorCode = 22
	ErrorCodeInsufficientSpace   ErrorCode = 23
	ErrorCodeNotDirectory        ErrorCode = 24
	ErrorCodeNotEmpty            ErrorCode = 25
	ErrorCodeNotRecoverable      ErrorCode = 26
	ErrorCodeUnsupported         ErrorCode = 27
	ErrorCodeNoTTY               ErrorCode = 28
	ErrorCodeNoSuchDevice        ErrorCode = 29
	ErrorCodeOverflow            ErrorCode = 30
	ErrorCodeNotPermitted        ErrorCode = 31
	ErrorCodePipe                ErrorCode = 32
	ErrorCodeReadOnly            ErrorCode = 33
	ErrorCodeInvalidSeek         ErrorCode = 34
	ErrorCodeTextFileBusy        ErrorCode = 35
	ErrorCodeCrossDev            ErrorCode = 36
)

// ErrorCodeName returns a human-readable name for the given error code.
func ErrorCodeName(code ErrorCode) string {
	if int(code) < len(errorCodeNames) {
		return errorCodeNames[code]
	}
	return "unknown"
}

var errorCodeNames = [...]string{
	"access",
	"would-block",
	"already",
	"bad-descriptor",
	"busy",
	"deadlock",
	"quota",
	"exist",
	"file-too-large",
	"illegal-byte-sequence",
	"in-progress",
	"interrupted",
	"invalid",
	"io",
	"is-directory",
	"loop",
	"too-many-links",
	"message-size",
	"name-too-long",
	"no-device",
	"no-entry",
	"no-lock",
	"insufficient-memory",
	"insufficient-space",
	"not-directory",
	"not-empty",
	"not-recoverable",
	"unsupported",
	"no-tty",
	"no-such-device",
	"overflow",
	"not-permitted",
	"pipe",
	"read-only",
	"invalid-seek",
	"text-file-busy",
	"cross-device",
}

// DescriptorType enumerates the types of descriptors.
type DescriptorType uint8

const (
	DescriptorTypeUnknown         DescriptorType = 0
	DescriptorTypeBlockDevice     DescriptorType = 1
	DescriptorTypeCharacterDevice DescriptorType = 2
	DescriptorTypeDirectory       DescriptorType = 3
	DescriptorTypeFIFO            DescriptorType = 4
	DescriptorTypeSymbolicLink    DescriptorType = 5
	DescriptorTypeRegularFile     DescriptorType = 6
	DescriptorTypeSocket          DescriptorType = 7
)
