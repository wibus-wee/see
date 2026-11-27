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

func TestReadIgnoredSourcesSeparatesGlobalAndParentRules(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "git")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	globalIgnore := filepath.Join(configDir, ".gitignore")
	write(t, configDir, ".gitignore", "scratch/\nparent-only/\n")
	write(t, home, ".gitconfig", "[core]\n\texcludesFile = "+filepath.ToSlash(globalIgnore)+"\n")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	root, _ := newRepo(t)
	if err := os.MkdirAll(filepath.Join(root, "scratch"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, root, "scratch/x", "x")
	ig, err := ReadIgnoredSources(context.Background(), root, true, true)
	if err != nil || !ig.Dir("scratch") {
		t.Fatalf("global ignore missing: %+v %v", ig, err)
	}
	ig, err = ReadIgnoredSources(context.Background(), root, false, true)
	if err != nil || ig.Dir("scratch") {
		t.Fatalf("global ignore survived disablement: %+v %v", ig, err)
	}

	write(t, root, ".gitignore", "sub/parent-only/\n")
	if err := os.MkdirAll(filepath.Join(root, "sub", "parent-only"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, root, "sub/parent-only/x", "x")
	sub := filepath.Join(root, "sub")
	ig, err = ReadIgnoredSources(context.Background(), sub, true, true)
	if err != nil || !ig.Dir("parent-only") {
		t.Fatalf("parent ignore missing: %+v %v", ig, err)
	}
	ig, err = ReadIgnoredSources(context.Background(), sub, false, false)
	if err != nil || ig.Dir("parent-only") {
		t.Fatalf("parent ignore survived disablement: %+v %v", ig, err)
	}
	ig, err = ReadIgnoredSources(context.Background(), sub, true, false)
	if err != nil || !ig.Dir("parent-only") {
		t.Fatalf("enabled global ignore hidden by disabled parent rule: %+v %v", ig, err)
	}
}
