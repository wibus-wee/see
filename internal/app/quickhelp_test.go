package app

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/lsp"
	"see/internal/uri"
)

func TestSplitQuickHelp(t *testing.T) {
	got := splitQuickHelp("```go\nfunc helper() int\n```\n\nReturns the result.\n\nUse it when needed.")
	if got.declaration != "```go\nfunc helper() int\n```" || got.summary != "Returns the result." || got.discussion != "Use it when needed." {
		t.Fatalf("sections = %+v", got)
	}
	got = splitQuickHelp("~~~rust\nfn helper()\n~~~\n\nOnly a summary.")
	if got.declaration != "~~~rust\nfn helper()\n~~~" || got.summary != "Only a summary." || got.discussion != "" {
		t.Fatalf("single paragraph = %+v", got)
	}
	if got = splitQuickHelp(" \n\t"); got != (quickHelpSections{}) {
		t.Fatalf("empty markdown = %+v", got)
	}
	got = splitQuickHelp("Plain text without a declaration.")
	if got.declaration != "" || got.summary != "Plain text without a declaration." || got.discussion != "" {
		t.Fatalf("plain markdown = %+v", got)
	}
}

func TestQuickHelpShortcutDoesNotConflict(t *testing.T) {
	var quick command
	for _, cmd := range commands() {
		if cmd.name == "Show Quick Help" {
			quick = cmd
		}
		if cmd.shortcutKey == 0 {
			continue
		}
		for _, other := range commands() {
			if cmd.name != other.name && cmd.shortcutMods == other.shortcutMods && cmd.shortcutKey == other.shortcutKey {
				t.Fatalf("%s and %s share a shortcut", cmd.name, other.name)
			}
		}
	}
	if quick.shortcutMods != ui.Alt|ui.Cmd || quick.shortcutKey != ui.KeyK {
		t.Fatalf("Quick Help shortcut = %v %v", quick.shortcutMods, quick.shortcutKey)
	}
	if commandLabel(quick.name) != "Show Quick Help" {
		t.Fatalf("command palette label = %q", commandLabel(quick.name))
	}
}

func TestQuickHelpAltClick(t *testing.T) {
	h, d, offset := quickHelpHarness(t)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offset + 2))
	h.tt.Frame()
	caret, caretOK := h.tt.TextCaret()
	if !caretOK {
		t.Fatal("editor caret is not available")
	}
	h.tt.ClickAtWith(ui.Alt, caret.X, caret.Y+caret.H/2)
	waitForQuickHelp(t, h, st, "Declaration")
	if !h.tt.HasText("Returns the result.") || !h.tt.HasText("Use it when needed.") || !h.tt.HasText("b.go:2") {
		t.Fatalf("quick help sections missing: %q", h.tt.Texts())
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.tt.HasText("Declaration") {
		t.Fatal("Escape did not dismiss Quick Help")
	}
}

func TestQuickHelpCommandPaletteAndDismissal(t *testing.T) {
	h, d, offset := quickHelpHarness(t)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offset + 2))
	h.tt.Frame()
	caret, ok := h.tt.TextCaret()
	if !ok {
		t.Fatal("editor caret is not available")
	}
	h.a.openQuickPanel(">")
	h.a.quick.query = ">Show Quick Help"
	h.a.rankQuick()
	h.tt.Frame()
	h.tt.Key(0, ui.KeyEnter)
	waitForQuickHelp(t, h, st, "Declaration")
	if os.Getenv("SEE_QUICKHELP_SCREENSHOTS") == "1" {
		writeQuickHelpScreenshots(t, h)
	}
	h.tt.ClickAt(12, 12)
	h.tt.Frame()
	if h.tt.HasText("Declaration") {
		t.Fatal("clicking elsewhere did not dismiss Quick Help")
	}

	st.GoTo(d.buf.Pos(offset + 2))
	h.tt.ClickAt(caret.X, caret.Y+caret.H/2)
	h.tt.Frame()
	quickHelpShortcut := jumpShortcut(t, "Show Quick Help")
	h.tt.Key(quickHelpShortcut.shortcutMods, quickHelpShortcut.shortcutKey)
	waitForQuickHelp(t, h, st, "Declaration")
	before := d.buf.String()
	h.tt.TypeKey(0, ui.KeyX, "x")
	h.tt.Frame()
	if d.buf.String() == before {
		t.Fatal("typing did not edit the focused document")
	}
	if h.tt.HasText("Declaration") {
		active, noInputMethod := h.tt.TextInputActive()
		t.Fatalf("editing did not dismiss Quick Help: marker=%p focused=%v textInput=%v/%v buffer=%q", h.a.quickHelp, st.Focused(), active, noInputMethod, d.buf.String())
	}
}

