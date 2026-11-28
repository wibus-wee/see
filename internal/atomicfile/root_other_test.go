//go:build !unix

package atomicfile

func runningAsRoot() bool { return false }
