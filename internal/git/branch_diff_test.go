package git

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestCompareBranchUsesMergeBaseAndWorkingTree(t *testing.T) {
	root, run := newRepo(t)
	write(t, root, ".gitignore", "ignored-*\n")
	write(t, root, "working.txt", "base\n")
	write(t, root, "renamed.txt", strings.Repeat("rename me\n", 8))
	write(t, root, "deleted.txt", "gone\n")
	write(t, root, "binary.dat", "old\x00binary")
	run("add", ".")
	run("commit", "-qm", "shared")
	run("switch", "-qc", "feature")
	write(t, root, "committed.txt", "committed change\n")
	run("add", "committed.txt")
	run("commit", "-qm", "feature work")
	run("switch", "-qc", "base-only", "main")
	write(t, root, "base-only.txt", "only on the base branch\n")
	run("add", "base-only.txt")
	run("commit", "-qm", "base-only work")
	run("switch", "-q", "feature")
	write(t, root, "staged.txt", "staged change\n")
	run("add", "staged.txt")
	write(t, root, "working.txt", "base\nworking change\n")
	run("mv", "renamed.txt", "moved.txt")
	run("rm", "deleted.txt")
	write(t, root, "binary.dat", "new\x00binary")
	write(t, root, "untracked.txt", "untracked change\n")
	write(t, root, "untracked-binary.dat", "new\x00untracked binary")
	write(t, root, "ignored-file", "ignored\n")
	strange := "odd\tname\n.txt"
	if runtime.GOOS == "windows" {
		strange = "odd name.txt"
	}
	write(t, root, strange, "odd path\n")

	comparison, err := CompareBranch(context.Background(), root, "refs/heads/base-only")
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, len(comparison.Files))
	for i, f := range comparison.Files {
		paths[i] = f.Path
	}
	for _, path := range []string{"committed.txt", "staged.txt", "working.txt", "moved.txt", "deleted.txt", "binary.dat", "untracked.txt", "untracked-binary.dat", strange} {
		if !slices.Contains(paths, path) {
			t.Errorf("missing %q from %q", path, paths)
		}
	}
	for _, path := range []string{"base-only.txt", "ignored-file"} {
		if slices.Contains(paths, path) {
			t.Errorf("unexpected %q in %q", path, paths)
		}
	}
	find := func(path string) (BranchDiffFile, bool) {
		for _, f := range comparison.Files {
			if f.Path == path {
				return f, true
			}
		}
		return BranchDiffFile{}, false
	}
	if f, ok := find("moved.txt"); !ok || f.OldPath != "renamed.txt" || f.Untracked {
		t.Errorf("rename = %+v, %v", f, ok)
	}
	if f, ok := find("deleted.txt"); !ok || f.Added != 0 || f.Deleted == 0 {
		t.Errorf("deletion = %+v, %v", f, ok)
	}
	if f, ok := find("binary.dat"); !ok || f.Added != -1 || f.Deleted != -1 {
		t.Errorf("binary = %+v, %v", f, ok)
	}
	if f, ok := find("untracked-binary.dat"); !ok || !f.Untracked || f.Added != -1 || f.Deleted != -1 {
		t.Errorf("untracked binary = %+v, %v", f, ok)
	}
	if f, ok := find("untracked.txt"); !ok || !f.Untracked || f.Added == 0 {
		t.Errorf("untracked = %+v, %v", f, ok)
	}
	if !strings.Contains(string(comparison.Patch), "committed change") || !strings.Contains(string(comparison.Patch), "untracked change") {
		t.Errorf("patch omits working-tree changes: %q", comparison.Patch)
	}
	if !strings.Contains(string(comparison.Patch), "Binary files ") || strings.Contains(string(comparison.Patch), "GIT binary patch") {
		t.Errorf("binary patch lacks its indicator or includes an encoded blob: %q", comparison.Patch)
	}
	if strings.Contains(string(comparison.Patch), "base-only work") || strings.Contains(string(comparison.Patch), "only on the base branch") {
		t.Errorf("patch includes base-only commits: %q", comparison.Patch)
	}
	patchName := strings.NewReplacer("\t", `\t`, "\n", `\n`).Replace(strange)
	if !strings.Contains(string(comparison.Patch), patchName) {
		t.Errorf("patch omits unusual path %q: %q", strange, comparison.Patch)
	}
}

func TestNoIndexDiffAcceptsExitStatusOneWithWarnings(t *testing.T) {
	root, run := newRepo(t)
	write(t, root, "untracked.txt", "untracked change\n")
	run("config", "core.autocrlf", "true")
	run("config", "core.safecrlf", "warn")

	patch, err := noIndexDiff(context.Background(), root, "untracked.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(patch), "diff --git ") {
		t.Fatalf("untracked patch = %q", patch)
	}

	numstat, err := noIndexDiff(context.Background(), root, "untracked.txt", "--numstat", "-z")
	if err != nil {
		t.Fatal(err)
	}
	files := ParseNumstat(numstat)
	if len(files) != 1 || files[0].Path != "untracked.txt" || files[0].Added == 0 {
		t.Fatalf("untracked numstat = %+v from %q", files, numstat)
	}
}

func TestCompareBranchRejectsMissingAndUnrelatedRefs(t *testing.T) {
	root, run := newRepo(t)
	run("commit", "--allow-empty", "-qm", "shared")
	if _, err := CompareBranch(context.Background(), root, "refs/heads/missing"); err == nil {
		t.Fatal("missing ref succeeded")
	}
	run("switch", "--orphan", "unrelated")
	run("commit", "--allow-empty", "-qm", "unrelated")
	run("switch", "-q", "main")
	if _, err := CompareBranch(context.Background(), root, "refs/heads/unrelated"); err == nil {
		t.Fatal("unrelated histories succeeded")
	}
}

func TestCompareBranchEmpty(t *testing.T) {
	root, run := newRepo(t)
	write(t, root, "unchanged.txt", "same\n")
	run("add", "unchanged.txt")
	run("commit", "-qm", "base")

	comparison, err := CompareBranch(context.Background(), root, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if comparison.BaseHash == "" || comparison.MergeBase != comparison.BaseHash {
		t.Fatalf("empty comparison refs = base %q, merge base %q", comparison.BaseHash, comparison.MergeBase)
	}
	if len(comparison.Files) != 0 || len(comparison.Patch) != 0 {
		t.Fatalf("clean comparison = files %v, patch %q", comparison.Files, comparison.Patch)
	}
}

func TestCompareBranchDoesNotAlterWorktreeOrIndex(t *testing.T) {
	root, run := newRepo(t)
	write(t, root, "a.txt", "base\n")
	run("add", "a.txt")
	run("commit", "-qm", "base")
	run("switch", "-qc", "feature")
	write(t, root, "a.txt", "changed\n")
	before, err := Command(context.Background(), root, "status", "--porcelain=v1", "-z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompareBranch(context.Background(), root, "refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	after, err := Command(context.Background(), root, "status", "--porcelain=v1", "-z")
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("comparison changed status: before %q, after %q", before, after)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
}
