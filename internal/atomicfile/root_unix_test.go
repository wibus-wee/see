//go:build unix

package atomicfile

import "os"

func runningAsRoot() bool { return os.Geteuid() == 0 }
