package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"see/internal/config"
)

func TestPageGuideSettings(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "guide.txt"), []byte(strings.Repeat("content\n", 30)), 0600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	path := filepath.Join(t.TempDir(), "config.json")
	h.a.cfg = config.Load(path)
	doc := h.open("guide.txt")
	st := h.a.focus.eds[doc.path]
	h.a.settings.pane = "editor"
	settings := ui.NewTester(h.a.settingsView, 900, 680)
	click := func(label string) {
		t.Helper()
		if err := settings.Click(label); err != nil {
			t.Fatal(err)
		}
		h.tt.Frame()
	}
	wantGuide := func(columns ...int) int {
		t.Helper()
		if !slices.Equal(st.WrapGuides, columns) || st.SoftWrap || st.WrapColumn != 0 {
			t.Fatalf("guides = %v, wrap = %v at %d, want %v without wrapping", st.WrapGuides, st.SoftWrap, st.WrapColumn, columns)
		}
		background := st.Theme.Background
		guide := st.Theme.WrapGuide.Over(background)
		image := h.tt.Image()
		for x := range image.Bounds().Dx() {
			p := image.RGBAAt(x, 220)
			q := image.RGBAAt(x, 280)
			if p == q && p.R == guide.R && p.G == guide.G && p.B == guide.B {
				if len(columns) == 0 {
					t.Fatalf("guide still painted at x=%d", x)
				}
				// A neutral guide needs clear separation from the editor background.
				if max(p.R, background.R)-min(p.R, background.R) < 64 {
					t.Fatalf("guide %v is too close to background %v", p, background)
				}
				return x
			}
		}
		if len(columns) != 0 {
			t.Fatal("guide was not painted across the source lines")
		}
		return 0
	}

	wantGuide()
	click("Show Page Guide")
	first := wantGuide(80)
	r, ok := settings.Find("Page Guide Column")
	if !ok {
		t.Fatal("page guide column field not found")
	}
	settings.ClickAt(r.X+16, r.Y+r.H/2)
	settings.Key(ui.Cmd, ui.KeyA)
	settings.Type("48")
	h.tt.Frame()
	if got := wantGuide(48); got >= first {
		t.Fatalf("column 48 at x=%d did not move left from column 80 at x=%d", got, first)
	}

	h.tt.SetDark(true)
	wantGuide(48)
	h.tt.SetDark(false)

	settings.Key(ui.Cmd, ui.KeyA)
	settings.Type("0")
	h.tt.Frame()
	wantGuide(48)

	click("Show Page Guide")
	wantGuide()
	click("Show Page Guide")
	wantGuide(48)
	h.a.cfg = config.Load(path)
	h.tt.Frame()
	wantGuide(48)

	click("Advanced")
	click("Reset")
	click("Editor")
	wantGuide()
	click("Show Page Guide")
	wantGuide(80)
	r, ok = settings.Find("Page Guide Column")
	if !ok {
		t.Fatal("page guide column field not found after reset")
	}
	settings.ClickAt(r.X+16, r.Y+r.H/2)
	settings.Key(0, ui.KeyUp)
	h.tt.Frame()
	wantGuide(81)
}
