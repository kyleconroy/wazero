//go:build plan9 || aix

package wasip3

func mapSyscallErrno(_ error) byte {
	return 0
}
