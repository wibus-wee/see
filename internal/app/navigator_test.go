package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"see/locales"
)

func TestNavigatorBarSegmentsScrollSideways(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"file.txt": "file\n"}))
	h.a.sidebarW = 210
	h.tt.Frame()

	if _, ok := h.tt.AccessByRole(ui.RoleRadioGroup, locales.MsgNavigator()); !ok {
		t.Fatalf("navigator segmented control is missing: %v", h.tt.AccessAll())
	}
	segment := func(label string) ui.AccessInfo {
		t.Helper()
		s, ok := h.tt.AccessByRole(ui.RoleRadio, label)
		if !ok {
			t.Fatalf("navigator segment %q is missing: %v", label, h.tt.AccessAll())
		}
		return s
	}
	first := segment(locales.MsgProjectNavigator())
	second := segment(locales.MsgFindNavigator())
	last := segment(bookmarkText("bookmarksTitle"))
	if first.Bounds.W != 38 || second.Bounds.X-first.Bounds.X != 40 {
		t.Fatalf("navigator segments do not keep their fixed spacing: first %+v, second %+v", first.Bounds, second.Bounds)
	}
	if !last.Offscreen {
		t.Fatalf("narrow sidebar did not initially clip the last segment: %+v", last.Bounds)
	}

	h.tt.Wheel(100, 60, 120, 0)
	h.tt.Advance(150 * time.Millisecond)
	last = segment(bookmarkText("bookmarksTitle"))
	if last.Offscreen || last.Bounds.X+last.Bounds.W > 200.5 {
		t.Fatalf("horizontal scrolling did not reveal the last segment in the visible pill: %+v", last.Bounds)
	}
	img := h.tt.Image()
	br, bg, bb, ba := img.At(30, 70).RGBA()
	for x := 30; x < 190; x++ {
		r, g, b, a := img.At(x, 70).RGBA()
		if r != br || g != bg || b != bb || a != ba {
			t.Fatalf("navigator shows scroll chrome at x=%d", x)
		}
	}
	if err := h.tt.Click(bookmarkText("bookmarksTitle")); err != nil {
		t.Fatalf("clicking the revealed navigator segment: %v", err)
	}
	h.tt.Frame()
	if h.a.navigator != navBookmarks {
		t.Fatalf("clicking the revealed navigator segment chose %d", h.a.navigator)
	}
}

func TestNavigatorKeyboardSelectionScrollsIntoView(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"file.txt": "file\n"}))
	h.a.sidebarW = 210
	h.tt.Frame()
	if err := h.tt.Click(locales.MsgProjectNavigator()); err != nil {
		t.Fatalf("focusing the navigator: %v", err)
	}
	for range 5 {
		h.tt.Key(0, ui.KeyRight)
		h.tt.Advance(80 * time.Millisecond)
	}
	if h.a.navigator != navBookmarks {
		t.Fatalf("right arrows chose navigator %d", h.a.navigator)
	}
	last, ok := h.tt.AccessByRole(ui.RoleRadio, bookmarkText("bookmarksTitle"))
	if !ok || last.Offscreen {
		t.Fatalf("keyboard selection did not scroll the last segment into view: %+v", last.Bounds)
	}
}

func TestNavigatorBarNamesSegmentsOnHover(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"file.txt": "file\n"}))
	h.tt.Frame()
	name := locales.MsgProjectNavigator()
	seg, ok := h.tt.AccessByRole(ui.RoleRadio, name)
	if !ok {
		t.Fatalf("navigator segment %q is missing: %v", name, h.tt.AccessAll())
	}
	h.tt.Move(seg.Bounds.X+seg.Bounds.W/2, seg.Bounds.Y+seg.Bounds.H/2)
	h.tt.Advance(700 * time.Millisecond)
	if !h.tt.HasText(name) {
		t.Fatalf("resting on the segment does not show %q: %v", name, h.tt.Texts())
	}
}

func TestNavigatorBarSegmentsAreAccessible(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"file.txt": "file\n"}))
	h.tt.Frame()
	if _, ok := h.tt.AccessByRole(ui.RoleRadioGroup, locales.MsgNavigator()); !ok {
		t.Fatalf("navigator segmented control is missing: %v", h.tt.AccessAll())
	}
	names := []string{
		locales.MsgProjectNavigator(),
		locales.MsgFindNavigator(),
		locales.MsgGitNavigator(),
		problemText("problemsList"),
		outlineText("outlineTitle"),
		bookmarkText("bookmarksTitle"),
	}
	for i, name := range names {
		seg, ok := h.tt.AccessByRole(ui.RoleRadio, name)
		if !ok {
			t.Fatalf("navigator segment %q is missing: %v", name, h.tt.AccessAll())
		}
		if !seg.Focusable || !seg.Segment {
			t.Errorf("navigator segment %q is not a focusable segment: %+v", name, seg)
		}
		if seg.Description != name {
			t.Errorf("navigator segment %q has description %q", name, seg.Description)
		}
		if seg.Checked != (i == h.a.navigator) {
			t.Errorf("navigator segment %q checked = %v with %d chosen", name, seg.Checked, h.a.navigator)
		}
	}
}
