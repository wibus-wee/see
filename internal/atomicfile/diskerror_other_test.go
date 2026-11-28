//go:build !unix

package atomicfile

import "os"

// limitedHelper cannot exist here: the kernel write limit it lowers is a
// Unix rlimit. TestMain must still compile, and the mode it serves is
// unreachable — the tests that fork it are unix-only too.
func limitedHelper() { os.Exit(2) }
