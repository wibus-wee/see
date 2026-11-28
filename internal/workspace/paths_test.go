package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWithin(t *testing.T) {
	root := filepath.FromSlash("/w/project")
	for _, tc := range []struct {
		p    string
		want bool
	}{
		{"/w/project", true},
		{"/w/project/a/b.go", true},
		{"/w/project/..file", true},
		{"/w/projectile", false},
		{"/w", false},
		{"/w/other/x", false},
	} {
		if got := Within(root, filepath.FromSlash(tc.p)); got != tc.want {
			t.Errorf("Within(%q, %q) = %v", root, tc.p, got)
		}
	}
}

func TestDocPathMapsRealRootToOpenedRoot(t *testing.T) {
	root, real := filepath.FromSlash("/tmp/ws"), filepath.FromSlash("/private/tmp/ws")
	if got, want := DocPath(root, real, filepath.FromSlash("/private/tmp/ws/a/../b.go")), filepath.FromSlash("/tmp/ws/b.go"); got != want {
		t.Fatalf("through the real root: %q, want %q", got, want)
	}
	if got, want := DocPath(root, real, filepath.FromSlash("/tmp/ws/c.go")), filepath.FromSlash("/tmp/ws/c.go"); got != want {
		t.Fatalf("under the opened root: %q, want %q", got, want)
	}
	if got, want := DocPath(root, real, filepath.FromSlash("/elsewhere/d.go")), filepath.FromSlash("/elsewhere/d.go"); got != want {
		t.Fatalf("outside: %q, want %q", got, want)
	}
}

func TestRelPath(t *testing.T) {
	root := filepath.FromSlash("/w/project")
	if got := RelPath(root, filepath.FromSlash("/w/project/a/b.go")); got != "a/b.go" {
		t.Fatalf("inside: %q", got)
	}
	if p := filepath.FromSlash("/w/other.go"); RelPath(root, p) != p {
		t.Fatalf("outside: %q", RelPath(root, p))
	}
}

func TestCanonicalRoot(t *testing.T) {
	dir := t.TempDir()
	got, err := CanonicalRoot(filepath.Join(dir, "x", ".."))
	if err != nil || got != filepath.Clean(dir) {
		t.Fatalf("CanonicalRoot = %q, %v", got, err)
	}
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := CanonicalRoot(file); !errors.Is(err, ErrNotDir) {
		t.Fatalf("a file: %v", err)
	}
}

func TestJoinRelConfines(t *testing.T) {
	root := filepath.FromSlash("/w/project")
	for _, tc := range []struct{ rel, want string }{
		{"a/b.go", "a/b.go"},
		{"a//b.go", "a/b.go"},
		{"./a/./b", "a/b"},
		{"a/../b.go", "b.go"},
		{"a/x/../../b.go", "b.go"},
		{"", ""},
		{".", ""},
		{"a//", "a"},
	} {
		got, err := JoinRel(root, tc.rel)
		if err != nil {
			t.Errorf("JoinRel(%q) err = %v", tc.rel, err)
			continue
		}
		want := root
		if tc.want != "" {
			want = filepath.Join(root, filepath.FromSlash(tc.want))
		}
		if got != want {
			t.Errorf("JoinRel(%q) = %q, want %q", tc.rel, got, want)
		}
	}
	for _, rel := range []string{"..", "../outside", "a/../../outside", "../../etc/passwd", "/etc/passwd", "a/..\x00/b"} {
		if got, err := JoinRel(root, rel); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("JoinRel(%q) = %q, %v; want ErrOutsideRoot", rel, got, err)
		}
	}
	// The crucial case of the specification: an intermediate directory
	// balanced by .. but escaping first.
	if _, err := JoinRel(root, "a/../../outside"); !errors.Is(err, ErrOutsideRoot) {
		t.Error("root/a/../../outside must be refused")
	}
	// Components ending in a dot or a space are refused on every
	// platform: Win32 normalization strips them at open time, so the
	// lexical join cannot confine them.
	for _, rel := range []string{".. /x", "notes.", "file ", "d /f"} {
		if got, err := JoinRel(root, rel); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("JoinRel(%q) = %q, %v; want ErrOutsideRoot", rel, got, err)
		}
	}
}

func TestValidRel(t *testing.T) {
	for rel, want := range map[string]bool{
		"a/b.go":       true,
		"a//b.go":      true,
		"a/../b.go":    true,
		"":             false,
		".":            false,
		"/a":           false,
		"../a":         false,
		"a/../../b":    false,
		"a\x00b":       false,
		"../../../etc": false,
		"file ":        false,
		"notes.":        false,
		"a.b/file.txt":  true,
		"my file.txt":   true,
	} {
		if ValidRel(rel) != want {
			t.Errorf("ValidRel(%q) = %v, want %v", rel, !want, want)
		}
	}
}

