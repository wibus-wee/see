package app

// Renders the states the confusing-UI review flagged, as PNGs under
// SEE_MOCK_OUT. Run with:
//   SEE_MOCK_OUT=/tmp/seemock go test ./internal/app -run TestMockRender -v

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mockOut(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("SEE_MOCK_OUT")
	if dir == "" {
		t.Skip("SEE_MOCK_OUT unset")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func mockSave(t *testing.T, h *harness, name string) {
	t.Helper()
	img := h.tt.Image()
	f, err := os.Create(filepath.Join(mockOut(t), name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// The untrusted workspace: trust banner over the editor, inspector's
// trust state behind its tab strip. The folder carries the UUID name a
// worktree checkout has, which the screenshot review used.
func TestMockRenderUntrusted(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "d14f0dcb-e737-40fe-9a64-7141f6f668db")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# See\n\nA editor for reading code.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\nfunc call() { helper() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	d := h.open("a.go")
	h.settle(func() bool { return !d.loading })
	h.a.trustAsk = true // as the LSP's first request sets it
	h.a.cfg.QuickHelpInspector = true
	h.tt.Frame()
	mockSave(t, h, "untrusted.png")
}

// The trusted workspace with the caret on a documented symbol: the
// inspector shows its header badge and the sections.
func TestMockRenderInspector(t *testing.T) {
	h, d, offset := quickHelpHarness(t)
	h.a.cfg.QuickHelpInspector = true
	st := h.a.focus.eds[d.path]
	st.GoTo(st.Buffer.Pos(offset))
	h.tt.Frame()
	deadline := time.After(5 * time.Second)
	for !h.tt.HasText("Returns the result.") {
		h.tt.Frame()
		select {
		case fn := <-h.q:
			fn()
		case <-time.After(5 * time.Millisecond):
		case <-deadline:
			t.Fatalf("no documentation: %q", strings.Join(h.tt.Texts(), "|"))
		}
	}
	h.tt.Frame()
	mockSave(t, h, "inspector.png")
}

// The inspector with no symbol under the caret: the header without the
// noise badge, and the empty state's explanation.
func TestMockRenderInspectorEmpty(t *testing.T) {
	h, d, _ := quickHelpHarness(t)
	h.a.cfg.QuickHelpInspector = true
	st := h.a.focus.eds[d.path]
	st.GoTo(st.Buffer.Pos(0)) // on "package", no symbol
	h.tt.Frame()
	h.tt.Frame()
	mockSave(t, h, "inspector-empty.png")
}

// The Git log of a branch with commits to push and to pull, a release
// tag at one, as the screenshot review annotated: the count of the
// whole walk over the rows, the sync arrows on the mainline's commits
// alone, and the navigator's history at their left.
func TestMockRenderGitLog(t *testing.T) {
	root := historyRepo(t)
	// A shared upstream main tracks: the release commit is pushed, the
	// two after it are not, and one of upstream's is not pulled.
	gitRun(t, root, "commit", "-qam", "chore: release Nightly [risk:high]")
	gitRun(t, root, "tag", "nightly-0.114.0-nightly.0")
	gitRun(t, root, "branch", "origin-main")
	writeFile(t, root, "local1.go", "package a\n\nfunc Local1() {}\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "fix: harden E2EE pairing and attachment grants [risk:high] (#4450)")
	writeFile(t, root, "local2.go", "package a\n\nfunc Local2() {}\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "feat: include workspace paid seats in Streams JWT scopes [risk:medium] (#4447)")
	// origin/main gains a commit main does not have, through a detour.
	gitRun(t, root, "switch", "-qc", "upstream-work", "origin-main")
	writeFile(t, root, "remote1.go", "package a\n\nfunc Remote1() {}\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "fix: make the verified E2EE ledger Owner the single Owner (#4453)")
	gitRun(t, root, "branch", "-f", "origin-main", "upstream-work")
	gitRun(t, root, "switch", "-q", "main")
	gitRun(t, root, "branch", "-D", "upstream-work")
	gitRun(t, root, "branch", "--set-upstream-to=origin-main", "main")
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator = navGit
	h.a.openGitLog()
	h.settle(func() bool { return !h.a.git.history.loading })
	v := h.a.focus.diffs[gitLogTab].log
	h.settle(func() bool { return v.detail != nil && !v.detail.loading })
	h.tt.Frame()
	h.tt.Frame()
	mockSave(t, h, "gitlog.png")
}
