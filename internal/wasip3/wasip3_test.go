package wasip3

import (
	"testing"

	"github.com/tetratelabs/wazero/internal/testing/require"
)

func TestInterfaceNames(t *testing.T) {
	// Verify p3 interface names follow the WIT naming convention with @0.3.0 version.
	require.Equal(t, "wasi:clocks/monotonic-clock@0.3.0", ClocksMonotonicClockName)
	require.Equal(t, "wasi:clocks/wall-clock@0.3.0", ClocksWallClockName)
	require.Equal(t, "wasi:io/error@0.3.0", IOErrorName)
	require.Equal(t, "wasi:io/streams@0.3.0", IOStreamsName)
	require.Equal(t, "wasi:io/poll@0.3.0", IOPollName)
	require.Equal(t, "wasi:random/random@0.3.0", RandomRandomName)
	require.Equal(t, "wasi:filesystem/types@0.3.0", FilesystemTypesName)
	require.Equal(t, "wasi:filesystem/preopens@0.3.0", FilesystemPreopensName)
	require.Equal(t, "wasi:cli/stdin@0.3.0", CLIStdinName)
	require.Equal(t, "wasi:cli/stdout@0.3.0", CLIStdoutName)
	require.Equal(t, "wasi:cli/stderr@0.3.0", CLIStderrName)
	require.Equal(t, "wasi:cli/environment@0.3.0", CLIEnvironmentName)
	require.Equal(t, "wasi:cli/exit@0.3.0", CLIExitName)
	require.Equal(t, "wasi:sockets/tcp@0.3.0", SocketsTCPName)
	require.Equal(t, "wasi:http/types@0.3.0", HTTPTypesName)
	require.Equal(t, "wasi:http/outgoing-handler@0.3.0", HTTPOutgoingHandlerName)
}

func TestWorldNames(t *testing.T) {
	require.Equal(t, "wasi:cli/command@0.3.0", CLICommandWorld)
	require.Equal(t, "wasi:http/proxy@0.3.0", HTTPProxyWorld)
}

func TestFutureState(t *testing.T) {
	require.Equal(t, FutureState(0), FutureStatePending)
	require.Equal(t, FutureState(1), FutureStateReady)
	require.Equal(t, FutureState(2), FutureStateClosed)
}

func TestStreamState(t *testing.T) {
	require.Equal(t, StreamState(0), StreamStateOpen)
	require.Equal(t, StreamState(1), StreamStateClosed)
	require.Equal(t, StreamState(2), StreamStateError)
}

func TestSubtaskState(t *testing.T) {
	require.Equal(t, SubtaskState(0), SubtaskStateStarted)
	require.Equal(t, SubtaskState(1), SubtaskStateReturned)
	require.Equal(t, SubtaskState(2), SubtaskStateCancelled)
}

func TestAsyncCanonicalOptions(t *testing.T) {
	// Verify async options struct works.
	opts := AsyncCanonicalOptions{Async: true}
	require.True(t, opts.Async)
	require.Nil(t, opts.Callback)

	cb := uint32(42)
	opts2 := AsyncCanonicalOptions{Async: true, Callback: &cb}
	require.True(t, opts2.Async)
	require.Equal(t, uint32(42), *opts2.Callback)
}
