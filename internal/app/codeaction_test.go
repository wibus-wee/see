package app

import (
	"encoding/json"

	"os"
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/internal/uri"
)

// codeActionHarness opens a Go file whose fake server offers actions.
func codeActionHarness(t *testing.T, actions []lsp.CodeAction) (*harness, *doc, *editor.State) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tx := 1\n\tfmt.Println(x)\n}\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.fc.codeActions = actions
	d := h.open("main.go")
	return h, d, h.a.activeEditor()
}

func TestCodeActionGroupsOrder(t *testing.T) {
	actions := []lsp.CodeAction{
		{Title: "Generate tests", Kind: "source.generate"},
		{Title: "Rename x", Kind: "refactor.rename"},
		{Title: "Fix import", Kind: "quickfix"},
		{Title: "Fix typo", Kind: "quickfix", IsPreferred: true},
		{Title: "Other", Kind: ""},
	}
	groups := codeActionGroups(actions)
	if len(groups) != 4 {
		t.Fatalf("groups: %d", len(groups))
	}
	// Quick fixes first, the preferred one leading; refactorings next;
	// source actions; what no kind matches last, in the server's order.
	if groups[0][0].Title != "Fix typo" || groups[0][1].Title != "Fix import" {
		t.Fatalf("quickfix group: %+v", groups[0])
	}
	if groups[1][0].Title != "Rename x" {
		t.Fatalf("refactor group: %+v", groups[1])
	}
	if groups[2][0].Title != "Generate tests" {
		t.Fatalf("source group: %+v", groups[2])
	}
	if groups[3][0].Title != "Other" {
		t.Fatalf("rest group: %+v", groups[3])
	}
}

func TestCodeActionsRequestAndApply(t *testing.T) {
	fix := lsp.CodeAction{
		Title: "Fix it",
		Kind:  "quickfix",
		Edit: &lsp.WorkspaceEdit{
			Changes: map[string][]lsp.TextEdit{},
		},
	}
	h, d, st := codeActionHarness(t, []lsp.CodeAction{fix})
	// The fake server learns the document's URI when it opens, so the
	// edit can name it after.
	docURI := d.uri
	fix.Edit.Changes[docURI] = []lsp.TextEdit{
		{Range: lsp.Range{Start: lsp.Position{Line: 5, Character: 1}, End: lsp.Position{Line: 5, Character: 2}}, NewText: "y"},
	}
	st.GoTo(editor.Pos{Line: 5, Col: 1})
	h.tt.Frame()
	a := h.a
	a.codeActionAt(a.focus, d, st)
	h.settle(func() bool { return a.codeActions != nil })
	if len(a.codeActions.list) != 1 || a.codeActions.list[0].Title != "Fix it" {
		t.Fatalf("actions: %+v", a.codeActions.list)
	}
	a.runCodeAction(a.codeActions, a.codeActions.list[0])
	// The open buffer holds the edit, folded into one undo step, and the
	// server heard the change.
	if got := d.buf.Snapshot().Line(5); got != "\ty := 1" {
		t.Fatalf("line 5: %q", got)
	}
	h.settle(func() bool {
		for _, s := range h.fc.calls() {
			if s == "change main.go" {
				return true
			}
		}
		return false
	})
}

func TestCodeActionsExecuteCommand(t *testing.T) {
	cmd := lsp.Command{Title: "Run it", Command: "gopls.test"}
	h, d, st := codeActionHarness(t, []lsp.CodeAction{{Title: "Run it", Kind: "quickfix", Command: &cmd}})
	st.GoTo(editor.Pos{Line: 4, Col: 1})
	h.tt.Frame()
	a := h.a
	a.codeActionAt(a.focus, d, st)
	h.settle(func() bool { return a.codeActions != nil })
	a.runCodeAction(a.codeActions, a.codeActions.list[0])
	h.settle(func() bool {
		for _, s := range h.fc.calls() {
			if s == "executeCommand gopls.test" {
				return true
			}
		}
		return false
	})
	if h.fc.lastCommand.Command != "gopls.test" {
		t.Fatalf("command: %+v", h.fc.lastCommand)
	}
}

func TestCodeActionsClosedFileEdit(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go":    "package main\n\nvar x = 1\n",
		"b.go":    "package main\n\nvar y = 1\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	_ = h.open("main.go")
	// An edit that names b.go, which no tab holds, goes to the file on
	// disk: the next open reads the new text.
	bURI := uri.FromPath(h.a.root + "/b.go")
	edit := &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		bURI: {{Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 4}, End: lsp.Position{Line: 2, Character: 5}}, NewText: "z"}},
	}}
	if err := h.a.applyWorkspaceEdit(edit, h.a.lsp); err != nil {
		t.Fatal(err)
	}
	got, err := readFileString(h.a.root + "/b.go")
	if err != nil {
		t.Fatal(err)
	}
	if got != "package main\n\nvar z = 1\n" {
		t.Fatalf("b.go: %q", got)
	}
	// a.go is untouched.
	got, _ = readFileString(h.a.root + "/a.go")
	if got != "package main\n\nvar x = 1\n" {
		t.Fatalf("a.go: %q", got)
	}
}

func readFileString(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

// A compound action must not edit a file and only then discover that its second step is unsupported.
func TestCodeActionUnsupportedStepPreservesFile(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	h := newHarness(t, root)
	path := h.a.root + "/a.go"
	edit := &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{uri.FromPath(path): {{NewText: "modified"}}}}
	h.fc.noCommands = true
	h.a.applyCodeAction(&codeActions{client: h.fc, server: h.a.lsp}, &lsp.CodeAction{Edit: edit, Command: &lsp.Command{Command: "missing"}})
	if text, err := readFileString(path); err != nil || text != "package a\n" {
		t.Fatalf("unsupported command changed file: %q %v", text, err)
	}
	edit.DocumentChanges = []json.RawMessage{json.RawMessage(`{"kind":"delete","uri":"file:///elsewhere"}`)}
	if err := h.a.applyWorkspaceEdit(edit, h.a.lsp); err == nil {
		t.Fatal("accepted unsupported delete")
	}
	if text, err := readFileString(path); err != nil || text != "package a\n" {
		t.Fatalf("unsupported delete changed file: %q %v", text, err)
	}
	edit.DocumentChanges = []json.RawMessage{json.RawMessage(`{"textDocument":{"uri":"file:///not-open","version":0},"edits":[]}`)}
	if err := h.a.applyWorkspaceEdit(edit, h.a.lsp); err == nil {
		t.Fatal("accepted unverifiable version zero")
	}
	if text, err := readFileString(path); err != nil || text != "package a\n" {
		t.Fatalf("version conflict changed file: %q %v", text, err)
	}

}
