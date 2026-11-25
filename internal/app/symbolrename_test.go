package app

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/internal/uri"
)

// renameHarness opens a Go file whose fake server renames.
func renameHarness(t *testing.T) (*harness, *doc, *editor.State) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"main.go":  "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tx := 1\n\tfmt.Println(x)\n}\n",
		"other.go": "package main\n\nvar x = 2\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	d := h.open("main.go")
	return h, d, h.a.activeEditor()
}

func TestRenameSymbolAppliesOpenAndClosedFiles(t *testing.T) {
	h, d, st := renameHarness(t)
	mainURI := d.uri
	otherURI := uri.FromPath(h.a.root + "/other.go")

	h.fc.prepareRename = func(ctx context.Context, u string, pos lsp.Position) (*lsp.PrepareRenameResult, error) {
		return &lsp.PrepareRenameResult{
			Range:       lsp.Range{Start: lsp.Position{Line: 5, Character: 1}, End: lsp.Position{Line: 5, Character: 2}},
			Placeholder: "x",
		}, nil
	}
	var gotName string
	h.fc.rename = func(ctx context.Context, u string, pos lsp.Position, newName string) (*lsp.WorkspaceEdit, error) {
		gotName = newName
		return &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
			mainURI: {
				{Range: lsp.Range{Start: lsp.Position{Line: 5, Character: 1}, End: lsp.Position{Line: 5, Character: 2}}, NewText: newName},
				{Range: lsp.Range{Start: lsp.Position{Line: 6, Character: 13}, End: lsp.Position{Line: 6, Character: 14}}, NewText: newName},
			},
			otherURI: {
				{Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 4}, End: lsp.Position{Line: 2, Character: 5}}, NewText: newName},
			},
		}}, nil
	}

	st.GoTo(editor.Pos{Line: 5, Col: 1})
	h.tt.Frame()
	a := h.a
	a.renameSymbol()
	h.settle(func() bool { return a.rename != nil })
	if a.rename.name != "x" {
		t.Fatalf("seed: %q", a.rename.name)
	}
	a.rename.name = "y"
	a.submitSymbolRename(a.rename)
	// The rename request is async: wait until the edit is applied and
	// the prompt is gone.
	h.settle(func() bool { return a.rename == nil && d.buf.Snapshot().Line(5) == "\ty := 1" })
	// The open buffer holds the rename, and the unopened file changed on
	// disk.
	if got := d.buf.Snapshot().Line(5); got != "\ty := 1" {
		t.Fatalf("main.go line 5: %q", got)
	}
	if got := d.buf.Snapshot().Line(6); got != "\tfmt.Println(y)" {
		t.Fatalf("main.go line 6: %q", got)
	}
	got, err := readFileString(h.a.root + "/other.go")
	if err != nil {
		t.Fatal(err)
	}
	if got != "package main\n\nvar y = 2\n" {
		t.Fatalf("other.go: %q", got)
	}
	if gotName != "y" {
		t.Fatalf("newName: %q", gotName)
	}
	// The open doc told the server it changed.
	h.settle(func() bool {
		for _, s := range h.fc.calls() {
			if s == "change main.go" {
				return true
			}
		}
		return false
	})
}

func TestRenameSymbolNoProvider(t *testing.T) {
	h, _, st := renameHarness(t)
	h.fc.noRename = true
	st.GoTo(editor.Pos{Line: 5, Col: 1})
	h.tt.Frame()
	a := h.a
	a.renameSymbol()
	if a.rename != nil {
		t.Fatal("rename should not have started")
	}
}

func TestRenameSymbolPrepareRejects(t *testing.T) {
	h, _, st := renameHarness(t)
	h.fc.prepareRename = func(ctx context.Context, u string, pos lsp.Position) (*lsp.PrepareRenameResult, error) {
		return nil, lsp.ErrNoRename
	}
	h.fc.rename = func(ctx context.Context, u string, pos lsp.Position, newName string) (*lsp.WorkspaceEdit, error) {
		t.Fatal("rename should not be called")
		return nil, nil
	}
	st.GoTo(editor.Pos{Line: 0, Col: 0})
	h.tt.Frame()
	a := h.a
	a.renameSymbol()
	h.settle(func() bool { return a.renameCancel == nil && !a.finding })
	if a.rename != nil {
		t.Fatal("rename should not have opened")
	}
}

func TestRenameCommandEnabled(t *testing.T) {
	h, _, _ := renameHarness(t)
	h.fc.rename = func(ctx context.Context, u string, pos lsp.Position, n string) (*lsp.WorkspaceEdit, error) {
		return &lsp.WorkspaceEdit{}, nil
	}
	if !h.a.commandEnabled("symbol.rename") {
		t.Fatal("rename should be enabled")
	}
	h.fc.noRename = true
	if h.a.commandEnabled("symbol.rename") {
		t.Fatal("rename should be disabled without the provider")
	}
}