func TestQuickHelpDeclaredInJumpsToDefinition(t *testing.T) {
	h, d, offset := quickHelpHarness(t)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offset + 2))
	h.tt.Frame()
	h.a.showQuickHelpAtCaret()
	waitForQuickHelp(t, h, st, "b.go:2")
	if err := h.tt.Click("b.go:2"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool {
		return h.a.activePath() == filepath.Join(h.a.root, "b.go") && h.a.activeEditor() != nil
	})
	if h.a.activeEditor().CursorPos().Line != 1 {
		t.Fatalf("declaration cursor = %+v", h.a.activeEditor().CursorPos())
	}
}

func TestQuickHelpWithoutHover(t *testing.T) {
	for _, result := range []string{"empty", "error"} {
		t.Run(result, func(t *testing.T) {
			h, d, offset := quickHelpHarness(t)
			st := h.a.activeEditor()
			st.GoTo(d.buf.Pos(offset + 2))
			h.tt.Frame()
			done := make(chan struct{})
			h.fc.hover = func(context.Context, string, lsp.Position) (string, *lsp.Range, error) {
				defer close(done)
				if result == "error" {
					return "", nil, errors.New("hover failed")
				}
				return "", nil, nil
			}
			h.a.showQuickHelpAtCaret()
			select {
			case <-done:
			case <-time.After(time.Second * 5):
				t.Fatal("hover request did not finish")
			}
			h.tt.Frame()
			if h.a.quickHelp != nil || h.tt.HasText("Declaration") {
				t.Fatal("Quick Help appeared without hover documentation")
			}
		})
	}
}

func quickHelpHarness(t *testing.T) (*harness, *doc, int) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"a.go": "package a\nfunc call() { helper() }\n",
		"b.go": "package a\nfunc helper() int { return 1 }\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.fc.hover = func(_ context.Context, _ string, pos lsp.Position) (string, *lsp.Range, error) {
		if pos.Line != 1 {
			t.Errorf("hover position = %+v", pos)
		}
		return "```go\nfunc helper() int\n```\n\nReturns the result.\n\nUse it when needed.", &lsp.Range{
			Start: lsp.Position{Line: 1, Character: 14}, End: lsp.Position{Line: 1, Character: 20},
		}, nil
	}
	h.fc.def = func(_ context.Context, _ string, _ lsp.Position) ([]lsp.Location, error) {
		return []lsp.Location{{URI: uri.FromPath(filepath.Join(root, "b.go")), Range: lsp.Range{
			Start: lsp.Position{Line: 1, Character: 5}, End: lsp.Position{Line: 1, Character: 11},
		}}}, nil
	}
	d := h.open("a.go")
	t.Cleanup(func() { h.a.lsp.StopWait(5 * time.Second) })
	offset := strings.Index(d.buf.String(), "helper")
	return h, d, offset
}

func writeQuickHelpScreenshots(t *testing.T, h *harness) {
	t.Helper()
	for _, appearance := range []struct {
		dark bool
		name string
	}{{false, "quickhelp-light.png"}, {true, "quickhelp-dark.png"}} {
		h.tt.SetDark(appearance.dark)
		h.tt.Frame()
		dir := os.Getenv("SEE_QUICKHELP_SCREENSHOT_DIR")
		if dir == "" {
			dir = os.Getenv("HOME")
		}
		f, err := os.Create(filepath.Join(dir, appearance.name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, h.tt.Image()); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	h.tt.SetDark(false)
	h.tt.Frame()
}

func waitForQuickHelp(t *testing.T, h *harness, st *editor.State, text string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		h.tt.Frame()
		if h.a.quickHelp == st && h.tt.HasText(text) {
			return
		}
		select {
		case fn := <-h.q:
			fn()
			h.ran++
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			state, err := h.a.lsp.Status()
			t.Fatalf("Quick Help did not show %q: calls=%v server=%v/%v trusted=%v active=%q marker=%p texts=%q", text, h.fc.calls(), state, err, h.a.cfg.IsTrusted(h.a.root), h.a.focus.active, h.a.quickHelp, h.tt.Texts())
		}
	}
}
