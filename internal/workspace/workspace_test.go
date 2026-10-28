package workspace

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"
)

func TestCompare(t *testing.T) {
	names := []string{"file10", "File2", "file1", "a", "B", "file10b", "file02", "_x", "文件", "Z"}
	slices.SortFunc(names, Compare)
	want := []string{"_x", "a", "B", "file1", "File2", "file02", "file10", "file10b", "Z", "文件"}
	if !slices.Equal(names, want) {
		t.Errorf("got %q\nwant %q", names, want)
	}
	// The order is total: names equal but for case still compare.
	if Compare("readme", "README") == 0 || Compare("readme", "README") != -Compare("README", "readme") {
		t.Error("case variants must order consistently")
	}
}

func TestReadDirSortsDirsFirst(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string, isDir bool) {
		p := filepath.Join(dir, name)
		if isDir {
			os.Mkdir(p, 0o755)
		} else {
			os.WriteFile(p, nil, 0o644)
		}
	}
	mk("zeta", true)
	mk("alpha.go", false)
	mk("Beta", true)
	mk(".hidden", false)
	es, err := ReadDir(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range es {
		got = append(got, e.Name)
	}
	want := []string{"Beta", "zeta", ".hidden", "alpha.go"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadDirLargeAndCanceled(t *testing.T) {
	dir := t.TempDir()
	for i := range 2000 {
		os.WriteFile(filepath.Join(dir, "f"+strconv.Itoa(i)), nil, 0o644)
	}
	es, err := ReadDir(context.Background(), dir)
	if err != nil || len(es) != 2000 {
		t.Fatalf("got %d entries, %v", len(es), err)
	}
	if es[0].Name != "f0" || es[1].Name != "f1" || es[10].Name != "f10" {
		t.Errorf("not natural: %q %q %q", es[0].Name, es[1].Name, es[10].Name)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadDir(ctx, dir); err == nil {
		t.Error("canceled read succeeded")
	}
}

func TestSymlinkLoopIsNotFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "a"), 0o755)
	if err := os.Symlink(dir, filepath.Join(dir, "a", "loop")); err != nil {
		t.Skip(err)
	}
	os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "dangling"))
	es, err := ReadDir(context.Background(), filepath.Join(dir, "a"))
	if err != nil || len(es) != 1 || !es[0].Link || !es[0].Dir {
		t.Fatalf("got %+v, %v", es, err)
	}
	es, err = ReadDir(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if e.Name == "dangling" && (!e.Link || e.Dir) {
			t.Errorf("dangling link: %+v", e)
		}
	}
}

func TestPermissionError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permissions do not apply")
	}
	dir := filepath.Join(t.TempDir(), "locked")
	os.Mkdir(dir, 0o000)
	defer os.Chmod(dir, 0o755)
	if _, err := ReadDir(context.Background(), dir); !os.IsPermission(err) {
		t.Errorf("err = %v, want permission error", err)
	}
}

func TestClassifyAndRead(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, data, 0o644)
		return p
	}
	for _, tc := range []struct {
		data []byte
		kind Kind
	}{
		{[]byte("package main\r\n// 你好 🎉\n"), Text},
		{[]byte("\x7fELF\x00\x01"), Binary},
		{[]byte("caf\xe9"), BadEncoding},
		{[]byte{0xff, 0xfe, 'a', 0}, BadEncoding},
		{nil, Text},
	} {
		if k := Classify(tc.data); k != tc.kind {
			t.Errorf("Classify(%q) = %v, want %v", tc.data, k, tc.kind)
		}
	}
	f, err := ReadFile(write("bom.go", []byte("\xef\xbb\xbfpackage x\n")))
	if err != nil || f.Text != "package x\n" || f.Kind != Text {
		t.Errorf("bom: %+v %v", f, err)
	}
	if _, err := ReadFile(dir); err == nil {
		t.Error("reading a directory succeeded")
	}
	if _, err := ReadFile(filepath.Join(dir, "nope")); !os.IsNotExist(err) {
		t.Errorf("missing file: %v", err)
	}
}

func TestIgnoredRel(t *testing.T) {
	for rel, want := range map[string]bool{
		"node_modules":          true,
		"web/node_modules/x":    true,
		".git/HEAD":             true,
		"cmd/build.go":          false,
		"internal/builder/x.go": false,
	} {
		if IgnoredRel(rel) != want {
			t.Errorf("IgnoredRel(%q) = %v", rel, !want)
		}
	}
}
