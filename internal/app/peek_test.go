package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"see/internal/lsp"
	"see/internal/uri"
	"see/locales"
)

func TestPeekDefinition(t *testing.T) {
	for _, result := range []string{"single", "multiple", "empty", "skipped", "error"} {
		t.Run(result, func(t *testing.T) {
			root := writeTree(t, map[string]string{"a.go": "package a\nvar source int\n", "b.go": "package a\n\ntype Target int\n"})
			h := newHarness(t, root)
			defer h.a.lsp.StopWait(5e9)
			loc := lsp.Location{URI: uri.FromPath(filepath.Join(root, "b.go")), Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 5}, End: lsp.Position{Line: 2, Character: 11}}}
			h.fc.def = func(ctx context.Context, docURI string, pos lsp.Position) ([]lsp.Location, error) {
				switch result {
				case "error":
					return nil, errors.New("navigation failed")
				case "empty":
					return nil, nil
				case "skipped":
					return []lsp.Location{{URI: "untitled:one", Range: loc.Range}}, nil
				case "multiple":
					return []lsp.Location{loc, loc}, nil
				default:
					return []lsp.Location{loc}, nil
				}
			}
			h.a.cfg.Trust(root)
			d := h.open("a.go")
			h.settle(func() bool { return h.a.lsp.Current() != nil })
			h.a.activeEditor().GoTo(d.buf.Pos(strings.Index(d.buf.String(), "source")))
			h.tt.Frame()
			h.tt.Key(ui.Alt, ui.KeyF12)
			h.tt.Frame()
			switch result {
			case "single":
				h.settle(func() bool { return h.a.peek != nil })
				p := h.a.peek
				if p.t.path != filepath.Join(root, "b.go") || p.t.pos.Line != 2 {
					t.Fatalf("peek target: %+v", p.t)
				}
				if p.from.Path != d.path || p.from.Line != 1 {
					t.Fatalf("peek from: %+v", p.from)
				}
				if p.preview == nil {
					t.Fatal("no preview")
				}
				if got := p.preview.Buffer.Line(2); got != "type Target int" {
					t.Fatalf("preview line: %q", got)
				}
				if p.preview.Cursor() != strings.Index(p.preview.Buffer.String(), "Target") {
					t.Fatalf("preview cursor: %v", p.preview.CursorPos())
				}
				// Esc closes the peek and opens nothing.
				h.tt.Key(0, ui.KeyEscape)
				h.tt.Frame()
				if h.a.peek != nil || h.a.activePath() != d.path {
					t.Fatal("Esc did not close the peek")
				}
			case "multiple":
				h.settle(func() bool { return h.a.choices != nil && h.a.choices.previewEditor != nil })
				if h.a.peek != nil {
					t.Fatal("several definitions must give the choice list, not a peek")
				}
			case "empty":
				h.settle(func() bool { return h.hasFeedback(locales.MsgNoDefinitionFound()) })
			case "skipped":
				h.settle(func() bool { return h.hasFeedback(locales.MsgTheDefinitionIsInAPlaceThatIs()) })
			case "error":
				h.settle(func() bool { return h.feedbackContains("navigation failed") })
			}
		})
	}
}

// The Go to Definition button of a peek jumps to the definition as
// Go to Definition does, recording where it came from.
func TestPeekDefinitionGoesThere(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nvar source int\n", "b.go": "package a\n\ntype Target int\n"})
	h := newHarness(t, root)
	defer h.a.lsp.StopWait(5e9)
	h.fc.def = func(ctx context.Context, docURI string, pos lsp.Position) ([]lsp.Location, error) {
		return []lsp.Location{{URI: uri.FromPath(filepath.Join(root, "b.go")), Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 5}, End: lsp.Position{Line: 2, Character: 11}}}}, nil
	}
	h.a.cfg.Trust(root)
	d := h.open("a.go")
	h.settle(func() bool { return h.a.lsp.Current() != nil })
	off := strings.Index(d.buf.String(), "source")
	h.a.activeEditor().GoTo(d.buf.Pos(off))
	h.a.peekDefinition(h.a.focus, d, off)
	h.settle(func() bool { return h.a.peek != nil })
	p := h.a.peek
	h.a.jump(p.group, p.from, p.t, p.enc)
	h.a.peek = nil
	h.settle(func() bool { return h.a.activePath() == filepath.Join(root, "b.go") && h.a.activeEditor() != nil })
	if h.a.activeEditor().Cursor() != len("package a\n\ntype ") {
		t.Fatalf("wrong cursor: %v", h.a.activeEditor().CursorPos())
	}
	h.a.goBack()
	h.settle(func() bool { return h.a.activePath() == d.path && h.a.activeEditor() != nil })
	if h.a.activeEditor().Cursor() != off {
		t.Fatal("history lost the origin")
	}
}
