package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRecentFilterAppearsPastFive(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = true
	for i := range 4 {
		h.a.cfg.Recent = append(h.a.cfg.Recent, filepath.Join("/work", string(rune('a')+rune(i))))
	}
	h.tt.Frame()
	if h.tt.HasText(welcomeText("welcomeFilterRecent")) {
		t.Fatal("filter shows with four folders")
	}
	h.a.cfg.Recent = append(h.a.cfg.Recent, "/work/e", "/work/f")
	h.tt.Frame()
	if !h.tt.HasText(welcomeText("welcomeFilterRecent")) {
		t.Fatal("filter is missing with six folders")
	}
}

func TestRecentFilterNarrowsTheList(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = true
	h.a.cfg.Recent = []string{"/work/alpha", "/work/beta", "/work/gamma", "/work/delta", "/work/epsilon", "/work/zeta"}
	h.tt.Frame()
	h.a.recentQuery = "alp"
	h.tt.Frame()
	if !h.tt.HasText("alpha") || h.tt.HasText("beta") {
		texts := h.tt.Texts()
		t.Fatalf("filter kept the wrong rows: %v", texts)
	}
	h.a.recentQuery = "nothing-matches"
	h.tt.Frame()
	if !h.tt.HasText(welcomeText("welcomeNoMatchingFolders")) {
		t.Fatal("empty filter result shows no message")
	}
}

func TestRecentContextMenuRemovesRow(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = true
	h.a.cfg.Recent = []string{"/work/alpha", "/work/beta"}
	h.tt.Frame()
	if err := h.tt.RightClick("/work/beta"); err != nil {
		t.Skipf("tester cannot right-click rows: %v", err)
	}
	if err := h.tt.ChooseMenuItem(welcomeText("welcomeRemoveFromRecents")); err != nil {
		t.Fatalf("menu lacks %q: %v", welcomeText("welcomeRemoveFromRecents"), err)
	}
	h.tt.Frame()
	if slices.Contains(h.a.cfg.Recent, "/work/beta") {
		t.Fatalf("Remove from Recents kept the folder: %v", h.a.cfg.Recent)
	}
}

func TestRecentContextMenuRevealsInFileManager(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = true
	h.a.cfg.Recent = []string{"/work/alpha"}
	h.tt.Frame()
	if err := h.tt.RightClick("/work/alpha"); err != nil {
		t.Skipf("tester cannot right-click rows: %v", err)
	}
	want := showInFolderLabel()
	if !slices.Contains(h.tt.Menu(), want) {
		t.Fatalf("context menu lacks %q: %v", want, h.tt.Menu())
	}
}

func TestWelcomeCloneActionOpensDialog(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = true
	h.tt.Frame()
	if !h.tt.HasText(welcomeText("welcomeCloneRepository")) {
		t.Fatal("welcome lacks the clone action")
	}
	if err := h.tt.Click(welcomeText("welcomeCloneRepository")); err != nil {
		t.Fatalf("clone action did not click: %v", err)
	}
	h.tt.Frame()
	for _, text := range []string{welcomeText("welcomeCloneURL"), welcomeText("welcomeCloneLocation"), welcomeText("welcomeCloneButton")} {
		if !h.tt.HasText(text) {
			t.Fatalf("clone dialog is missing %q", text)
		}
	}
}

func TestCloneRepositoryRequiresURL(t *testing.T) {
	h := newHarness(t, "")
	h.a.clone = welcomeClone{open: true}
	h.a.cloneRepository()
	if h.a.clone.err != welcomeText("welcomeCloneEnterURL") || h.a.clone.cloning {
		t.Fatalf("empty URL: err=%q cloning=%v", h.a.clone.err, h.a.clone.cloning)
	}
}

func TestCloneRepositoryClonesAndOpens(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = true
	var gotURL, gotTarget string
	old := gitClone
	gitClone = func(url, target string) error {
		gotURL, gotTarget = url, target
		return os.MkdirAll(target, 0o755)
	}
	t.Cleanup(func() { gitClone = old })
	dir := t.TempDir()
	h.a.clone = welcomeClone{open: true, url: "https://github.com/o/r.git", dir: dir}
	h.a.cloneRepository()
	target := filepath.Join(dir, "r")
	h.settle(func() bool { return !h.a.clone.cloning && h.a.root == target })
	if gotURL != "https://github.com/o/r.git" || gotTarget != target {
		t.Fatalf("clone called with %q -> %q", gotURL, gotTarget)
	}
	if h.a.clone.open || h.a.clone.err != "" {
		t.Fatalf("dialog did not close cleanly: %+v", h.a.clone)
	}
	if h.a.showWelcome {
		t.Fatal("welcome stayed up over the cloned workspace")
	}
}

func TestCloneRepositoryReportsFailure(t *testing.T) {
	h := newHarness(t, "")
	old := gitClone
	gitClone = func(url, target string) error { return errors.New("fatal: repository not found") }
	t.Cleanup(func() { gitClone = old })
	h.a.clone = welcomeClone{open: true, url: "https://github.com/o/r.git", dir: "/work"}
	h.a.cloneRepository()
	h.settle(func() bool { return !h.a.clone.cloning })
	if !h.a.clone.open || h.a.clone.err == "" {
		t.Fatalf("failure closed or cleared the dialog: %+v", h.a.clone)
	}
	h.tt.Frame()
	if !h.tt.HasText("repository not found") {
		t.Fatalf("clone error is not shown: %q", h.a.clone.err)
	}
}

func TestCloneRepoName(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/owner/repo.git": "repo",
		"git@github.com:owner/repo.git":     "repo",
		"https://github.com/owner/repo/":    "repo",
		"ssh://git@host/owner/repo":         "repo",
	} {
		if got := cloneRepoName(url); got != want {
			t.Errorf("cloneRepoName(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestExpandTilde(t *testing.T) {
	home := t.TempDir()
	// os.UserHomeDir reads HOME on Unix, USERPROFILE on Windows.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if got := expandTilde("~/code"); got != filepath.Join(home, "code") {
		t.Fatalf("expandTilde(~/code) = %q", got)
	}
	if got := expandTilde("/abs/path"); got != "/abs/path" {
		t.Fatalf("expandTilde kept an absolute path as %q", got)
	}
}
