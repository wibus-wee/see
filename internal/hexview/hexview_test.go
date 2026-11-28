package hexview

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"see/internal/rangeio"
)

func TestFormat(t *testing.T) {
	got := Format(16, []byte{'A', 0, 0x7f, 'z'})
	if !strings.HasPrefix(got, "0000000000000010  41 00 7F 7A") || !strings.HasSuffix(got, "|A..z|") {
		t.Fatal(got)
	}
	if Rows(0) != 0 || Rows(17) != 2 {
		t.Fatal("wrong row count")
	}
	if PageLen(0, PageBytes) != PageBytes || PageLen(0, PageBytes+3) != PageBytes || PageLen(PageBytes, PageBytes+3) != 3 {
		t.Fatal("wrong page length")
	}
}

func BenchmarkFormatRow(b *testing.B) {
	data := []byte("0123456789abcdef")
	b.ReportAllocs()
	for b.Loop() {
		_ = Format(0x12345678, data)
	}
}

func sessionFor(t *testing.T, data []byte) *rangeio.Session {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := rangeio.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFindAcrossChunks(t *testing.T) {
	data := bytes.Repeat([]byte{'x'}, rangeio.ScanChunk+32)
	copy(data[rangeio.ScanChunk-2:], "needle")
	s := sessionFor(t, data)
	off, err := Find(context.Background(), s, 0, []byte("needle"))
	if err != nil || off != rangeio.ScanChunk-2 {
		t.Fatalf("offset %d: %v", off, err)
	}
	if _, err = Find(context.Background(), s, off+1, []byte("needle")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected no match: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Find(ctx, s, 0, []byte("needle")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}

	p, err := Pattern("hex: 00 ff 41")
	if err != nil || !bytes.Equal(p, []byte{0, 255, 65}) {
		t.Fatalf("pattern %x %v", p, err)
	}
	if _, err := Pattern("hex: 0"); err == nil {
		t.Fatal("accepted odd hex")
	}

	// A negative start is an error like Scan's and ReadAt's, never a
	// match outside the file — an empty pattern included.
	if _, err := Find(context.Background(), s, -5, nil); err == nil {
		t.Fatal("empty pattern matched at a negative start")
	}
	if _, err := Find(context.Background(), s, -5, []byte("needle")); err == nil {
		t.Fatal("accepted a negative start")
	}
}

func TestFindReportsTheFileChanging(t *testing.T) {
	// The scan of a multi-gigabyte sparse file stays in flight long
	// enough that the version is guaranteed to change mid-scan, so no
	// match position of it can be trusted and the scan must fail
	// loudly rather than mix versions.
	path := filepath.Join(t.TempDir(), "data")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(2 << 30); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	s, err := rangeio.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		when := time.Now().Add(2 * time.Second)
		os.Chtimes(path, when, when)
	}()
	if _, err := Find(context.Background(), s, 0, []byte("absent-needle")); !errors.Is(err, rangeio.ErrChanged) {
		t.Fatalf("change mid-scan: %v", err)
	}
}

func BenchmarkFindInSparseFile(b *testing.B) {
	f, err := os.CreateTemp(b.TempDir(), "sparse")
	if err != nil {
		b.Fatal(err)
	}
	const size = int64(1 << 30)
	if err := f.Truncate(size); err != nil {
		b.Fatal(err)
	}
	f.Close()
	s, err := rangeio.Open(f.Name())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Find(context.Background(), s, 0, []byte("absent-needle")); !errors.Is(err, ErrNotFound) {
			b.Fatal(err)
		}
	}
}
