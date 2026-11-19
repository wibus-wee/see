package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReplaceRipgrepMatchesPreservesEncoding(t *testing.T) {
	const source = "\ufeff猫 SEED seed\r\nseedlings\nseed\r\n"
	root := tree(t, map[string]string{"sub/a.txt": source})
	path := filepath.Join(root, "sub", "a.txt")
	if err := os.Chmod(path, 0o751); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := run(t, ripgrep(t), root, Options{Query: "se+d", Regexp: true, WholeWord: true})
	dir, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	rel := filepath.Join("sub", "a.txt")
	n, err := ReplaceFile(t.Context(), dir, rel, matches["sub/a.txt"], "$1 🧪")
	if n != 3 || err != nil {
		t.Fatalf("ReplaceFile = %d, %v", n, err)
	}
	got, err := os.ReadFile(path)
	if want := "\ufeff猫 $1 🧪 $1 🧪\r\nseedlings\n$1 🧪\r\n"; err != nil || string(got) != want {
		t.Fatalf("file = %q, %v; want %q", got, err, want)
	}
	if after, err := os.Stat(path); err != nil || after.Mode().Perm() != before.Mode().Perm() {
		t.Fatalf("permissions changed: %v, %v", after, err)
	}
}

func TestReplaceRejectsStaleMatchWithoutPartialWrite(t *testing.T) {
	root := tree(t, map[string]string{"a.txt": "seed\nseed and original context\n"})
	matches, _ := run(t, ripgrep(t), root, Options{Query: "seed"})
	path := filepath.Join(root, "a.txt")
	const updated = "seed\nseed and modified context\n"
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if n, err := ReplaceFile(t.Context(), dir, "a.txt", matches["a.txt"], ""); n != 0 || !errors.Is(err, ErrStaleMatches) {
		t.Fatalf("ReplaceFile = %d, %v", n, err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != updated {
		t.Fatalf("stale file = %q, %v", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if n, err := ReplaceFile(ctx, dir, "a.txt", matches["a.txt"], ""); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ReplaceFile = %d, %v", n, err)
	}
}

func TestReplaceDoesNotFollowSymlinkOrOverwriteReadOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX permissions and symlinks")
	}
	root := tree(t, map[string]string{"a.txt": "seed"})
	other := tree(t, map[string]string{"a.txt": "seed"})
	matches, _ := run(t, ripgrep(t), root, Options{Query: "seed"})
	path := filepath.Join(root, "a.txt")
	outside := filepath.Join(other, "a.txt")
	dir, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	if n, err := ReplaceFile(t.Context(), dir, "a.txt", matches["a.txt"], "changed"); n != 0 || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("read-only ReplaceFile = %d, %v", n, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if n, err := ReplaceFile(t.Context(), dir, "a.txt", matches["a.txt"], "changed"); n != 0 || !errors.Is(err, ErrNotText) {
		t.Fatalf("symlink ReplaceFile = %d, %v", n, err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "seed" {
		t.Fatalf("symlink target = %q, %v", got, err)
	}
}
