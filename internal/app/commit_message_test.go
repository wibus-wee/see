package app

import (
	"slices"
	"testing"

	"see/internal/git"
)

// A commit's tab shows its body, its co-authors, and the other trailers
// as written, not the Co-authored-by lines its people already say.
func TestCommitTabShowsCoAuthorsAndBody(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	writeFile(t, root, "b.go", "package a\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "Add pair\n\nBody text.\n\nCo-authored-by: Ada <ada@x.org>\nSigned-off-by: Bob <bob@x.org>")

	h := newHarness(t, root)
	h.readGit()
	h.a.loadHistory(true)
	h.settle(func() bool { return !h.a.git.history.loading })
	cm := h.a.git.history.commits[0]
	h.a.openCommit(cm)
	h.settle(func() bool {
		v := h.a.focus.diffs[commitTab(cm.Hash)]
		return v != nil && !v.commit.loading
	})

	for _, s := range []string{"Body text.", "Ada", "ada@x.org", "Co-author", "Signed-off-by: Bob <bob@x.org>"} {
		if !h.tt.HasText(s) {
			t.Fatalf("the commit tab shows no %q: %q", s, h.tt.Texts())
		}
	}
	if h.tt.HasText("Co-authored-by: Ada") {
		t.Fatalf("the commit tab keeps the trailer line: %q", h.tt.Texts())
	}
}

// The Git log's chosen commit shows the same message block as the tab.
func TestGitLogDetailShowsCoAuthors(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	writeFile(t, root, "b.go", "package a\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "Add pair\n\nBody text.\n\nCo-authored-by: Ada <ada@x.org>")

	h := newHarness(t, root)
	h.readGit()
	h.a.openGitLog()
	h.settle(func() bool { return !h.a.git.history.loading })
	v := h.a.focus.diffs[gitLogTab].log
	h.settle(func() bool { return v.detail != nil && !v.detail.loading })

	for _, s := range []string{"Body text.", "Ada", "ada@x.org", "Co-author"} {
		if !h.tt.HasText(s) {
			t.Fatalf("the log detail shows no %q: %q", s, h.tt.Texts())
		}
	}
}

// Both details show the same hash, subject and refs, not one a short
// hash and the other a full one without the rest. Parents stay out.
func TestCommitDetailsShowTheSameFields(t *testing.T) {
	root := historyRepo(t)
	gitRun(t, root, "tag", "release")
	h := newHarness(t, root)
	h.readGit()
	h.a.loadHistory(true)
	h.settle(func() bool { return !h.a.git.history.loading })
	cm := h.a.git.history.commits[0] // the merge: two parents and refs
	h.a.openCommit(cm)
	h.settle(func() bool {
		v := h.a.focus.diffs[commitTab(cm.Hash)]
		return v != nil && !v.commit.loading
	})
	want := []string{cm.Short, "Merge side", "release"}
	for _, s := range want {
		if !h.tt.HasText(s) {
			t.Fatalf("the commit tab shows no %q: %q", s, h.tt.Texts())
		}
	}
	if h.tt.HasText("Parents") {
		t.Fatalf("the commit tab still shows parents: %q", h.tt.Texts())
	}

	h.a.openGitLog()
	h.settle(func() bool { return !h.a.git.history.loading })
	v := h.a.focus.diffs[gitLogTab].log
	h.settle(func() bool { return v.detail != nil && !v.detail.loading })
	for _, s := range want {
		if !h.tt.HasText(s) {
			t.Fatalf("the log detail shows no %q: %q", s, h.tt.Texts())
		}
	}
	if h.tt.HasText("Parents") {
		t.Fatalf("the log detail still shows parents: %q", h.tt.Texts())
	}
}

// A commit at HEAD shows its branch and one tag; other branches stay
// out of the detail.
func TestVisibleCommitRefs(t *testing.T) {
	tests := []struct {
		name       string
		cm         git.Commit
		current    string
		wantShown  []string
		wantActive string
	}{
		{
			name:       "head branch",
			cm:         git.Commit{Refs: []string{"HEAD", "feat", "main", "origin/main"}},
			current:    "feat",
			wantShown:  []string{"feat"},
			wantActive: "feat",
		},
		{
			name:       "head branch and tag",
			cm:         git.Commit{Refs: []string{"HEAD", "feat", "main"}, Tags: []string{"v1.0"}},
			current:    "feat",
			wantShown:  []string{"feat", "v1.0"},
			wantActive: "feat",
		},
		{
			name:       "tag before branch away from head",
			cm:         git.Commit{Refs: []string{"main", "origin/main"}, Tags: []string{"v1.0"}},
			current:    "feat",
			wantShown:  []string{"v1.0"},
			wantActive: "",
		},
		{
			name:      "first ref without a tag",
			cm:        git.Commit{Refs: []string{"main", "origin/main"}},
			current:   "feat",
			wantShown: []string{"main"},
		},
		{
			name:    "no refs",
			cm:      git.Commit{Refs: []string{"HEAD"}},
			current: "feat",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shown, active := visibleCommitRefs(tt.cm, tt.current)
			if !slices.Equal(shown, tt.wantShown) || active != tt.wantActive {
				t.Fatalf("shown %q active %q, want %q %q", shown, active, tt.wantShown, tt.wantActive)
			}
		})
	}
}

// An unsigned commit shows no GPG key; a signed one shows its Key ID.
func TestCommitTabShowsOnlySignedCommits(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	h := newHarness(t, root)
	h.readGit()
	h.a.loadHistory(true)
	h.settle(func() bool { return !h.a.git.history.loading })
	cm := h.a.git.history.commits[0]
	h.a.openCommit(cm)
	h.settle(func() bool {
		v := h.a.focus.diffs[commitTab(cm.Hash)]
		return v != nil && !v.commit.loading
	})
	if h.tt.HasText("GPG") {
		t.Fatalf("an unsigned commit shows a GPG key: %q", h.tt.Texts())
	}

	v := h.a.focus.diffs[commitTab(cm.Hash)]
	v.commit.details.Signature = git.CommitSignature{Key: "3C1F165A4275D886"}
	h.tt.Frame()
	if !h.tt.HasText("3C1F165A4275D886") {
		t.Fatalf("the signed commit shows no GPG key: %q", h.tt.Texts())
	}
}