// caseInsensitiveTempFS probes what the platform's temp directory does
// with case variants, so that tests read as behavior of a real file
// system rather than assumptions about one.
func caseInsensitiveTempFS(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	one := filepath.Join(dir, "Probe-xq71.txt")
	if err := os.WriteFile(one, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi1, err1 := os.Stat(one)
	fi2, err2 := os.Stat(filepath.Join(dir, "probe-xq71.txt"))
	return err1 == nil && err2 == nil && os.SameFile(fi1, fi2)
}

func TestDiskSpellingResolvesCaseVariantsOnDisk(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "Sources")
	if err := os.MkdirAll(filepath.Join(sub, "Deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(sub, "Deep", "My File.go")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The exact spelling answers itself on every platform.
	if got := DiskSpelling(target); got != target {
		t.Errorf("exact: %q, want %q", got, target)
	}
	// A case variant of every component.
	variant := filepath.Join(dir, "sources", "deep", "my file.go")
	got := DiskSpelling(variant)
	if insensitive := caseInsensitiveTempFS(t); insensitive {
		// The variant canonicalizes to the disk's spelling, which is
		// what makes both spellings one document identity.
		if got != target {
			t.Errorf("case-insensitive fs: DiskSpelling(%q) = %q, want %q", variant, got, target)
		}
	} else {
		// A case-sensitive file system spells the variant nowhere: the
		// input comes back unchanged, never merged into another file.
		if got != variant {
			t.Errorf("case-sensitive fs: DiskSpelling(%q) = %q, want the input back", variant, got)
		}
		if _, err := os.Stat(got); !os.IsNotExist(err) {
			t.Errorf("variant must not resolve to the file: %v", err)
		}
	}
	// A missing leaf keeps its spelling while its parents resolve.
	missing := filepath.Join(sub, "deep", "NEW FILE.GO")
	got = DiskSpelling(missing)
	if filepath.Base(got) != "NEW FILE.GO" {
		t.Errorf("missing leaf: %q, want the leaf as typed", got)
	}
	if insensitive := caseInsensitiveTempFS(t); insensitive {
		if filepath.Dir(got) != filepath.Dir(target) {
			t.Errorf("missing leaf parent: %q, want %q", filepath.Dir(got), filepath.Dir(target))
		}
	}
}

func TestFoldEqual(t *testing.T) {
	if !FoldEqual("File.GO", "file.go") {
		t.Error("case")
	}
	// NFC and NFD spellings of é fold together.
	nfc := "caf\u00e9.go"
	nfd := "cafe\u0301.go"
	if !FoldEqual(nfc, nfd) {
		t.Error("unicode normalization")
	}
	if FoldEqual("a.go", "b.go") {
		t.Error("distinct names")
	}
	if FoldEqual("a.go", "a.gox") {
		t.Error("prefix")
	}
	// Full folding, as file systems apply it: Turkish İ folds to
	// i + combining dot above, and uppercase ẞ to ss. Simple case
	// mapping keeps these distinct, which would split one file's
	// spellings into two documents on APFS and NTFS.
	if !FoldEqual("İx.txt", "i̇x.txt") {
		t.Error("full fold of U+0130")
	}
	if !FoldEqual("ẞx.txt", "ssx.txt") {
		t.Error("full fold of U+1E9E")
	}
}

// TestCaseInsensitiveWriteDetectsCollision probes what the platform's
// temp directory does with case variants, so the other tests read as
// behavior of a real file system rather than assumptions about one.
func TestCaseVariantSameFileProbe(t *testing.T) {
	dir := t.TempDir()
	one := filepath.Join(dir, "Probe.txt")
	if err := os.WriteFile(one, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	variant := filepath.Join(dir, "probe.txt")
	fi1, err1 := os.Stat(one)
	fi2, err2 := os.Stat(variant)
	same := err1 == nil && err2 == nil && os.SameFile(fi1, fi2)
	t.Logf("%s: case-insensitive temp fs = %v", runtime.GOOS, same)
	if same {
		if got := DiskSpelling(variant); got != one {
			t.Errorf("case-insensitive fs: DiskSpelling(%q) = %q, want %q", variant, got, one)
		}
	} else {
		// Case-sensitive: the variant must not resolve to the file.
		if got := DiskSpelling(variant); got != variant {
			t.Errorf("case-sensitive fs: DiskSpelling(%q) = %q, want the input back", variant, got)
		}
	}
}
