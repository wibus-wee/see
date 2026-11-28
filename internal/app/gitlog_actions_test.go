package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"see/internal/git"
)

// Show File History scopes the log to the file's commits; the scope
// chip shows its path and clearing it reads the repository's again.
// settleFrames waits like settle, rendering between the tasks so a
// load that the render itself asks for can start.
func (h *harness) settleFrames(cond func() bool) {
	h.t.Helper()
	guard := time.After(time.Minute)
	for !cond() {
		h.tt.Frame()
		select {
		case fn := <-h.q:
			fn()
		case <-guard:
			h.t.Fatal("condition never held")
		case <-time.After(time.Millisecond):
		}
	}
	h.tt.Frame()
}

func TestFileHistoryScopesTheLog(t *testing.T) {
	h := newHarness(t, historyRepo(t))
	h.readGit()

	h.a.showFileHistory(filepath.Join(h.a.root, "b.go"))
	if h.a.focus.active != gitLogTab {
		t.Fatalf("active %q", h.a.focus.active)
	}
	v := h.a.focus.diffs[gitLogTab].log
	if v.path != "b.go" {
		t.Fatalf("scope %q", v.path)
	}
	// The scope reaches the load through the log's render, so pump
	// frames between the loads.
	h.settleFrames(func() bool {
		return !h.a.git.history.loading && h.a.git.history.path == "b.go" && h.a.git.history.commits != nil
	})
	// A merge that did not touch the file is no commit of its history.
	var subjects []string
	for _, cm := range h.a.git.history.commits {
		subjects = append(subjects, cm.Subject)
	}
	if len(subjects) != 1 || subjects[0] != "Add B on the side" {
		t.Fatalf("b.go's history %q", subjects)
	}
	if !h.tt.HasText("b.go") {
		t.Fatalf("no scope chip in %q", h.tt.Texts())
	}
	// The filter keeps working over the file's commits.
	v.query = "merge"
	h.tt.Frame()
	if h.tt.HasText("Add B on the side") || !h.tt.HasText("No commits match") {
		t.Fatalf("filtered %q", h.tt.Texts())
	}
	v.query = ""

	// Clearing the scope reads the whole history again.
	h.a.openWholeLog()
	h.settleFrames(func() bool {
		return !h.a.git.history.loading && h.a.git.history.path == "" && len(h.a.git.history.commits) >= 4
	})
	if v.path != "" {
		t.Fatalf("scope %q after clearing", v.path)
	}
}

// Jump to Date reveals the newest commit at or before the date typed;
// a date no commit is on or before says so under the field.
func TestJumpToDate(t *testing.T) {
	h := newHarness(t, historyRepo(t))
	h.readGit()
	h.a.openJumpToDate()
	if !h.a.jumpDate.open {
		t.Fatal("the panel did not open")
	}
	today := time.Now().Format(time.DateOnly)
	h.a.jumpDate.query = today
	h.a.jumpLogToDate()
	h.settleFrames(func() bool {
		d := h.a.focus.diffs[gitLogTab]
		return !h.a.jumpDate.loading && d != nil && d.log != nil && d.log.pending == "" && d.log.selected != ""
	})
	if h.a.jumpDate.open {
		t.Fatalf("the panel stayed open: %+v", h.a.jumpDate)
	}

	h.a.openJumpToDate()
	h.a.jumpDate.query = "2000-01-01"
	h.a.jumpLogToDate()
	h.settle(func() bool { return !h.a.jumpDate.loading })
	if !h.a.jumpDate.open || !h.tt.HasText("No commit on or before 2000-01-01") {
		t.Fatalf("no error under the field: %q", h.tt.Texts())
	}
	h.a.jumpDate.query = "garbage"
	h.a.jumpLogToDate()
	h.tt.Frame()
	if !h.tt.HasText("Type a date as 2006-01-02") {
		t.Fatalf("no format error: %q", h.tt.Texts())
	}
}

// Reveal names HEAD and the branches' tips in the log; a name that
// resolves to nothing notes it.
func TestRevealLogRef(t *testing.T) {
	h := newHarness(t, historyRepo(t))
	h.readGit()
	h.a.revealLogRef("side")
	h.settleFrames(func() bool {
		d := h.a.focus.diffs[gitLogTab]
		return d != nil && d.log != nil && d.log.pending == "" && d.log.selected != ""
	})
	d := h.a.focus.diffs[gitLogTab]
	tip := h.a.git.history.commits[1] // "Add B on the side", side's tip
	if tip.Subject != "Add B on the side" || d.log.selected != tip.Hash {
		t.Fatalf("revealed %q, side's tip is %q", d.log.selected, tip.Hash)
	}
	h.a.revealLogRef("no-such")
	h.settle(func() bool { return len(h.feedback) != 0 })
	if len(h.feedback) == 0 {
		t.Fatal("an unknown revision notes it")
	}
}

// changedPaths joins a commit's files, a rename old -> new.
func TestChangedPaths(t *testing.T) {
	files := []git.FileStat{
		{Path: "a.go"},
		{Path: "new.go", OldPath: "old.go"},
	}
	if got := changedPaths(files); got != "a.go\nold.go -> new.go" {
		t.Fatalf("%q", got)
	}
	if got := firstPath("a.go\nb.go"); got != "a.go…" {
		t.Fatalf("%q", got)
	}
}

// compareLink is the host's comparison of a commit with its parent, on
// the hosts whose shape is known; "" elsewhere and for a first commit.
func TestCompareLink(t *testing.T) {
	h := newHarness(t, "")
	cm := git.Commit{Hash: strings.Repeat("a", 40), Parents: []string{strings.Repeat("b", 40)}}
	for host, want := range map[string]bool{
		"https://github.com/o/r":      true,
		"https://gitlab.com/o/r":      true,
		"https://codeberg.org/o/r":    true,
		"https://bitbucket.org/o/r":   false,
		"https://git.example.com/o/r": false,
	} {
		h.a.git.web = host
		link := h.a.compareLink(cm)
		if want != (link == host+"/compare/"+cm.Parents[0]+"..."+cm.Hash) {
			t.Fatalf("%s: %q", host, link)
		}
	}
	h.a.git.web = "https://github.com/o/r"
	if link := h.a.compareLink(git.Commit{Hash: "abc"}); link != "" {
		t.Fatalf("a first commit compares with nothing: %q", link)
	}
}
