package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStashSubject(t *testing.T) {
	for _, tc := range []struct{ subject, branch, message string }{
		{"On main: tidy the parser", "main", "tidy the parser"},
		{"WIP on feature/x: 1a2b3c4 Add parser", "feature/x", "WIP: Add parser"},
		{"On (no branch): detached work", "", "detached work"},
		{"odd subject", "", "odd subject"},
	} {
		branch, message := parseStashSubject(tc.subject)
		if branch != tc.branch || message != tc.message {
			t.Errorf("%q: got %q, %q", tc.subject, branch, message)
		}
	}
}

// A stash takes tracked, staged and untracked changes; its files and
// diffs read them back, and applying restores what was staged as staged.
func TestStashPushListFilesApplyDrop(t *testing.T) {
	root, run := newRepo(t)
	ctx := context.Background()
	write(t, root, "a.txt", "a\n")
	write(t, root, "b.txt", "b\n")
	run("add", ".")
	run("commit", "-qm", "first")

	write(t, root, "a.txt", "a\nchanged\n")
	write(t, root, "b.txt", "b\nstaged\n")
	run("add", "b.txt")
	write(t, root, "new.txt", "new\n")
	if err := StashPush(ctx, root, "  parser work  "); err != nil {
		t.Fatal(err)
	}
	if out := run("status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Fatalf("working tree not clean after stashing: %q", out)
	}
	write(t, root, "a.txt", "a\nsecond\n")
	if err := StashPush(ctx, root, ""); err != nil {
		t.Fatal(err)
	}

	stashes, err := Stashes(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(stashes) != 2 {
		t.Fatalf("stashes %+v", stashes)
	}
	if s := stashes[0]; s.Index != 0 || s.Branch != "main" || s.Message != "WIP: first" || s.Date.IsZero() {
		t.Fatalf("newest %+v", s)
	}
	work := stashes[1]
	if work.Ref() != "stash@{1}" || work.Message != "parser work" {
		t.Fatalf("older %+v", work)
	}
	d, err := Show(ctx, root, work.Hash)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range d.Files {
		files = append(files, f.Path)
	}
	if got := strings.Join(files, ","); got != "a.txt,b.txt,new.txt" {
		t.Fatalf("stash files %s", got)
	}
	if out, err := CommitFileDiff(ctx, root, work.Hash, FileStat{Path: "new.txt"}); err != nil || !strings.Contains(string(out), "+new") {
		t.Fatalf("untracked diff %q %v", out, err)
	}
	if out, err := CommitFileDiff(ctx, root, work.Hash, FileStat{Path: "a.txt"}); err != nil || !strings.Contains(string(out), "+changed") {
		t.Fatalf("tracked diff %q %v", out, err)
	}

	// Dropping the newest shifts the older to stash@{0}; acting on the
	// one read before still finds it by hash.
	if err := StashDrop(ctx, root, stashes[0]); err != nil {
		t.Fatal(err)
	}
	if err := StashApply(ctx, root, work, true); err != nil {
		t.Fatal(err)
	}
	if left, _ := Stashes(ctx, root); len(left) != 0 {
		t.Fatalf("left %+v", left)
	}
	status := run("status", "--porcelain")
	for _, want := range []string{" M a.txt", "M  b.txt", "?? new.txt"} {
		if !strings.Contains(status, want) {
			t.Errorf("status %q lacks %q", status, want)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a.txt")); strings.ReplaceAll(string(b), "\r\n", "\n") != "a\nchanged\n" {
		t.Errorf("a.txt %q", b)
	}
	if err := StashDrop(ctx, root, work); err == nil {
		t.Fatal("dropped a stash no longer listed")
	}
}

// Two entries that store one commit are told apart by their index.
func TestStashDropDuplicateCommit(t *testing.T) {
	root, run := newRepo(t)
	ctx := context.Background()
	write(t, root, "a.txt", "a\n")
	run("add", ".")
	run("commit", "-qm", "first")
	write(t, root, "a.txt", "a\nchanged\n")
	if err := StashPush(ctx, root, "older"); err != nil {
		t.Fatal(err)
	}
	hash := strings.TrimSpace(run("rev-parse", "stash@{0}"))
	write(t, root, "a.txt", "a\nother\n")
	if err := StashPush(ctx, root, "between"); err != nil {
		t.Fatal(err)
	}
	run("stash", "store", "-m", "newer", hash)
	stashes, err := Stashes(ctx, root)
	if err != nil || len(stashes) != 3 || stashes[0].Hash != stashes[2].Hash {
		t.Fatalf("stashes %+v %v", stashes, err)
	}
	if err := StashDrop(ctx, root, stashes[2]); err != nil {
		t.Fatal(err)
	}
	if left, _ := Stashes(ctx, root); len(left) != 2 || left[0].Message != "newer" || left[1].Message != "between" {
		t.Fatalf("left %+v", left)
	}
}
