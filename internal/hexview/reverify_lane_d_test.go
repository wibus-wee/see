// Re-verification tests of Lane D written by the re-verifying engineer,
// after the verification round's fixes.
package hexview

import (
	"context"
	"testing"
)

// TestReverifyFindRejectsNegativeStart checks the verification round's
// D3: a negative start is an error even for an empty pattern, as Scan
// and ReadAt contract — the empty-pattern branch used to answer the
// negative start itself, a position outside the file.
func TestReverifyFindRejectsNegativeStart(t *testing.T) {
	s := sessionFor(t, []byte("hello world"))
	for _, pattern := range [][]byte{nil, []byte("hello")} {
		if off, err := Find(context.Background(), s, -5, pattern); err == nil {
			t.Fatalf("a negative start answered %d, no error, for pattern %q", off, pattern)
		}
	}
	// The contract for a valid start is unchanged: an empty pattern
	// matches at it, a missing pattern reports ErrNotFound.
	if off, err := Find(context.Background(), s, 0, nil); err != nil || off != 0 {
		t.Fatalf("empty pattern at 0: off=%d err=%v", off, err)
	}
	if _, err := Find(context.Background(), s, 0, []byte("not in the file")); err != ErrNotFound {
		t.Fatalf("missing pattern: err=%v", err)
	}
}
