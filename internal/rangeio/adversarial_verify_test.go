// Adversarial verification tests written by the independent verifier of
// Lane D (SPEC-008), not by the implementer. Each test probes a claim of
// the conformance matrix or a boundary the implementer's tests do not
// cover. They must pass for the matrix claims to hold; a failure is a
// refutation.
package rangeio

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The matrix (REQ-IO-060 gap note) claims in-place writes preserving
// size and mtime are undetectable. Verify the claim itself: without it
// the limitation note would be an excuse, not a fact.
func TestAdversarialWriteInPlacePreservingMtimeUndetectable(t *testing.T) {
	path := writeFile(t, 100, 'x')
	s := open(t, path)
	stamp := s.Stamp()

	// Overwrite every byte in place, then restore the exact mtime.
	f, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(bytes.Repeat([]byte{'y'}, 100), 0); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	if err := os.Chtimes(path, stamp.ModTime, stamp.ModTime); err != nil {
		t.Fatal(err)
	}

	data, _, err := s.ReadAt(context.Background(), 0, 100)
	if err != nil {
		t.Fatalf("the write was detected after all: %v", err)
	}
	// The read silently returns bytes of a version the session is not
	// pinned to: the documented hole is real.
	if !bytes.Equal(data, bytes.Repeat([]byte{'y'}, 100)) {
		t.Fatalf("unexpected content: %q", data[:8])
	}
	t.Log("confirmed: same-size same-mtime overwrite is invisible to the stamp")
}

// The same write without the mtime cover-up must be detected: a plain
// in-place overwrite is the common case (editors writing in place, log
// rotation by truncation and rewrite).
func TestAdversarialWriteInPlaceWithoutMtimeDetected(t *testing.T) {
	path := writeFile(t, 100, 'x')
	s := open(t, path)
	// Force a distinguishable mtime into the future, as the implementer's
	// own bump helper does: same-size writes tick mtime forward.
	f, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(bytes.Repeat([]byte{'y'}, 100), 0); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	when := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadAt(context.Background(), 0, 100); !errors.Is(err, ErrChanged) {
		t.Fatalf("same-size overwrite with fresh mtime not detected: %v", err)
	}
}

// The matrix claims Scan failing with ErrChanged covers file mutation
// mid-scan. The implementer's test only bumps mtime. What actually
// happens in the wild is truncation: the file shrinks under the scan.
func TestAdversarialScanTruncateSmallerMidway(t *testing.T) {
	path := writeFile(t, 1<<20, 'x')
	s := open(t, path)
	truncated := false
	err := s.Scan(context.Background(), 0, 1<<10, func(off int64, data []byte) bool {
		if !truncated {
			truncated = true
			if err := os.Truncate(path, 4<<10); err != nil {
				t.Error(err)
			}
		}
		return true
	})
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("truncate-smaller mid-scan: got %v, want ErrChanged", err)
	}
}

// Growth mid-scan: the scan is pinned to the old end, but the version
// no longer exists; it must fail rather than deliver a prefix of one
// version that pretends to be whole.
func TestAdversarialScanGrowMidway(t *testing.T) {
	path := writeFile(t, 1<<20, 'x')
	s := open(t, path)
	grown := false
	err := s.Scan(context.Background(), 0, 1<<10, func(off int64, data []byte) bool {
		if !grown {
			grown = true
			if err := os.Truncate(path, 2<<20); err != nil {
				t.Error(err)
			}
		}
		return true
	})
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("grow mid-scan: got %v, want ErrChanged", err)
	}
}

// Replacement by rename (git checkout, atomic save with backup-and-
// rename) is the most common real-world "change". The open descriptor
// still reads the old inode, so an in-flight scan finishes coherently —
// but the next operation of the session must detect the replacement.
// The dangerous case is a replacement file of the SAME size whose mtime
// is preserved (rsync -t, tar extraction): the session then reads the
// new inode and the stamp cannot tell. Verify both directions.
func TestAdversarialReplaceViaRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data")
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, 100), 0600); err != nil {
		t.Fatal(err)
	}
	s := open(t, path)

	// Case 1: replacement with a fresh mtime is detected.
	repl := filepath.Join(dir, "repl")
	if err := os.WriteFile(repl, bytes.Repeat([]byte{'y'}, 100), 0600); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(repl, when, when); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repl, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadAt(context.Background(), 0, 100); !errors.Is(err, ErrChanged) {
		t.Fatalf("rename replacement with fresh mtime not detected: %v", err)
	}

	// Case 2: replacement preserving size and mtime (rsync -t style) is
	// NOT detected: same documented hole, wider than "in-place writes"
	// because nothing was written in place at all.
	path2 := filepath.Join(dir, "data2")
	if err := os.WriteFile(path2, bytes.Repeat([]byte{'x'}, 100), 0600); err != nil {
		t.Fatal(err)
	}
	s2 := open(t, path2)
	stamp := s2.Stamp()
	repl2 := filepath.Join(dir, "repl2")
	if err := os.WriteFile(repl2, bytes.Repeat([]byte{'z'}, 100), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(repl2, stamp.ModTime, stamp.ModTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repl2, path2); err != nil {
		t.Fatal(err)
	}
	data, _, err := s2.ReadAt(context.Background(), 0, 100)
	if err != nil {
		t.Fatalf("same-stamp rename replacement was detected after all: %v", err)
	}
	if !bytes.Equal(data, bytes.Repeat([]byte{'z'}, 100)) {
		t.Fatalf("unexpected content: %q", data[:8])
	}
	t.Log("confirmed: same-size same-mtime rename replacement is invisible to the stamp")
}

// Scan's contract on a negative start. ReadAt rejects negative ranges;
// Scan rejects them too — pin the behavior so the contract stays
// explicit.
func TestAdversarialScanNegativeStart(t *testing.T) {
	path := writeFile(t, 100, 'x')
	s := open(t, path)
	err := s.Scan(context.Background(), -5, 10, func(int64, []byte) bool { return true })
	if err == nil {
		t.Fatal("negative start accepted")
	}
}

// ReadAt length zero at an out-of-range start answers eof from metadata
// without I/O; past-end reads must not allocate. Pin the boundary.
func TestAdversarialReadAtBoundaries(t *testing.T) {
	path := writeFile(t, 100, 'x')
	s := open(t, path)
	// length 0 at an in-range start: eof false, no error.
	data, eof, err := s.ReadAt(context.Background(), 50, 0)
	if err != nil || len(data) != 0 || eof {
		t.Fatalf("zero-length in-range: len=%d eof=%v err=%v", len(data), eof, err)
	}
	// Exactly to the end: eof true.
	data, eof, err = s.ReadAt(context.Background(), 90, 10)
	if err != nil || len(data) != 10 || !eof {
		t.Fatalf("exact-end read: len=%d eof=%v err=%v", len(data), eof, err)
	}
	// One before the end is not eof.
	_, eof, err = s.ReadAt(context.Background(), 89, 10)
	if err != nil || eof {
		t.Fatalf("one-before-end read: eof=%v err=%v", eof, err)
	}
}
