package app

import (
	"path/filepath"
	"runtime"
	"testing"

	"see/locales"
)

func TestTitlebarProjectMenuOpensRecentFolder(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the in-window project menu is used on Linux")
	}
	current, next := t.TempDir(), t.TempDir()
	h := newHarness(t, current)
	h.a.cfg.Recent = []string{current, next}
	opened := ""
	h.host.openProject = func(path string) bool {
		opened = path
		return true
	}
	h.tt.Frame()

	if err := h.tt.Click(locales.MsgSwitchProject()); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem(filepath.Base(next)); err != nil {
		t.Fatal(err)
	}
	if opened != next {
		t.Fatalf("opened folder = %q, want %q", opened, next)
	}
	if h.a.root != current {
		t.Fatalf("current window folder = %q, want %q", h.a.root, current)
	}
	h.tt.Frame()
	if err := h.tt.Click(locales.MsgSwitchProject()); err != nil {
		t.Fatal(err)
	}
	h.tt.CloseMenu()
	h.tt.Frame()
	if h.a.titleOpen {
		t.Fatal("project menu stayed open after dismissal")
	}
}
