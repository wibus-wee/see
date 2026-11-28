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

func writeFile(t *testing.T, size int, fill byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data")
	data := bytes.Repeat([]byte{fill}, size)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func open(t *testing.T, path string) *Session {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReadAtRangeContract(t *testing.T) {
	path := writeFile(t, 100, 'x')
	s := open(t, path)

	// A range inside the file reads fully and is not at its end.
	data, eof, err := s.ReadAt(context.Background(), 10, 40)
	if err != nil || len(data) != 40 || eof {
		t.Fatalf("in-range read: len=%d eof=%v err=%v", len(data), eof, err)
	}

	// A range extending past the end is a short read, not an error.
	data, eof, err = s.ReadAt(context.Background(), 80, 40)
	if err != nil || len(data) != 20 || !eof {
		t.Fatalf("past-end read: len=%d eof=%v err=%v", len(data), eof, err)
	}

	// A range starting at or past the end yields no bytes with eof set.
	for _, off := range []int64{100, 200} {
		data, eof, err = s.ReadAt(context.Background(), off, 10)
		if err != nil || len(data) != 0 || !eof {
			t.Fatalf("out-of-range read at %d: len=%d eof=%v err=%v", off, len(data), eof, err)
		}
	}

	// A zero length performs no I/O and answers from the pinned metadata.
	data, eof, err = s.ReadAt(context.Background(), 0, 0)
	if err != nil || len(data) != 0 || eof {
		t.Fatalf("zero-length read: len=%d eof=%v err=%v", len(data), eof, err)
	}

	// Negative ranges are invalid input, not EOF.
	if _, _, err = s.ReadAt(context.Background(), -1, 10); err == nil {
		t.Fatal("negative offset accepted")
	}
	if _, _, err = s.ReadAt(context.Background(), 0, -1); err == nil {
		t.Fatal("negative length accepted")
	}
}

func TestReadAtEmptyFile(t *testing.T) {
	path := writeFile(t, 0, 'x')
	s := open(t, path)
	data, eof, err := s.ReadAt(context.Background(), 0, 64)
	if err != nil || len(data) != 0 || !eof {
		t.Fatalf("empty file read: len=%d eof=%v err=%v", len(data), eof, err)
	}
}

func TestReadWindowCostIndependentOfFileSize(t *testing.T) {
	const window = 64 << 10

	small := writeFile(t, 1<<20, 's')
	// A sparse multi-gigabyte file costs no disk space and no scan time
	// to a positioned read, but seconds to a sequential one.
	sparse := filepath.Join(t.TempDir(), "sparse")
	f, err := os.Create(sparse)
	if err != nil {
		t.Fatal(err)
	}
	const sparseSize = int64(8 << 30)
	if err := f.Truncate(sparseSize); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	ss, sp := open(t, small), open(t, sparse)

	// The last window of the sparse file reads zeros, which a scan-free
	// read delivers directly.
	data, eof, err := sp.ReadAt(context.Background(), sparseSize-window, window)
	if err != nil || len(data) != window || !eof {
		t.Fatalf("sparse tail read: len=%d eof=%v err=%v", len(data), eof, err)
	}
	if !bytes.Equal(data, make([]byte, window)) {
		t.Fatal("sparse tail is not zeros")
	}

	// Allocations follow the window, not the file.
	smallAllocs := testing.AllocsPerRun(20, func() {
		if _, _, err := ss.ReadAt(context.Background(), 0, window); err != nil {
			t.Fatal(err)
		}
	})
	sparseAllocs := testing.AllocsPerRun(20, func() {
		if _, _, err := sp.ReadAt(context.Background(), sparseSize-window, window); err != nil {
			t.Fatal(err)
		}
	})
	if sparseAllocs > smallAllocs+1 {
		t.Fatalf("allocations scale with file size: small %.0f sparse %.0f", smallAllocs, sparseAllocs)
	}

	// Wall time follows the window, not the file: the minimum of a few
	// runs keeps scheduling noise out, and a sequential scan of eight
	// gigabytes would miss the bound by orders of magnitude.
	best := func(s *Session, off int64) time.Duration {
		d := time.Hour
		for range 5 {
			start := time.Now()
			if _, _, err := s.ReadAt(context.Background(), off, window); err != nil {
				t.Fatal(err)
			}
			if took := time.Since(start); took < d {
				d = took
			}
		}
		return d
	}
	smallTime, sparseTime := best(ss, 0), best(sp, sparseSize-window)
	t.Logf("first-window read: 1MiB file %v, 8GiB file %v", smallTime, sparseTime)
	if sparseTime > smallTime*10+time.Millisecond {
		t.Fatalf("read cost scales with file size: small %v sparse %v", smallTime, sparseTime)
	}
}

func bump(t *testing.T, path string) {
	t.Helper()
	when := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestReadAtFileChanged(t *testing.T) {
	path := writeFile(t, 100, 'x')
	s := open(t, path)
	bump(t, path)
	if _, _, err := s.ReadAt(context.Background(), 0, 10); !errors.Is(err, ErrChanged) {
		t.Fatalf("mtime change: %v", err)
	}

	s = open(t, path)
	if err := os.Truncate(path, 50); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadAt(context.Background(), 0, 10); !errors.Is(err, ErrChanged) {
		t.Fatalf("truncate: %v", err)
	}
}

func TestScanCoversInOrder(t *testing.T) {
	path := writeFile(t, 1000, 'x')
	s := open(t, path)
	var order []int64
	var total int
	err := s.Scan(context.Background(), 100, 100, func(off int64, data []byte) bool {
		order = append(order, off)
		total += len(data)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 900 {
		t.Fatalf("scanned %d bytes of 900", total)
	}
	for i, off := range order {
		if off != int64(100+i*100) {
			t.Fatalf("chunk %d at %d", i, off)
		}
	}

	// Stopping early delivers no error and ends the scan.
	seen := 0
	err = s.Scan(context.Background(), 0, 100, func(off int64, data []byte) bool {
		seen++
		return seen < 2
	})
	if err != nil || seen != 2 {
		t.Fatalf("early stop: seen=%d err=%v", seen, err)
	}

	// Starting past the end scans nothing.
	err = s.Scan(context.Background(), 1000, 100, func(off int64, data []byte) bool {
		t.Fatal("yielded past the end")
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestScanCancellation(t *testing.T) {
	path := writeFile(t, 1<<20, 'x')
	s := open(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	yielded := 0
	err := s.Scan(ctx, 0, 1<<10, func(off int64, data []byte) bool {
		yielded++
		cancel()
		return true
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel mid-scan: %v", err)
	}
	if yielded != 1 {
		t.Fatalf("scan went on after cancellation: %d chunks", yielded)
	}

	// A canceled scan frees the session and the limiter slot: a fresh
	// scan on a one-slot limiter completes instead of hanging.
	limit := NewLimiter(1)
	s2, err := OpenWithLimiter(path, limit)
	if err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	if err := s2.Scan(ctx2, 0, 1<<10, func(int64, []byte) bool { cancel2(); return true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("setup cancel: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- s2.Scan(context.Background(), 0, 1<<20, func(int64, []byte) bool { return true })
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("scan after cancellation never ran: the slot leaked")
	}
}

func TestScanFileChangedMidway(t *testing.T) {
	path := writeFile(t, 1<<20, 'x')
	s := open(t, path)
	changed := false
	err := s.Scan(context.Background(), 0, 1<<10, func(off int64, data []byte) bool {
		if !changed {
			changed = true
			bump(t, path)
		}
		return true
	})
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("change mid-scan: %v", err)
	}
}

func TestScanAwaitsOneSlot(t *testing.T) {
	path := writeFile(t, 4<<10, 'x')
	limit := NewLimiter(1)
	s, err := OpenWithLimiter(path, limit)
	if err != nil {
		t.Fatal(err)
	}

	// The first scan parks inside yield, holding the only slot.
	entered := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- s.Scan(context.Background(), 0, 4<<10, func(int64, []byte) bool {
			close(entered)
			<-release
			return false
		})
	}()
	<-entered

	// The second scan queues behind it and never starts yielding.
	yielded := make(chan struct{}, 1)
	second := make(chan error, 1)
	go func() {
		second <- s.Scan(context.Background(), 0, 4<<10, func(int64, []byte) bool {
			yielded <- struct{}{}
			return true
		})
	}()
	select {
	case <-yielded:
		t.Fatal("second scan yielded while the only slot was held")
	case <-time.After(100 * time.Millisecond):
	}

	// Cancelling a queued scan resolves it with the context's error.
	ctx, cancel := context.WithCancel(context.Background())
	third := make(chan error, 1)
	go func() {
		third <- s.Scan(ctx, 0, 4<<10, func(int64, []byte) bool { return true })
	}()
	cancel()
	select {
	case err := <-third:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued scan cancellation: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("queued scan never noticed cancellation")
	}

	// Releasing the first scan lets the queued second one run.
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-second:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("second scan never ran after the slot freed")
	}
}

func BenchmarkReadSparseFileWindow(b *testing.B) {
	f, err := os.CreateTemp(b.TempDir(), "sparse")
	if err != nil {
		b.Fatal(err)
	}
	const size = int64(8 << 30)
	if err := f.Truncate(size); err != nil {
		b.Fatal(err)
	}
	f.Close()
	s, err := Open(f.Name())
	if err != nil {
		b.Fatal(err)
	}
	const window = 64 << 10
	b.SetBytes(window)
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := s.ReadAt(context.Background(), size-window, window); err != nil {
			b.Fatal(err)
		}
	}
}
