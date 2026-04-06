//go:build !plan9 && !aix

package wasip3

import "syscall"

func mapSyscallErrno(err error) byte {
	switch {
	case err == syscall.ENOTDIR:
		return ecNotDirectory
	case err == syscall.EISDIR:
		return ecIsDirectory
	case err == syscall.ENOTEMPTY:
		return ecNotEmpty
	case err == syscall.ELOOP:
		return ecLoop
	case err == syscall.ENAMETOOLONG:
		return ecNameTooLong
	case err == syscall.ENOSPC:
		return ecNotEnoughSpace
	case err == syscall.EXDEV:
		return ecCrossDevice
	case err == syscall.EROFS:
		return ecReadOnly
	case err == syscall.EBUSY:
		return ecBusy
	case err == syscall.EINVAL:
		return ecInvalid
	case err == syscall.EBADF:
		return ecBadDescriptor
	}
	return 0
}
