package app

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"

	"see/locales"
)

func TestNavigatorBarSegmentsFitSidebar(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"file.txt": "file\n"}))
	h.a.sidebarW = 210
	h.tt.Frame()

	track, ok := h.tt.AccessByRole(ui.RoleRadioGroup, locales.MsgNavigator())
	if !ok {
		t.Fatalf("navigator segmented control is missing: %v", h.tt.AccessAll())
	}
	labels := []string{
		"Show the Project Navigator",
		"Show the Find Navigator",
		"Show the Git Navigator",
		commandLabel("Show Problems"),
		commandLabel("Show Outline"),
		commandLabel("Show Bookmarks"),
	}
	var width float32
	for i, label := range labels {
		segment, ok := h.tt.AccessByRole(ui.RoleRadio, label)
		if !ok {
			t.Fatalf("navigator segment %q is missing: %v", label, h.tt.AccessAll())
		}
		if segment.Offscreen {
			t.Fatalf("navigator segment %q is offscreen at %+v", label, segment.Bounds)
		}
		if segment.Bounds.X < track.Bounds.X-0.5 || segment.Bounds.X+segment.Bounds.W > track.Bounds.X+track.Bounds.W+0.5 {
			t.Fatalf("navigator segment %q at %+v escapes its track %+v", label, segment.Bounds, track.Bounds)
		}
		if i == 0 {
			width = segment.Bounds.W
		} else if math.Abs(float64(segment.Bounds.W-width)) > 0.5 {
			t.Fatalf("navigator segment %q is %v wide, want about %v", label, segment.Bounds.W, width)
		}
	}
	if err := h.tt.Click(commandLabel("Show Bookmarks")); err != nil {
		t.Fatalf("clicking the last navigator segment: %v", err)
	}
	h.tt.Frame()
	if h.a.navigator != navBookmarks {
		t.Fatalf("clicking the last navigator segment chose %d", h.a.navigator)
	}
}
