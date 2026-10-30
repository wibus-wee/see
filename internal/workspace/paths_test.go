package workspace

import (
	"errors"
	"os"
	"path/filepath"
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
