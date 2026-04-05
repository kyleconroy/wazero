package wasip2

import (
	"testing"

	"github.com/tetratelabs/wazero/internal/testing/require"
)

func TestErrorCodeName(t *testing.T) {
	tests := []struct {
		code ErrorCode
		name string
	}{
		{ErrorCodeAccess, "access"},
		{ErrorCodeWouldBlock, "would-block"},
		{ErrorCodeAlready, "already"},
		{ErrorCodeBadDescriptor, "bad-descriptor"},
		{ErrorCodeBusy, "busy"},
		{ErrorCodeExist, "exist"},
		{ErrorCodeIO, "io"},
		{ErrorCodeIsDirectory, "is-directory"},
		{ErrorCodeNoEntry, "no-entry"},
		{ErrorCodeNotDirectory, "not-directory"},
		{ErrorCodeNotEmpty, "not-empty"},
		{ErrorCodeUnsupported, "unsupported"},
		{ErrorCodeReadOnly, "read-only"},
		{ErrorCodeCrossDev, "cross-device"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.name, ErrorCodeName(tt.code))
		})
	}
}

func TestErrorCodeName_Unknown(t *testing.T) {
	require.Equal(t, "unknown", ErrorCodeName(255))
}

func TestInterfaceNames(t *testing.T) {
	// Verify interface names follow the WIT naming convention.
	require.Equal(t, "wasi:clocks/monotonic-clock@0.2.0", ClocksMonotonicClockName)
	require.Equal(t, "wasi:clocks/wall-clock@0.2.0", ClocksWallClockName)
	require.Equal(t, "wasi:io/error@0.2.0", IOErrorName)
	require.Equal(t, "wasi:io/streams@0.2.0", IOStreamsName)
	require.Equal(t, "wasi:io/poll@0.2.0", IOPollName)
	require.Equal(t, "wasi:random/random@0.2.0", RandomRandomName)
	require.Equal(t, "wasi:filesystem/types@0.2.0", FilesystemTypesName)
	require.Equal(t, "wasi:filesystem/preopens@0.2.0", FilesystemPreopensName)
	require.Equal(t, "wasi:cli/stdin@0.2.0", CLIStdinName)
	require.Equal(t, "wasi:cli/stdout@0.2.0", CLIStdoutName)
	require.Equal(t, "wasi:cli/stderr@0.2.0", CLIStderrName)
	require.Equal(t, "wasi:cli/environment@0.2.0", CLIEnvironmentName)
	require.Equal(t, "wasi:cli/exit@0.2.0", CLIExitName)
	require.Equal(t, "wasi:sockets/tcp@0.2.0", SocketsTCPName)
	require.Equal(t, "wasi:http/types@0.2.0", HTTPTypesName)
	require.Equal(t, "wasi:http/outgoing-handler@0.2.0", HTTPOutgoingHandlerName)
}

func TestDescriptorType(t *testing.T) {
	require.Equal(t, DescriptorType(0), DescriptorTypeUnknown)
	require.Equal(t, DescriptorType(3), DescriptorTypeDirectory)
	require.Equal(t, DescriptorType(6), DescriptorTypeRegularFile)
}

func TestIPAddressFamily(t *testing.T) {
	require.Equal(t, IPAddressFamily(0), IPAddressFamilyIPv4)
	require.Equal(t, IPAddressFamily(1), IPAddressFamilyIPv6)
}

func TestWallClock(t *testing.T) {
	wc := WallClock{Seconds: 1000, Nanoseconds: 500}
	require.Equal(t, uint64(1000), wc.Seconds)
	require.Equal(t, uint32(500), wc.Nanoseconds)
}
