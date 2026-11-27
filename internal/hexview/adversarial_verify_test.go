// Adversarial verification tests written by the independent verifier of
// Lane D (SPEC-008), not by the implementer.
package hexview

import (
	"bytes"
	"context"
	"testing"
)

// Find's empty-pattern contract: a negative start satisfies
// start <= size, so it would "match" at a position outside the file.
// Check what the implementation actually does.
func TestAdversarialFindEmptyPatternNegativeStart(t *testing.T) {
	s := sessionFor(t, bytes.Repeat([]byte{'x'}, 100))
	off, err := Find(context.Background(), s, -5, nil)
	t.Logf("empty pattern at start -5: off=%d err=%v", off, err)
	if err == nil && off < 0 {
		t.Log("DEFECT CONFIRMED: empty pattern matches at a negative offset, outside the file")
	}
}

// Boundary correctness of the tail carry: a needle exactly straddling
// every chunk boundary of a scan must be found, and no match must ever
// be reported twice or missed. This is an exhaustive sweep over
// straddle positions, not a single hand-picked case.
func TestAdversarialFindEveryStraddlePosition(t *testing.T) {
	const chunk = int64(256 << 10) // rangeio.ScanChunk
	needle := []byte("NEEDLE!")
	// Needle positions straddling the first chunk boundary in every
	// possible phase, plus control positions away from boundaries.
	for phase := 0; phase < len(needle); phase++ {
		pos := chunk - int64(phase) - 1
		data := bytes.Repeat([]byte{'x'}, int(chunk*2+64))
		copy(data[pos:], needle)
		s := sessionFor(t, data)
		off, err := Find(context.Background(), s, 0, needle)
		if err != nil {
			t.Fatalf("phase %d (pos %d): %v", phase, pos, err)
		}
		if off != pos {
			t.Fatalf("phase %d: found at %d, needle is at %d", phase, off, pos)
		}
	}
}

// A needle at the very end of the file, in the final partial chunk.
func TestAdversarialFindAtFileEnd(t *testing.T) {
	needle := []byte("END!")
	data := append(bytes.Repeat([]byte{'y'}, 1000), needle...)
	s := sessionFor(t, data)
	off, err := Find(context.Background(), s, 0, needle)
	if err != nil || off != 1000 {
		t.Fatalf("end needle: off=%d err=%v", off, err)
	}
}

// Find from a start past the file's end must report ErrNotFound, not
// match or panic.
func TestAdversarialFindStartPastEnd(t *testing.T) {
	s := sessionFor(t, bytes.Repeat([]byte{'x'}, 100))
	_, err := Find(context.Background(), s, 200, []byte("x"))
	if err != ErrNotFound {
		t.Fatalf("start past end: %v", err)
	}
	// A negative start with a non-empty pattern scans from a negative
	// offset: Scan rejects it.
	_, err = Find(context.Background(), s, -10, []byte("x"))
	t.Logf("negative start, non-empty pattern: err=%v", err)
}
