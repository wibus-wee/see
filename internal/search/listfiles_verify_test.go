package search

// Verifier-added evidence (branch verify-lane-b) for the ownership-crossing
// change Lane B made in ListFiles: the ValidRel filter must not drop
// legitimate files, however oddly they are named.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"see/internal/workspace"
)

func validRelForTest(rel string) bool { return workspace.ValidRel(rel) }

func TestListFilesKeepsOddButLegitimateNames(t *testing.T) {
	rg, err := FindRipgrep("")
	if err != nil {
		t.Skipf("no rg: %v", err)
	}
	root := t.TempDir()
	names := []string{
		"plain.go",
		"with space.go",
		"100%.go",
		"utf-文件.go",
		"trailing.",
		"semi;colon.go",
		"quote'quote.go",
		"back\\slash-in-name.go", // legal on POSIX
	}
	// Names leading with a dot are hidden files, which rg --files leaves
	// out by design — independent of the ValidRel filter. What the filter
	// must not do is reject one itself: ValidRel("..dots-in-name.go") is
	// true (checked below), so if such a line ever reaches the scan it
	// survives.
	if !validRelForTest("..dots-in-name.go") {
		t.Error("ValidRel rejects a legitimate dot-leading file name")
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(n)), []byte("x"), 0o644); err != nil {
			t.Fatalf("create %q: %v", n, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	files, _, err := ListFiles(ctx, rg, root, 1000, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	for _, f := range files {
		set[f] = true
	}
	for _, n := range names {
		slash := filepath.ToSlash(filepath.FromSlash(n))
		if !set[slash] {
			t.Errorf("legitimate file %q dropped from quick open: entries %v", n, files)
		}
	}
}

func TestListFilesNewlineNameFragments(t *testing.T) {
	rg, err := FindRipgrep("")
	if err != nil {
		t.Skipf("no rg: %v", err)
	}
	root := t.TempDir()
	// A newline in a file name is legal on POSIX. rg prints the raw bytes,
	// splitting the entry across lines.
	nl := "first\nsecond.go"
	if err := os.WriteFile(filepath.Join(root, nl), []byte("x"), 0o644); err != nil {
		t.Fatalf("newline file unsupported here: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	files, _, err := ListFiles(ctx, rg, root, 1000, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("entries for a newline-named file: %q", files)
	joined := strings.Join(files, "")
	if !strings.Contains(joined, "first") || !strings.Contains(joined, "second.go") {
		t.Errorf("the file vanished entirely rather than fragmenting: %q", files)
	}
	// What matters for safety: no fragment may escape the root when joined.
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil || strings.HasPrefix(rel, "..") {
			t.Errorf("entry %q escapes the root when joined", f)
		}
	}
}
