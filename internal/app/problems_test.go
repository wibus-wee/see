package app

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestProblemsNavigatorFiltersAndNavigates(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "first\nsecond\nthird", "b.txt": "one\ntwo\nthree"}))
	a := h.open("a.txt")
	b := h.open("b.txt")
	a.diagnosticVersion = a.buf.Version()
	a.diagnostics = []editor.Diagnostic{{Start: 13, End: 15, Message: "Later error", Source: "checker"}, {Start: 6, End: 8, Severity: editor.SeverityWarning, Message: "Earlier warning", Source: "lint"}}
	b.diagnosticVersion = b.buf.Version()
	b.diagnostics = []editor.Diagnostic{{Start: 4, End: 7, Message: "Other file error", Source: "checker"}}
	origin := h.a.activeEditor()
	origin.Select(1, 1)
	h.tt.Frame()
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	h.tt.Frame()
	if h.a.navigator != navProblems || h.a.sidebarOff {
		t.Fatal("shortcut did not show Problems")
	}
	for _, text := range []string{"Earlier warning", "Later error", "Other file error", "a.txt:2:1", "lint"} {
		if !h.tt.HasText(text) {
			t.Fatalf("missing %q in %q", text, h.tt.Texts())
		}
	}
	rows := h.a.problemLocations()
	if len(rows) != 3 || rows[0].path != a.path || rows[0].diagnostic.Message != "Earlier warning" {
		t.Fatalf("source ordering: %+v", rows)
	}
	// Export only when explicitly requested by the verification runner.
	if output := os.Getenv("SEE_PROBLEMS_SCREENSHOT"); output != "" {
		f, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(f, h.tt.Image()); err != nil {
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.tt.Click("Filter Problems"); err != nil {
		t.Fatal(err)
	}
	h.tt.Type("earlier")
	h.tt.Frame()
	if h.tt.HasText("Later error") || !h.tt.HasText("Earlier warning") {
		t.Fatalf("message filtering: %q", h.tt.Texts())
	}
	label := "Warning: Earlier warning — a.txt:2:1"
	if err := h.tt.Click(label); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.a.activePath() != a.path || h.a.activeEditor().Cursor() != 6 {
		t.Fatal("problem did not navigate")
	}
	h.a.goBack()
	if h.a.activePath() != b.path || h.a.activeEditor().Cursor() != 1 {
		t.Fatal("Go Back did not restore origin")
	}
	h.a.problems.query = "CHECKER"
	h.tt.Frame()
	if len(h.a.problemLocations()) != 2 {
		t.Fatal("source filtering failed")
	}
	h.a.problems.query = "b.txt"
	if got := h.a.problemLocations(); len(got) != 1 || got[0].path != b.path {
		t.Fatal("path filtering failed")
	}
	h.a.problems.query = "no such problem"
	h.tt.Frame()
	if !h.tt.HasText("No matching problems") {
		t.Fatal("missing filtered empty state")
	}
	h.a.problems.query = ""
	old := rows[0]
	current := rows[2]
	b.diagnostics = nil
	h.a.openProblem(current)
	if h.a.activePath() != b.path || h.a.activeEditor().Cursor() != 1 {
		t.Fatal("withdrawn diagnostic navigated")
	}
	b.diagnostics = []editor.Diagnostic{current.diagnostic}
	a.buf.Set("new text")
	h.a.openProblem(old)
	if h.a.activePath() != b.path {
		t.Fatal("stale problem navigated")
	}
	h.tt.Frame()
	if h.tt.HasText("Earlier warning") || h.tt.HasText("Later error") {
		t.Fatal("stale problems stayed visible")
	}
	b.diagnostics = nil
	h.tt.Frame()
	if !h.tt.HasText("No problems in open files") {
		t.Fatal("missing empty state")
	}
}

func TestProblemsOutsideWorkspaceAndClosedDocuments(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "hello"}))
	d := h.open("a.txt")
	d.diagnosticVersion = d.buf.Version()
	d.diagnostics = []editor.Diagnostic{{Start: 0, Message: "Visible"}}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	h.a.userOpen(outside)
	other := h.a.docs[outside]
	h.settle(func() bool { return !other.loading })
	other.diagnosticVersion = other.buf.Version()
	other.diagnostics = []editor.Diagnostic{{Message: "Outside"}}
	if got := h.a.problemLocations(); len(got) != 1 || strings.Contains(got[0].diagnostic.Message, "Outside") {
		t.Fatal("included non-workspace diagnostics")
	}
	h.a.closeDoc(d.path)
	if got := h.a.problemLocations(); len(got) != 0 {
		t.Fatal("included closed document")
	}
}
