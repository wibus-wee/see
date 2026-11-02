package git

import (
	"context"
	"testing"
)

func TestWebURL(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:wibus-wee/see.git":       "https://github.com/wibus-wee/see",
		"https://github.com/wibus-wee/see.git\n": "https://github.com/wibus-wee/see",
		"ssh://git@gitlab.com/group/sub/repo":    "https://gitlab.com/group/sub/repo",
		"https://user@github.com/o/r":            "https://github.com/o/r",
		"/srv/git/repo.git":                      "",
		"file:///srv/git/repo.git":               "",
	} {
		if got := WebURL(in); got != want {
			t.Errorf("WebURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTracking(t *testing.T) {
	root, run := newRepo(t)
	ctx := context.Background()
	write(t, root, "a", "a\n")
	run("add", "a")
	run("commit", "-qm", "first")
	if up, _, _ := Tracking(ctx, root); up != "" {
		t.Fatalf("upstream without one: %q", up)
	}
	run("remote", "add", "origin", "https://example.com/r.git")
	run("update-ref", "refs/remotes/origin/main", "HEAD")
	run("branch", "--set-upstream-to=origin/main")
	run("commit", "-qm", "second", "--allow-empty")
	up, ahead, behind := Tracking(ctx, root)
	if up != "origin/main" || ahead != 1 || behind != 0 {
		t.Fatalf("tracking %q ↑%d ↓%d", up, ahead, behind)
	}
}
