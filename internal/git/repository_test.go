package git

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestSyncRepositoryRefs(t *testing.T) {
	root, run := newRepo(t)
	ctx := context.Background()
	run("commit", "--allow-empty", "-qm", "first")
	run("branch", "feature/ui")
	run("tag", "v1.0")
	run("remote", "add", "origin", "../local repository.git")
	run("remote", "add", "team/tools", "../tools.git")
	run("remote", "add", "upstream", "../empty.git")
	run("update-ref", "refs/remotes/origin/main", "HEAD")
	run("update-ref", "refs/remotes/team/tools/release/next", "HEAD")
	run("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	s, err := Sync(ctx, root, false)
	if err != nil || s.RefsErr != nil {
		t.Fatalf("Sync: %v, refs: %v", err, s.RefsErr)
	}
	repo := s.Repository
	if repo.Current != "main" || !slices.Equal(repo.Branches, []Ref{
		{Name: "feature/ui", FullName: "refs/heads/feature/ui"},
		{Name: "main", FullName: "refs/heads/main"},
	}) || !slices.Equal(repo.Tags, []Ref{{Name: "v1.0", FullName: "refs/tags/v1.0"}}) {
		t.Fatalf("local refs: %+v", repo)
	}
	if len(repo.Remotes) != 3 {
		t.Fatalf("remotes: %+v", repo.Remotes)
	}
	for i, want := range []RemoteRefs{
		{Name: "origin", Branches: []Ref{{Name: "main", FullName: "refs/remotes/origin/main"}}},
		{Name: "team/tools", Branches: []Ref{{Name: "release/next", FullName: "refs/remotes/team/tools/release/next"}}},
		{Name: "upstream"},
	} {
		got := repo.Remotes[i]
		if got.Name != want.Name || !slices.Equal(got.Branches, want.Branches) {
			t.Fatalf("remote %d: got %+v, want %+v", i, got, want)
		}
	}
	run("switch", "--detach", "HEAD")
	if s, err := Sync(ctx, root, false); err != nil || s.Repository.Current != "" {
		t.Fatalf("detached current: %q, %v", s.Repository.Current, err)
	}
}

func TestCreateTag(t *testing.T) {
	root, run := newRepo(t)
	ctx := context.Background()
	run("commit", "--allow-empty", "-qm", "first")
	first := Head(ctx, root)
	run("commit", "--allow-empty", "-qm", "next")
	for _, tc := range []struct{ name, message, kind string }{
		{"v1.0", "", "commit"}, {"releases/v1.1", "Release notes", "tag"},
	} {
		if err := CreateTag(ctx, root, tc.name, tc.message, first); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(run("cat-file", "-t", tc.name)); got != tc.kind {
			t.Errorf("tag %q type = %q, want %q", tc.name, got, tc.kind)
		}
		if got := Resolve(ctx, root, tc.name+"^{commit}"); got != first {
			t.Errorf("tag %q points to %q, want %q", tc.name, got, first)
		}
	}
	if got := run("cat-file", "-p", "releases/v1.1"); !strings.HasSuffix(got, "\n\nRelease notes\n") {
		t.Errorf("annotation: %s", got)
	}
	for _, name := range []string{"v1.0", "bad name", "--force"} {
		if err := CreateTag(ctx, root, name, "", "HEAD"); err == nil {
			t.Errorf("unexpected tag success for %q", name)
		}
	}
}

func TestCherryPick(t *testing.T) {
	root, run := newRepo(t)
	ctx := context.Background()
	write(t, root, "a", "base\n")
	run("add", "a")
	run("commit", "-qm", "base")
	run("switch", "-qc", "feature")
	write(t, root, "a", "feature\n")
	run("commit", "-qam", "feature change")
	feature := Head(ctx, root)
	run("switch", "-q", "main")
	run("commit", "--allow-empty", "-qm", "main change")
	run("branch", "conflicting")
	if err := CherryPick(ctx, root, feature); err != nil {
		t.Fatal(err)
	}
	if got := run("show", "HEAD:a"); got != "feature\n" || Head(ctx, root) == feature {
		t.Fatalf("cherry-picked content: %q, head: %q", got, Head(ctx, root))
	}
	run("switch", "-q", "conflicting")
	write(t, root, "a", "different\n")
	run("commit", "-qam", "conflicting change")
	before := Head(ctx, root)
	if err := CherryPick(ctx, root, feature); err == nil || !strings.Contains(err.Error(), "CONFLICT") {
		t.Fatalf("cherry-pick error: %v", err)
	}
	if got := Resolve(ctx, root, "CHERRY_PICK_HEAD"); got != feature || Head(ctx, root) != before {
		t.Fatalf("conflict did not preserve the original HEAD and cherry-pick state: %q", got)
	}
}
