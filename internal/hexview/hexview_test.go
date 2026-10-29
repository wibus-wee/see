package hexview

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormat(t *testing.T) {
	got := Format(16, []byte{'A', 0, 0x7f, 'z'})
	if !strings.HasPrefix(got, "0000000000000010  41 00 7F 7A") || !strings.HasSuffix(got, "|A..z|") {
		t.Fatal(got)
	}
	if Rows(0) != 0 || Rows(17) != 2 {
		t.Fatal("wrong row count")
	}
}
func TestReadPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	data := bytes.Repeat([]byte{0x41, 0x42}, PageBytes/2+3)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPage(path, PageBytes, int64(len(data)))
	if err != nil || !bytes.Equal(got, data[PageBytes:]) {
		t.Fatalf("read: %v %v", got, err)
	}
	if _, err := ReadPage(path, 1, int64(len(data))); err == nil {
		t.Fatal("accepted unaligned offset")
	}
	if err := os.Truncate(path, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPage(path, 0, int64(len(data))); err == nil {
		t.Fatal("accepted truncated file")
	}
}
func BenchmarkFormatRow(b *testing.B) {
	data := []byte("0123456789abcdef")
	b.ReportAllocs()
	for b.Loop() {
		_ = Format(0x12345678, data)
	}
}
func BenchmarkReadSparseFilePage(b *testing.B) {
	f, err := os.CreateTemp(b.TempDir(), "sparse")
	if err != nil {
		b.Fatal(err)
	}
	const size = int64(8 << 30)
	if err := f.Truncate(size); err != nil {
		b.Fatal(err)
	}
	f.Close()
	b.SetBytes(PageBytes)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ReadPage(f.Name(), size-PageBytes, size); err != nil {
			b.Fatal(err)
		}
	}
}

func TestFindAcrossPages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	data := bytes.Repeat([]byte{'x'}, PageBytes+32)
	copy(data[PageBytes-2:], "needle")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	off, err := Find(context.Background(), path, int64(len(data)), 0, []byte("needle"))
	if err != nil || off != PageBytes-2 {
		t.Fatalf("offset %d: %v", off, err)
	}
	if _, err = Find(context.Background(), path, int64(len(data)), off+1, []byte("needle")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected no match: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Find(ctx, path, int64(len(data)), 0, []byte("needle")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	p, err := Pattern("hex: 00 ff 41")
	if err != nil || !bytes.Equal(p, []byte{0, 255, 65}) {
		t.Fatalf("pattern %x %v", p, err)
	}
	if _, err := Pattern("hex: 0"); err == nil {
		t.Fatal("accepted odd hex")
	}
}
