package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestGitLogTableColumnsFilterAndNavigation(t *testing.T) {
	root := historyRepo(t)
	gitRun(t, root, "tag", "release-preview")
	h := newHarness(t, root)
	h.a.sidebarOff = true
	h.readGit()
	h.a.openGitLog()
	h.settle(func() bool { return !h.a.git.history.loading })
	v := h.a.focus.diffs[gitLogTab].log
	h.settle(func() bool { return v.detail != nil && !v.detail.loading })
	h.tt.Advance(time.Second)
	for _, label := range []string{"Branch / Tag", "Graph", "Commit Message", "Date", "Author"} {
		if !h.tt.HasText(label) {
			t.Fatalf("missing table header %q: %v", label, h.tt.Texts())
		}
	}
	head := h.a.git.history.commits[0]
	graph, ok := h.tt.Find("Graph " + head.Short)
	header, headerOK := h.tt.Find("Graph")
	if !ok || !headerOK || graph.X != header.X || graph.W != header.W {
		t.Fatalf("graph/header alignment: %v / %v", graph, header)
	}
	if !h.tt.HasText("release-preview") {
		t.Fatal("missing tag column")
	}
	if output := os.Getenv("SEE_LOG_PREVIEW_DIR"); output != "" {
		saveLogPreview(t, h, filepath.Join(output, "git-log-light.png"))
		h.tt.SetDark(true)
		h.tt.Frame()
		saveLogPreview(t, h, filepath.Join(output, "git-log-dark.png"))
	}
	if err := h.tt.Click("Filter Commits"); err != nil {
		t.Fatal(err)
	}
	h.tt.Type("side")
	h.tt.Frame()
	if !h.tt.HasText(logText("gitLogFilteredGraph")) || h.tt.HasText("Graph "+head.Short) {
		t.Fatal("filtered list still presents complete topology")
	}
	if err := h.tt.Click("Add B on the side"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool {
		return v.detail != nil && !v.detail.loading && v.detail.commit.Subject == "Add B on the side"
	})
	if len(v.detail.files) != 1 || v.detail.files[0].path != "b.go" || v.detail.files[0].diff == nil {
		t.Fatal("selection did not preserve commit diff")
	}
	// Clear through the model because the click moved keyboard focus to the row.
	v.query = ""
	h.tt.Frame()
	if h.tt.HasText(logText("gitLogFilteredGraph")) || !h.tt.HasText("Graph "+head.Short) {
		t.Fatal("clearing filter did not restore graph")
	}
	h.tt = ui.NewTester(h.a.view, 600, 700)
	h.tt.Frame()
	h.tt.Frame()
	for _, label := range []string{"Commit Message", "Author", "Date", "Hash"} {
		if !h.tt.HasText(label) {
			t.Fatalf("narrow columns hide %q: %v", label, h.tt.Texts())
		}
	}
	if output := os.Getenv("SEE_LOG_PREVIEW_DIR"); output != "" {
		saveLogPreview(t, h, filepath.Join(output, "git-log-narrow.png"))
	}
}

