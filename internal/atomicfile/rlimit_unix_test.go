//go:build unix

package atomicfile

import "syscall"

type syscallRlimit = syscall.Rlimit

const rlimitFSIZE = syscall.RLIMIT_FSIZE

func setrlimit(resource int, rlim *syscallRlimit) error {
	return syscall.Setrlimit(resource, rlim)
}
