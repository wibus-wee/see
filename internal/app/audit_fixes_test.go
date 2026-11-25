package app

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"see/internal/langserver"
	"see/internal/lsp"
)

func TestDefinitionF12Dispatch(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nvar source int\n"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.fc.def = func(context.Context, string, lsp.Position) ([]lsp.Location, error) { return nil, nil }
	h.open("a.go")
	h.settle(func() bool { return h.a.lsp.Current() != nil })
	h.tt.Frame()
	h.tt.Key(0, ui.KeyF12)
	h.tt.Frame()
	h.settle(func() bool { return slices.Contains(h.fc.calls(), "definition") })
	h.a.lsp.Stop()
}

func TestCloseWithoutConfirmationDiscards(t *testing.T) {
	for _, untitled := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing", true: "untitled"}[untitled], func(t *testing.T) {
			h := newHarness(t, writeTree(t, map[string]string{"a.txt": "original"}))
			d := h.open("a.txt")
			if untitled {
				h.a.newFile()
				d = h.a.docs[h.a.activePath()]
			}
			d.buf.Set("unsaved")
			h.a.cfg.ConfirmClose = false
			h.host.confirm = func(string) (int, error) {
				t.Error("unexpected confirmation")
				return 2, nil
			}
			h.a.closeDoc(d.path)
			h.tt.Frame()
			if h.a.confirm != nil || h.a.docs[d.path] != nil {
				t.Fatal("dirty document was not closed")
			}
			if untitled {
				h.settleFrames(func() bool {
					d.cache.mu.Lock()
					defer d.cache.mu.Unlock()
					return d.cache.removed
				})
			}
			content, err := os.ReadFile(d.path)
			if untitled {
				if !os.IsNotExist(err) {
					t.Fatalf("discarded draft remains: %v", err)
				}
			} else if err != nil || string(content) != "original" {
				t.Fatalf("disk content changed: %q, %v", content, err)
			}
		})
	}
}

func TestCloneCompletionRedrawsWelcome(t *testing.T) {
	a, w, mainWin, welcomeWin := newTestWindows(t)
	a.showWelcome = true
	a.clone = welcomeClone{open: true, cloning: true}
	w.sync(nil)
	before := welcomeWin.updates
	a.clone.cloning, a.clone.err = false, "clone failed"
	w.sync(mainWin)
	if welcomeWin.updates != before+1 {
		t.Fatal("background completion did not redraw welcome")
	}
	w.sync(mainWin)
	if welcomeWin.updates != before+1 {
		t.Fatal("unchanged state kept redrawing")
	}
}

func TestSplitPaletteCommands(t *testing.T) {
	for _, name := range []string{"editorGroup.splitRight", "editorGroup.splitDown"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "")
			if h.a.commandEnabled(name) {
				t.Fatal("split enabled without a tab")
			}
			h.a.newFile()
			h.a.openPalette()
			h.a.quick.query = ">" + name
			h.a.rankQuick()
			h.a.chooseQuick(0)
			if len(h.a.groups()) != 2 {
				t.Fatal("palette command did not split editor")
			}
		})
	}
}

func TestRevokingTrustStopsServers(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.open("a.go")
	m := h.a.lsp
	h.settle(func() bool { return m.Current() != nil })
	h.a.removeTrust(root)
	h.tt.Frame()
	if m.Current() != nil || h.a.lsp == m || h.a.lsp.Current() != nil || h.a.tsLSP.Current() != nil || h.a.clangLSP.Current() != nil || h.a.cmakeLSP.Current() != nil {
		t.Fatal("revoked workspace retained a running language server")
	}
	if h.a.lspState != langserver.Off || h.a.cfg.IsTrusted(root) || !h.a.trustAsk {
		t.Fatal("revoked workspace retained running status or trust")
	}
	h.a.trust()
	if h.a.lsp == nil || h.a.lsp == m {
		t.Fatal("re-trusting did not create fresh servers")
	}
	h.a.lsp.Stop()
}

func TestTrustBannerOnlyOnEditor(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\n"}))
	h.open("a.go")
	h.a.trustAsk = true
	h.tt.Frame()
	if !h.tt.HasText("Trust Folder") {
		t.Fatal("source editor lacks trust banner")
	}
	h.a.openGitLog()
	h.tt.Frame()
	if h.tt.HasText("Trust Folder") {
		t.Fatal("Git Log inherited source editor trust banner")
	}
}