func saveLogPreview(t *testing.T, h *harness, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, h.tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestGitLogRefsColumnFitsLongBranchNames(t *testing.T) {
	root := historyRepo(t)
	const name = "feat/commit-detail-shared-preview"
	gitRun(t, root, "branch", name, "HEAD~2")
	h := newHarness(t, root)
	h.a.sidebarOff = true
	h.readGit()
	h.a.openGitLog()
	h.settle(func() bool { return !h.a.git.history.loading })
	h.tt.Frame()
	h.tt.Frame()
	badge, ok := h.tt.Find(name)
	if !ok {
		t.Fatalf("the long branch is not shown: %v", h.tt.Texts())
	}
	// The column's old fixed width cut this name; it now takes the room
	// the badge needs.
	if badge.W <= 150 {
		t.Fatalf("long branch badge is %v wide: %+v", badge.W, badge)
	}
}

func TestGitLogAuthorAndHashColumnsFitTheirText(t *testing.T) {
	root := historyRepo(t)
	const author = "Alexandria Cassandra Longname"
	const coAuthor = "Beatrice Coplayer"
	gitRun(t, root, "commit", "--allow-empty", "--author", author+" <alex@example.com>",
		"-qm", "A long author", "-m", "Co-authored-by: "+coAuthor+" <bea@example.com>")
	h := newHarness(t, root)
	h.a.sidebarOff = true
	h.readGit()
	h.a.openGitLog()
	h.settle(func() bool { return !h.a.git.history.loading })
	h.tt.Frame()
	h.tt.Frame()
	byline := authors(h.a.git.history.commits[0])
	text, ok := h.tt.Find(byline)
	if !ok {
		t.Fatalf("the long author is not shown: %v", h.tt.Texts())
	}
	// The author column's old fixed width cut long names.
	if text.W <= 150 {
		t.Fatalf("author column is %v wide: %+v", text.W, text)
	}
	hash, ok := h.tt.Find(h.a.git.history.commits[0].Short)
	if !ok {
		t.Fatalf("the short hash is not shown: %v", h.tt.Texts())
	}
	// The hash column takes the room of the short hash, not its old
	// fixed width.
	if hash.W >= 60 {
		t.Fatalf("hash column is %v wide: %+v", hash.W, hash)
	}
}

func TestGitLogDateColumnFitsTheWidestStamp(t *testing.T) {
	root := historyRepo(t)
	// A stamp of the widest digits is wider than the time the test runs
	// at may be, and the column must hold both without changing size.
	gitRun(t, root, "commit", "--allow-empty", "--date", "2026-11-27 08:08:08", "-qm", "A wide stamp")
	h := newHarness(t, root)
	h.a.sidebarOff = true
	h.readGit()
	h.a.openGitLog()
	h.settle(func() bool { return !h.a.git.history.loading })
	h.tt.Frame()
	h.tt.Frame()
	want := h.a.git.history.commits[0].Date.Format("2006-01-02 15:04:05")
	stamp, ok := h.tt.Find(want)
	if !ok {
		t.Fatalf("the date %q is not shown: %v", want, h.tt.Texts())
	}
	if stamp.W < 120 {
		t.Fatalf("date stamp is %v wide: %+v", stamp.W, stamp)
	}
}

func TestGitLogFilteredHistoryCanLoadMore(t *testing.T) {
	h := newHarness(t, historyRepo(t))
	h.readGit()
	h.a.openGitLog()
	h.settle(func() bool { return !h.a.git.history.loading })
	v := h.a.focus.diffs[gitLogTab].log
	h.settle(func() bool { return v.detail != nil && !v.detail.loading })
	// Simulate a page ending before the only matching commit.
	h.a.git.history.commits = h.a.git.history.commits[:1]
	h.a.git.history.done = false
	v.query = "first"
	h.tt.Frame()
	if err := h.tt.Click(logText("gitLogLoadMore")); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.history.loading && h.a.git.history.done })
	if !h.tt.HasText("first") {
		t.Fatal("matching older commit never became reachable")
	}
}

func TestGitLogDenseGraphScrollsWithoutCollapsingColumns(t *testing.T) {
	h := newHarness(t, historyRepo(t))
	h.a.sidebarOff = true
	h.readGit()
	h.a.openGitLog()
	h.settle(func() bool { return !h.a.git.history.loading })
	v := h.a.focus.diffs[gitLogTab].log
	h.settle(func() bool { return v.detail != nil && !v.detail.loading })
	h.a.git.history.lanesW = 50
	h.tt = ui.NewTester(h.a.view, 600, 700)
	h.tt.Frame()
	h.tt.Frame()
	table, ok := h.tt.Find("Commit table")
	if !ok {
		t.Fatal("missing scrollable commit table")
	}
	h.tt.Scroll(table.X+100, table.Y+15, 1200, 0)
	h.tt.Frame()
	date, ok := h.tt.Find("Date")
	if !ok || date.W < 80 || date.X < 0 || date.X+date.W > 600 {
		t.Fatalf("date not reachable by horizontal scroll: %v", date)
	}
	message, ok := h.tt.Find("Commit Message")
	if !ok || message.W < 140 {
		t.Fatalf("message column collapsed: %v", message)
	}
}
