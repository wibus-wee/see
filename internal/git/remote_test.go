package git

import (
	"context"
	"errors"
	"testing"
)

func TestReachable(t *testing.T) {
	unreachable := []string{
		// The hosts' own words for a network that is not there, as curl
		// and OpenSSH phrase them under git's fatal:.
		"fatal: unable to access 'https://x/': Could not resolve host: github.com",
		"fatal: unable to access 'https://x/': Failed to connect to 127.0.0.1 port 1 after 0 ms: Couldn't connect to server",
		"fatal: unable to access 'https://x/': LibreSSL SSL_connect: SSL_ERROR_SYSCALL in connection to host:443",
		"fatal: unable to access 'https://x/': connection timed out after 30000 milliseconds",
		"ssh: connect to host github.com port 22: Network is unreachable",
		"ssh: connect to host github.com port 22: No route to host",
		"ssh: connect to host github.com port 22: Operation timed out",
		"fatal: unable to access 'https://x/': connection refused",
		"ssh: Could not resolve hostname github.com: nodename nor servname provided, or not known",
	}
	for _, s := range unreachable {
		if Reachable(errors.New(s)) {
			t.Errorf("Reachable(%q) = true, want false", s)
		}
	}
	reachable := []string{
		// The remote answered, even to refuse: the way to it is there.
		"fatal: repository 'https://github.com/o/r.git/' not found",
		"fatal: Authentication failed for 'https://github.com/o/r.git/'",
		"git@github.com: Permission denied (publickey).",
		"fatal: refusing to merge unrelated histories",
		"error: corrupt loose object",
	}
	for _, s := range reachable {
		if !Reachable(errors.New(s)) {
			t.Errorf("Reachable(%q) = false, want true", s)
		}
	}
	if !Reachable(nil) {
		t.Error("Reachable(nil) = false, want true")
	}
}

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
