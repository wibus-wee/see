package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadIgnored(t *testing.T) {
	root, _ := newRepo(t)
	for _, p := range []string{"sub/out/x", "a.log", "keep/b.log", "keep/c.go", "web/.next/cache/z"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, root, p, "x")
	}
	write(t, root, ".gitignore", "out/\n.next\n*.log\n")
	ig, err := ReadIgnored(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]bool{
		"a.log": true, "keep/b.log": true, "keep/c.go": false, "keep": false,
		"sub": false, "sub/out": true, "sub/out/x": true,
		"web": false, "web/.next": true, "web/.next/cache/z": true,
	} {
		if got := ig.Has(rel); got != want {
			t.Errorf("Has(%q) = %v, want %v", rel, got, want)
		}
	}
	if ig.Dir("a.log") || !ig.Dir("web/.next/cache") {
		t.Error("Dir answers for files, or not for folders in an ignored one")
	}

	// From a folder of the repository, paths are the folder's.
	sub, err := ReadIgnored(context.Background(), filepath.Join(root, "sub"))
	if err != nil || !sub.Dir("out") || sub.Has("x") {
		t.Fatalf("from sub: %+v %v", sub, err)
	}

	// Outside a repository, nothing.
	none, _ := ReadIgnored(context.Background(), t.TempDir())
	if none.Has("anything") {
		t.Fatal("ignored outside a repository")
	}
}
