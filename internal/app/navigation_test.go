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
)

func TestNavigationCommands(t *testing.T) {
	for _, kind := range []string{"implementation", "typeDefinition"} {
		for _, result := range []string{"single", "multiple", "empty", "error", "unsupported"} {
			t.Run(kind+"/"+result, func(t *testing.T) {
				root := writeTree(t, map[string]string{"a.go": "package a\nvar source int\n", "b.go": "package a\ntype Target int\n"})
				h := newHarness(t, root)
				defer h.a.lsp.StopWait(5e9)
				h.fc.noNavigation = result == "unsupported"
				answer := func(ctx context.Context, docURI string, pos lsp.Position) ([]lsp.Location, error) {
					if pos != (lsp.Position{Line: 1, Character: 4}) {
						t.Errorf("position: %+v", pos)
					}
					if result == "error" {
						return nil, errors.New("navigation failed")
					}
					if result == "empty" {
						return nil, nil
					}
					loc := lsp.Location{URI: uri.FromPath(filepath.Join(root, "b.go")), Range: lsp.Range{Start: lsp.Position{Line: 1, Character: 5}, End: lsp.Position{Line: 1, Character: 11}}}
					if result == "multiple" {
						return []lsp.Location{loc, loc}, nil
					}
					return []lsp.Location{loc}, nil
				}
				h.fc.implementation, h.fc.typeDefinition = answer, answer
				h.a.cfg.Trust(root)
				d := h.open("a.go")
				h.settle(func() bool { return h.a.lsp.Current() != nil })
				h.a.activeEditor().GoTo(d.buf.Pos(strings.Index(d.buf.String(), "source")))
				h.tt.Frame()
				name := navigationCommand(kind)
				if result == "unsupported" {
					if h.a.commandEnabled(name) {
						t.Fatal("unsupported command enabled")
					}
					items := h.a.navigationMenuItems(func(fn func()) { fn() })
					for _, item := range items {
						if item.Label == commandLabel(name) && !item.Disabled {
							t.Fatal("unsupported menu enabled")
						}
					}
					h.a.navigateActive(kind)
					if h.a.finding {
						t.Fatal("unsupported request started")
					}
					return
				}
				if kind == "implementation" {
					h.tt.Key(ui.Cmd, ui.KeyF12)
				} else {
					h.a.openPalette()
					h.a.quick.query = ">" + name
					h.a.rankQuick()
					h.tt.Frame()
					h.tt.Key(0, ui.KeyEnter)
				}
				h.tt.Frame()
				switch result {
				case "single":
					h.settle(func() bool { return h.a.activePath() == filepath.Join(root, "b.go") && h.a.activeEditor() != nil })
					if h.a.activeEditor().Cursor() != len("package a\ntype ") {
						t.Fatalf("wrong cursor: %v", h.a.activeEditor().CursorPos())
					}
					h.a.goBack()
					h.settle(func() bool { return h.a.activePath() == d.path && h.a.activeEditor() != nil })
					if h.a.activeEditor().Cursor() != strings.Index(d.buf.String(), "source") {
						t.Fatal("history lost origin")
					}
				case "multiple":
					h.settle(func() bool { return h.a.choices != nil && h.a.choices.previewEditor != nil })
					if h.a.choices.navigation != kind || len(h.a.choices.locs) != 2 {
						t.Fatalf("choices: %+v", h.a.choices)
					}
				case "empty":
					h.settle(func() bool { return h.a.notice == navigationText(kind, "Empty") })
				case "error":
					h.settle(func() bool { return strings.Contains(h.a.notice, "navigation failed") })
				}
			})
		}
	}
}

func TestImplementationShortcutDoesNotConflict(t *testing.T) {
	for _, cmd := range commands() {
		if cmd.shortcutKey == ui.KeyF12 && cmd.shortcutMods == ui.Cmd && cmd.name != "Go to Implementation" {
			t.Fatalf("shortcut conflicts with %s", cmd.name)
		}
		if cmd.name == "Go to Type Definition" && cmd.shortcutKey != 0 {
			t.Fatal("type definition has a default shortcut")
		}
	}
}
