package app

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"see/internal/config"
	"see/locales"
)

func openRegexToolHarness(t *testing.T) (*harness, *doc, *regexPlayground) {
	t.Helper()
	h := newHarness(t, writeTree(t, map[string]string{
		"a.go": "package a\nimport \"regexp\"\nvar re = regexp.MustCompile(`word`)\n",
		"b.go": "package b\nvar value = 1\n",
	}))
	d := h.open("a.go")
	st := h.a.focus.eds[d.path]
	st.GoTo(st.Buffer.Pos(strings.Index(d.buf.String(), "word")))
	h.a.openRegexPlaygroundFor(h.a.focus, d, st)
	h.tt.Frame()
	h.tt.Frame()
	if h.a.regexPlayground == nil || !h.tt.HasText(locales.RegexToolWindow()) {
		t.Fatal("editor regex tool did not open")
	}
	return h, d, h.a.regexPlayground
}

func regexToolGroupBounds(t *testing.T, h *harness, g *group) ui.Rect {
	t.Helper()
	label := fmt.Sprintf("%s %d", locales.Catalog.Format(i18n.Locale(), "editorGroup", nil), g.id)
	bounds, ok := h.tt.Find(label)
	if !ok {
		t.Fatalf("editor group %q was not found", label)
	}
	return bounds
}

func dragTester(t *testing.T, h *harness, bounds ui.Rect, dx, dy float32) {
	t.Helper()
	x, y := bounds.X+bounds.W/2, bounds.Y+bounds.H/2
	h.tt.Press(x, y)
	h.tt.Move(x+dx, y+dy)
	h.tt.Release(x+dx, y+dy)
	h.tt.Frame()
}

func regexToolPixelsDiffer(before, after []uint8, stride, width, height int, bounds ui.Rect) bool {
	x0, y0 := max(0, int(bounds.X)), max(0, int(bounds.Y))
	x1, y1 := min(width, int(bounds.X+bounds.W)+1), min(height, int(bounds.Y+bounds.H)+1)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			offset := y*stride + x*4
			if offset+3 < len(before) && offset+3 < len(after) &&
				(before[offset] != after[offset] || before[offset+1] != after[offset+1] ||
					before[offset+2] != after[offset+2] || before[offset+3] != after[offset+3]) {
				return true
			}
		}
	}
	return false
}

func TestRegexToolWindowTitleAndAllResizeHandles(t *testing.T) {
	h, _, _ := openRegexToolHarness(t)
	original := h.a.regexToolWindow.rect
	title, ok := h.tt.Find(locales.RegexToolTitlebar())
	if !ok {
		t.Fatal("titlebar drag handle is not accessible")
	}
	dragTester(t, h, title, 24, 18)
	if got := h.a.regexToolWindow.rect; got.X != original.X+24 || got.Y != original.Y+18 {
		t.Fatalf("title drag rect = %+v, want x/y %+v", got, ui.Rect{X: original.X + 24, Y: original.Y + 18})
	}

	for _, test := range []struct {
		label         string
		cursor        ui.Cursor
		dx, dy        float32
		width, height bool
	}{
		{locales.RegexToolResizeNorth(), ui.CursorResizeN, 0, -8, false, true},
		{locales.RegexToolResizeSouth(), ui.CursorResizeS, 0, 8, false, true},
		{locales.RegexToolResizeWest(), ui.CursorResizeW, -8, 0, true, false},
		{locales.RegexToolResizeEast(), ui.CursorResizeE, 8, 0, true, false},
		{locales.RegexToolResizeNorthWest(), ui.CursorResizeNWSE, -8, -8, true, true},
		{locales.RegexToolResizeNorthEast(), ui.CursorResizeNESW, 8, -8, true, true},
		{locales.RegexToolResizeSouthWest(), ui.CursorResizeNESW, -8, 8, true, true},
		{locales.RegexToolResizeSouthEast(), ui.CursorResizeNWSE, 8, 8, true, true},
	} {
		t.Run(test.label, func(t *testing.T) {
			target, ok := h.tt.Find(test.label)
			if !ok || target.W == 0 || target.H == 0 {
				t.Fatalf("resize hit target %q is missing: %+v %v", test.label, target, ok)
			}
			x, y := target.X+target.W/2, target.Y+target.H/2
			window := h.a.regexToolWindow.rect
			neutralX, neutralY := window.X+window.W/2, window.Y+20
			h.tt.Move(neutralX, neutralY)
			h.tt.Frame()
			baselineImage := h.tt.Image()
			baseline := append([]uint8(nil), baselineImage.Pix...)
			h.tt.Move(x, y)
			h.tt.Frame()
			if got := h.tt.Cursor(); got != test.cursor {
				t.Fatalf("cursor at %s = %v, want %v", test.label, got, test.cursor)
			}
			if test.cursor == ui.CursorResizeNWSE || test.cursor == ui.CursorResizeNESW {
				corner := target
				corner.W, corner.H = target.W/2, target.H/2
				if test.dx > 0 {
					corner.X += corner.W
				}
				if test.dy > 0 {
					corner.Y += corner.H
				}
				if !regexToolPixelsDiffer(baseline, h.tt.Image().Pix, baselineImage.Stride, baselineImage.Bounds().Dx(), baselineImage.Bounds().Dy(), corner) {
					t.Fatalf("hovering %s did not draw its curved corner grip", test.label)
				}
				h.tt.Move(neutralX, neutralY)
				h.tt.Frame()
				if regexToolPixelsDiffer(baseline, h.tt.Image().Pix, baselineImage.Stride, baselineImage.Bounds().Dx(), baselineImage.Bounds().Dy(), target) {
					t.Fatalf("curved corner grip remained visible after leaving %s", test.label)
				}
			}
			before := h.a.regexToolWindow.rect
			h.tt.Press(x, y)
			h.tt.Move(x+test.dx, y+test.dy)
			h.tt.Release(x+test.dx, y+test.dy)
			h.tt.Frame()
			after := h.a.regexToolWindow.rect
			if test.width && before.W == after.W || test.height && before.H == after.H {
				t.Fatalf("%s drag did not resize: before=%+v after=%+v", test.label, before, after)
			}
		})
	}
}

func TestRegexToolTitlebarControlsAlignAtOppositeEdges(t *testing.T) {
	h, _, _ := openRegexToolHarness(t)
	drag, dragFound := h.tt.Find(locales.RegexToolTitlebar())
	title, titleFound := h.tt.Find(locales.RegexPlaygroundTitle())
	close, closeFound := h.tt.Find(locales.Close())
	glyph, glyphFound := h.tt.Find("×")
	if !dragFound || !titleFound || !closeFound || !glyphFound {
		t.Fatalf("titlebar controls missing: drag=%v title=%v close=%v glyph=%v", dragFound, titleFound, closeFound, glyphFound)
	}
	if title.X > drag.X+1 {
		t.Fatalf("title is not left-aligned within the draggable area: title=%+v drag=%+v", title, drag)
	}
	centerY := func(r ui.Rect) float32 { return r.Y + r.H/2 }
	if math.Abs(float64(centerY(title)-centerY(close))) > 1 ||
		math.Abs(float64(glyph.X+glyph.W/2-(close.X+close.W/2))) > 1 ||
		math.Abs(float64(centerY(glyph)-centerY(close))) > 1 {
		t.Fatalf("titlebar controls are not centered: title=%+v close=%+v glyph=%+v", title, close, glyph)
	}
}

func TestRegexToolCloseAndNorthEastResizeRemainReachable(t *testing.T) {
	t.Run("close center", func(t *testing.T) {
		h, _, _ := openRegexToolHarness(t)
		closeBounds, ok := h.tt.Find(locales.Close())
		if !ok {
			t.Fatal("close button is missing")
		}
		x, y := closeBounds.X+closeBounds.W/2, closeBounds.Y+closeBounds.H/2
		h.tt.Move(x, y)
		h.tt.Frame()
		if got := h.tt.Cursor(); got == ui.CursorResizeNESW || got == ui.CursorResizeNWSE {
			t.Fatalf("close center shows resize cursor %v", got)
		}
		if err := h.tt.Click(locales.Close()); err != nil {
			t.Fatalf("clicking close center: %v", err)
		}
		h.tt.Frame()
		if h.a.regexPlayground != nil {
			t.Fatal("close button center was intercepted by the northeast resize target")
		}
	})

	t.Run("northeast resize", func(t *testing.T) {
		h, _, _ := openRegexToolHarness(t)
		closeBounds, closeFound := h.tt.Find(locales.Close())
		target, ok := h.tt.Find(locales.RegexToolResizeNorthEast())
		if !closeFound || !ok || target.W == 0 || target.H == 0 {
			t.Fatalf("northeast resize region or close button is missing: resize=%+v/%v close=%+v/%v", target, ok, closeBounds, closeFound)
		}
		if target.X < closeBounds.X+closeBounds.W || target.Y >= closeBounds.Y+closeBounds.H {
			t.Fatalf("northeast resize region is not adjacent to but clear of Close: resize=%+v close=%+v", target, closeBounds)
		}
		x, y := target.X+target.W/2, target.Y+target.H/2
		h.tt.Move(x, y)
		h.tt.Frame()
		if got := h.tt.Cursor(); got != ui.CursorResizeNESW {
			t.Fatalf("cursor next to Close = %v, want northeast diagonal resize", got)
		}
		before := h.a.regexToolWindow.rect
		h.tt.Press(x, y)
		h.tt.Move(x+8, y-8)
		h.tt.Release(x+8, y-8)
		h.tt.Frame()
		after := h.a.regexToolWindow.rect
		if after.W <= before.W || after.H <= before.H || after.Y >= before.Y {
			t.Fatalf("northeast resize next to Close did not change the window: before=%+v after=%+v", before, after)
		}
	})
}

func TestRegexToolResizesAcrossThreeEditorGroupsAndBack(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go": "package a\nimport \"regexp\"\nvar re = regexp.MustCompile(`word`)\n",
		"b.go": "package b\nvar value = 1\n",
		"c.go": "package c\nvar value = 2\n",
	})
	h := newHarness(t, root)
	sourceDoc := h.open("a.go")
	sourceGroup := h.a.focus
	sourceEditor := sourceGroup.eds[sourceDoc.path]
	sourceEditor.GoTo(sourceEditor.Buffer.Pos(strings.Index(sourceDoc.buf.String(), "word")))
	h.a.splitActive(sideRight)
	middleGroup := h.a.focus
	h.open("b.go")
	h.a.splitActive(sideRight)
	lastGroup := h.a.focus
	h.open("c.go")
	h.a.openRegexPlaygroundFor(sourceGroup, sourceDoc, sourceEditor)
	h.tt.Frame()
	h.tt.Frame()
	if h.a.regexPlayground == nil || !h.tt.HasText(locales.RegexToolWindow()) {
		t.Fatal("regex tool did not open in the source editor")
	}

	panesBefore := []ui.Rect{
		regexToolGroupBounds(t, h, sourceGroup),
		regexToolGroupBounds(t, h, middleGroup),
		regexToolGroupBounds(t, h, lastGroup),
	}
	initial := h.a.regexToolWindow.rect
	if initial.X < panesBefore[0].X || initial.Y < panesBefore[0].Y ||
		initial.X+initial.W > panesBefore[0].X+panesBefore[0].W || initial.Y+initial.H > panesBefore[0].Y+panesBefore[0].H {
		t.Fatalf("tool did not start inside its source pane: tool=%+v source=%+v", initial, panesBefore[0])
	}

	targetRight := panesBefore[2].X + panesBefore[2].W/2
	dx := targetRight - (initial.X + initial.W)
	if dx <= 0 {
		t.Fatalf("three-pane fixture leaves no room to expand: tool=%+v panes=%+v", initial, panesBefore)
	}
	east, ok := h.tt.Find(locales.RegexToolResizeEast())
	if !ok {
		t.Fatal("east resize handle is missing before crossing panes")
	}
	dragTester(t, h, east, dx, 0)
	expanded := h.a.regexToolWindow.rect
	if expanded.X+expanded.W <= panesBefore[2].X {
		t.Fatalf("direct edge resize did not reach the third pane: tool=%+v panes=%+v", expanded, panesBefore)
	}
	for i, group := range []*group{sourceGroup, middleGroup, lastGroup} {
		if got := regexToolGroupBounds(t, h, group); got != panesBefore[i] {
			t.Fatalf("pane %d geometry changed during tool resize: before=%+v after=%+v", i, panesBefore[i], got)
		}
	}

	east, ok = h.tt.Find(locales.RegexToolResizeEast())
	if !ok {
		t.Fatal("east resize handle is missing after crossing panes")
	}
	dragTester(t, h, east, initial.X+initial.W-(expanded.X+expanded.W), 0)
	if got := h.a.regexToolWindow.rect; got != initial {
		t.Fatalf("shrinking the tool back did not restore its source-pane bounds: got=%+v want=%+v", got, initial)
	}
	for i, group := range []*group{sourceGroup, middleGroup, lastGroup} {
		if got := regexToolGroupBounds(t, h, group); got != panesBefore[i] {
			t.Fatalf("pane %d geometry changed while shrinking the tool: before=%+v after=%+v", i, panesBefore[i], got)
		}
	}
}

func TestRegexToolReopeningFitsRememberedGeometryToNewNarrowSource(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go": "package a\nimport \"regexp\"\nvar re = regexp.MustCompile(`word`)\n",
		"b.go": "package b\nvar value = 1\n",
		"c.go": "package c\nvar value = 2\n",
	})
	h := newHarness(t, root)
	sourceDoc := h.open("a.go")
	sourceGroup := h.a.focus
	sourceEditor := sourceGroup.eds[sourceDoc.path]
	sourceEditor.GoTo(sourceEditor.Buffer.Pos(strings.Index(sourceDoc.buf.String(), "word")))
	h.a.splitActive(sideRight)
	middleGroup := h.a.focus
	h.open("b.go")
	h.a.splitActive(sideRight)
	narrowGroup := h.a.focus
	narrowDoc := h.open("c.go")
	narrowEditor := narrowGroup.eds[narrowDoc.path]
	h.a.openRegexPlaygroundFor(sourceGroup, sourceDoc, sourceEditor)
	h.tt.Frame()
	h.tt.Frame()

	panes := []ui.Rect{
		regexToolGroupBounds(t, h, sourceGroup),
		regexToolGroupBounds(t, h, middleGroup),
		regexToolGroupBounds(t, h, narrowGroup),
	}
	if narrowGroup == sourceGroup || panes[2].W >= regexToolMinWidth {
		t.Fatalf("fixture does not use a different narrow source: source=%+v narrow=%+v", panes[0], panes[2])
	}
	title, ok := h.tt.Find(locales.RegexToolTitlebar())
	if !ok {
		t.Fatal("titlebar is missing before moving remembered geometry")
	}
	dragTester(t, h, title, 0, -40)
	initial := h.a.regexToolWindow.rect
	east, ok := h.tt.Find(locales.RegexToolResizeEast())
	if !ok {
		t.Fatal("east resize handle is missing before opening elsewhere")
	}
	targetRight := panes[2].X + panes[2].W/2
	dragTester(t, h, east, targetRight-(initial.X+initial.W), 0)
	expanded := h.a.regexToolWindow.rect
	if expanded.X+expanded.W <= panes[2].X {
		t.Fatalf("tool did not expand across the panes: expanded=%+v panes=%+v", expanded, panes)
	}
	if expanded.Y != initial.Y {
		t.Fatalf("horizontal expansion changed remembered vertical position: before=%+v after=%+v", initial, expanded)
	}
	previous := h.a.regexPlayground
	h.a.closeRegexPlayground(previous)
	h.a.openRegexPlaygroundFor(narrowGroup, narrowDoc, narrowEditor)
	h.tt.Frame()
	h.tt.Frame()

	narrowBounds := regexToolGroupBounds(t, h, narrowGroup)
	window, ok := h.tt.Find(locales.RegexToolWindow())
	if !ok || window.X < narrowBounds.X || window.Y < narrowBounds.Y ||
		window.X+window.W > narrowBounds.X+narrowBounds.W || window.Y+window.H > narrowBounds.Y+narrowBounds.H {
		t.Fatalf("reopened tool did not fit its new narrow source pane: window=%+v source=%+v found=%v", window, narrowBounds, ok)
	}
	workspaceBounds := ui.Rect{
		X: min(panes[0].X, panes[1].X, panes[2].X),
		Y: min(panes[0].Y, panes[1].Y, panes[2].Y),
	}
	workspaceRight := max(panes[0].X+panes[0].W, panes[1].X+panes[1].W, panes[2].X+panes[2].W)
	workspaceBottom := max(panes[0].Y+panes[0].H, panes[1].Y+panes[1].H, panes[2].Y+panes[2].H)
	workspaceBounds.W, workspaceBounds.H = workspaceRight-workspaceBounds.X, workspaceBottom-workspaceBounds.Y
	wantWidth, wantHeight := regexToolMinimumSize(workspaceBounds, narrowBounds)
	if !h.a.regexToolWindow.minimumSet || h.a.regexToolWindow.minWidth != wantWidth || h.a.regexToolWindow.minHeight != wantHeight {
		t.Fatalf("reopened tool retained stale minimum: got=%g/%g want=%g/%g", h.a.regexToolWindow.minWidth, h.a.regexToolWindow.minHeight, wantWidth, wantHeight)
	}
	wantRect := clampRegexToolRectWithMinimum(expanded, narrowBounds, wantWidth, wantHeight)
	if h.a.regexPlayground == nil || h.a.regexPlayground.group != narrowGroup || h.a.regexPlayground.doc != narrowDoc || h.a.regexToolWindow.rect != wantRect {
		t.Fatalf("reopened geometry was not fitted from the remembered rectangle: got=%+v want=%+v playground=%+v", h.a.regexToolWindow.rect, wantRect, h.a.regexPlayground)
	}
}

func TestRegexToolWindowClampsToSmallEditor(t *testing.T) {
	h, _, p := openRegexToolHarness(t)
	h.tt.SetSize(500, 320)
	h.tt.Frame()
	h.tt.Frame()
	window, ok := h.tt.Find(locales.RegexToolWindow())
	group := regexToolGroupBounds(t, h, p.group)
	if !ok || window.W <= 0 || window.X < group.X || window.Y < group.Y || window.X+window.W > group.X+group.W || window.Y+window.H > group.Y+group.H {
		t.Fatalf("regex tool is not bounded to its small editor: window=%+v editor=%+v found=%v", window, group, ok)
	}
	if h.a.regexToolWindow.rect.W >= regexToolMinWidth || h.a.regexToolWindow.rect.H >= regexToolMinHeight {
		t.Fatalf("small editor did not shrink effective minimum: %+v", h.a.regexToolWindow.rect)
	}
}

func TestRegexToolCompactCaptureRowsAreReachableByScrolling(t *testing.T) {
	h, _, p := openRegexToolHarness(t)
	p.pattern = `(?P<one>a)(?P<two>b)(?P<three>c)(?P<four>d)(?P<five>e)(?P<six>f)(?P<seven>g)(?P<eight>h)`
	p.patternVersion++
	p.sample.Buffer.Set(strings.Repeat("abcdefgh ", 8))
	h.a.regexToolWindow.rect = ui.Rect{X: 36, Y: 36, W: 500, H: 320}
	h.a.regexToolWindow.initialized = true
	h.tt.Frame()
	if len(p.result.Matches) != 8 || len(p.result.Matches[0].Captures) != 9 {
		t.Fatalf("compact fixture has %d matches and %d captures, want 8 and 9", len(p.result.Matches), len(p.result.Matches[0].Captures))
	}

	tool, ok := h.tt.Find(locales.RegexToolWindow())
	if !ok || tool.W >= 580 || tool.H > 320 {
		t.Fatalf("compact regex tool bounds = %+v, found=%v", tool, ok)
	}
	visibleCapture := func(value string) (ui.Rect, bool) {
		bounds, found := h.tt.Find(value)
		return bounds, found && bounds.H >= 12 && bounds.Y >= tool.Y+40 && bounds.Y+bounds.H <= tool.Y+tool.H
	}
	firstBounds, firstVisible := visibleCapture(`"a"`)
	lastBounds, lastVisible := visibleCapture(`"h"`)
	if firstVisible && lastVisible {
		t.Fatal("compact layout did not require scrolling to reach both capture rows")
	}
	matchesBefore, _ := h.tt.Find(locales.RegexMatches())

	x, y := tool.X+tool.W/2, tool.Y+tool.H-16
	scrolled := false
	for range 12 {
		h.tt.Scroll(x, y, 0, 160)
		h.tt.Frame()
		matchesNow, _ := h.tt.Find(locales.RegexMatches())
		if matchesNow.Y != matchesBefore.Y || matchesNow.H > matchesBefore.H {
			scrolled = true
		}
		firstNow, firstNowVisible := visibleCapture(`"a"`)
		if firstNowVisible {
			firstBounds, firstVisible = firstNow, true
		}
		if captures, found := h.tt.Find(locales.RegexCaptures()); found && captures.H >= 8 && captures.Y+captures.H+8 < tool.Y+tool.H {
			y = captures.Y + captures.H + 8
			break
		}
	}
	captures, _ := h.tt.Find(locales.RegexCaptures())
	if captures.H >= 8 && captures.Y+captures.H+8 < tool.Y+tool.H {
		for range 12 {
			h.tt.Scroll(x, y, 0, 40)
			h.tt.Frame()
			if bounds, visible := visibleCapture(`"a"`); visible {
				firstBounds, firstVisible = bounds, true
			}
			if bounds, visible := visibleCapture(`"h"`); visible {
				lastBounds, lastVisible = bounds, true
			}
			if scrolled && firstVisible && lastVisible {
				return
			}
		}
	}
	t.Fatalf("compact capture rows were not both reachable by scrolling: first=%+v visible=%v last=%+v visible=%v matches before=%+v tool=%+v", firstBounds, firstVisible, lastBounds, lastVisible, matchesBefore, tool)
}

func TestRegexToolWindowClampsWhenSidebarExpands(t *testing.T) {
	h, _, p := openRegexToolHarness(t)
	h.a.regexToolWindow.rect = ui.Rect{X: 400, Y: 80, W: 680, H: 420}
	h.a.regexToolWindow.initialized = true
	h.tt.Advance(300 * time.Millisecond)

	groupBeforeResize := regexToolGroupBounds(t, h, p.group)
	splitterX := h.a.sidebarW + 0.5
	splitterY := groupBeforeResize.Y + groupBeforeResize.H/2
	h.tt.Move(splitterX, splitterY)
	h.tt.Frame()
	if cursor := h.tt.Cursor(); cursor != ui.CursorResizeEW {
		t.Fatalf("sidebar divider did not expose its horizontal resize cursor: %v", cursor)
	}
	h.tt.Press(splitterX, splitterY)
	h.tt.Move(splitterX+200, splitterY)
	h.tt.Release(splitterX+200, splitterY)
	h.tt.Frame()
	if h.a.sidebarW < 450 {
		t.Fatalf("sidebar divider did not expand the sidebar: width=%v", h.a.sidebarW)
	}

	window, ok := h.tt.Find(locales.RegexToolWindow())
	group := regexToolGroupBounds(t, h, p.group)
	if !ok || window.X < group.X || window.Y < group.Y || window.X+window.W > group.X+group.W || window.Y+window.H > group.Y+group.H {
		t.Fatalf("floating tool escaped its editor after sidebar expansion: window=%+v editor=%+v found=%v", window, group, ok)
	}
}

func TestRegexToolWindowClampsRestoredGeometryToSmallViewport(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go": "package a\nimport \"regexp\"\nvar re = regexp.MustCompile(`word`)\n",
		"b.go": "package b\nvar value = 1\n",
		"c.go": "package c\nvar value = 2\n",
	})
	cfg := &config.Config{ConfirmClose: true, SidebarWidth: 240}
	h := newHarnessWithConfig(t, root, cfg)
	sourceDoc := h.open("a.go")
	sourceGroup := h.a.focus
	sourceEditor := sourceGroup.eds[sourceDoc.path]
	sourceEditor.GoTo(sourceEditor.Buffer.Pos(strings.Index(sourceDoc.buf.String(), "word")))
	h.a.openRegexPlaygroundFor(sourceGroup, sourceDoc, sourceEditor)
	h.tt.Frame()
	h.tt.Frame()
	h.a.splitActive(sideRight)
	middleGroup := h.a.focus
	h.open("b.go")
	h.a.splitActive(sideRight)
	lastGroup := h.a.focus
	h.open("c.go")
	h.tt.Advance(300 * time.Millisecond)

	if panes := []ui.Rect{
		regexToolGroupBounds(t, h, sourceGroup),
		regexToolGroupBounds(t, h, middleGroup),
		regexToolGroupBounds(t, h, lastGroup),
	}; panes[0].W >= 440 || panes[1].W >= 440 || panes[2].W >= 440 {
		t.Fatalf("fixture did not create narrow split panes: %+v", panes)
	}

	wide := ui.Rect{X: 400, Y: 80, W: 680, H: 420}
	h.a.regexToolWindow.rect = wide
	h.a.regexToolWindow.initialized = true
	h.tt.Frame()
	h.a.captureSession()
	saved := cfg.Workspaces[root]
	if saved.RegexToolX != wide.X || saved.RegexToolWidth != wide.W {
		t.Fatalf("wide geometry was not persisted before restore: %+v", saved)
	}
	h.a.sidebarOff = true
	h.tt.Advance(300 * time.Millisecond)
	h.a.sidebarOff = false
	h.tt.Advance(300 * time.Millisecond)
	h.a.restoreRegexToolWindow(saved)
	h.tt.Frame()
	if h.a.sidebarOff || regexToolGroupBounds(t, h, sourceGroup).X <= 0 {
		t.Fatal("sidebar did not reopen beside the split editor")
	}

	const viewportWidth, viewportHeight = 590, 450
	h.tt.SetSize(viewportWidth, viewportHeight)
	_, windowFound := h.tt.Find(locales.RegexToolWindow())
	if !windowFound {
		t.Fatal("restored regex tool disappeared after shrinking the window")
	}
	close, closeFound := h.tt.Find(locales.Close())
	geometry := h.a.regexToolWindow.rect
	closeX, closeY := close.X+close.W/2, close.Y+close.H/2
	if geometry.X < 0 || geometry.Y < 0 || geometry.X+geometry.W > viewportWidth || geometry.Y+geometry.H > viewportHeight ||
		!closeFound || close.W == 0 || close.H == 0 || close.X < geometry.X || close.Y < geometry.Y || close.X+close.W > geometry.X+geometry.W || close.Y+close.H > geometry.Y+geometry.H ||
		close.X+close.W > viewportWidth || close.Y+close.H > viewportHeight {
		t.Fatalf("restored tool or close control escaped the current %dx%d-DIP viewport: geometry=%+v close=%+v found=%v", viewportWidth, viewportHeight, geometry, close, closeFound)
	}
	h.tt.ClickAt(closeX, closeY)
	h.tt.Frame()
	if h.a.regexPlayground != nil {
		t.Fatal("close control was not reachable after the small-window resize")
	}
}

func TestRegexToolPersistenceDoesNotReopenPlayground(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nimport \"regexp\"\nvar re = regexp.MustCompile(`word`)\n"})
	cfg := &config.Config{ConfirmClose: true}
	h := newHarnessWithConfig(t, root, cfg)
	d := h.open("a.go")
	if d == nil {
		t.Fatal("source document did not open")
	}
	st := h.a.focus.eds[d.path]
	st.GoTo(st.Buffer.Pos(strings.Index(d.buf.String(), "word")))
	h.a.openRegexPlaygroundFor(h.a.focus, d, st)
	h.tt.Frame()
	p := h.a.regexPlayground
	if p == nil {
		t.Fatal("regex playground did not open")
	}
	p.sample.Buffer.Set("private sample text")
	rect := h.a.regexToolWindow.rect
	h.a.captureSession()
	saved := cfg.Workspaces[h.a.root]
	if saved.RegexToolX != rect.X || saved.RegexToolY != rect.Y || saved.RegexToolWidth != rect.W || saved.RegexToolHeight != rect.H {
		t.Fatalf("workspace session did not capture editor-window geometry: %+v", saved)
	}
	h.a.closeRegexPlayground(p)
	h.a.closeWorkspace()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private sample text") {
		t.Fatal("sample text was persisted in workspace config")
	}
	var restored config.Config
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	h2 := newHarnessWithConfig(t, root, &restored)
	if h2.a.regexPlayground != nil {
		t.Fatal("restore reopened the regex tool")
	}
	state := h2.a.regexToolWindow
	if !state.initialized || state.rect != rect {
		t.Fatalf("workspace geometry did not restore: %+v", state)
	}
	d2 := h2.open("a.go")
	st2 := h2.a.focus.eds[d2.path]
	st2.GoTo(st2.Buffer.Pos(strings.Index(d2.buf.String(), "word")))
	h2.a.openRegexPlaygroundFor(h2.a.focus, d2, st2)
	h2.tt.Frame()
	if h2.a.regexPlayground == nil || h2.a.regexPlayground.sample.Buffer.String() != "" || h2.a.regexToolWindow.rect != state.rect {
		t.Fatalf("reopening tool did not reuse geometry without sample text: state=%+v playground=%+v", h2.a.regexToolWindow, h2.a.regexPlayground)
	}
}

func TestRegexToolGeometryRestoresWithoutSavedEditorLayout(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	cfg := &config.Config{ConfirmClose: true, Workspaces: map[string]config.Workspace{
		root: {
			RegexToolX: 42, RegexToolY: 53, RegexToolWidth: 610, RegexToolHeight: 380,
		},
	}}
	h := newHarnessWithConfig(t, root, cfg)
	state := h.a.regexToolWindow
	if h.a.regexPlayground != nil || !state.initialized || state.rect != (ui.Rect{X: 42, Y: 53, W: 610, H: 380}) {
		t.Fatalf("geometry did not restore without a saved editor layout: %+v", state)
	}
}

func workspacePath(a *App, rel string) string { return filepath.Join(a.root, rel) }

func TestRegexToolWorkspaceSwitchRestoresOwnGeometry(t *testing.T) {
	rootA := writeTree(t, map[string]string{"a.go": "package a\n"})
	rootB := writeTree(t, map[string]string{"b.go": "package b\n"})
	cfg := &config.Config{ConfirmClose: true}
	h := newHarnessWithConfig(t, rootA, cfg)
	h.a.regexToolWindow.rect = ui.Rect{X: 70, Y: 80, W: 600, H: 400}
	h.a.regexToolWindow.initialized = true
	h.a.openWorkspace(rootB)
	if h.a.regexPlayground != nil || h.a.regexToolWindow.initialized {
		t.Fatal("workspace switch carried active tool state into a new workspace")
	}
	h.a.regexToolWindow.rect = ui.Rect{X: 25, Y: 35, W: 510, H: 310}
	h.a.regexToolWindow.initialized = true
	h.a.openWorkspace(rootA)
	if !h.a.regexToolWindow.initialized || h.a.regexToolWindow.rect != (ui.Rect{X: 70, Y: 80, W: 600, H: 400}) {
		t.Fatalf("workspace-specific geometry did not restore: %+v", h.a.regexToolWindow)
	}
}

func TestRegexToolEscapeAndSourceClosureAreScoped(t *testing.T) {
	h, _, p := openRegexToolHarness(t)
	title, ok := h.tt.Find(locales.RegexToolTitlebar())
	if !ok {
		t.Fatal("tool titlebar not found")
	}
	dragTester(t, h, title, 50, 100)
	sourceGroup := p.group
	h.a.splitActive(sideRight)
	otherGroup := h.a.focus
	h.open("b.go")
	other := otherGroup.eds[workspacePath(h.a, "b.go")]
	if other == nil {
		t.Fatal("other split editor did not open")
	}
	otherDoc := h.a.docs[workspacePath(h.a, "b.go")]
	tool, ok := h.tt.Find(locales.RegexToolWindow())
	if !ok {
		t.Fatal("tool did not remain in its initiating editor")
	}
	if sourceGroup == otherGroup {
		t.Fatal("split editor did not become distinct")
	}
	otherBounds := regexToolGroupBounds(t, h, otherGroup)
	clickX, clickY := otherBounds.X+otherBounds.W/2, otherBounds.Y+otherBounds.H/2
	if clickX >= tool.X && clickX < tool.X+tool.W && clickY >= tool.Y && clickY < tool.Y+tool.H {
		clickY = min(clickY, tool.Y-8)
	}
	if clickX < otherBounds.X || clickX >= otherBounds.X+otherBounds.W || clickY < otherBounds.Y || clickY >= otherBounds.Y+otherBounds.H ||
		clickX >= tool.X && clickX < tool.X+tool.W && clickY >= tool.Y && clickY < tool.Y+tool.H {
		t.Fatalf("could not find an uncovered editor point: point=(%g,%g) pane=%+v tool=%+v", clickX, clickY, otherBounds, tool)
	}
	h.tt.ClickAt(clickX, clickY)
	h.tt.Type("X")
	h.tt.Frame()
	if !strings.Contains(otherDoc.buf.String(), "X") {
		t.Fatal("typing outside the tool did not reach the other editor")
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.a.regexPlayground != p {
		t.Fatal("Escape outside the tool closed it")
	}
	h.a.quick.open = true
	h.tt.Frame()
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.a.regexPlayground != p {
		t.Fatal("Escape in the command palette closed the tool")
	}
	h.a.quick.open = false
	h.tt.Frame()
	sample, ok := h.tt.Find(locales.RegexSample())
	if !ok {
		t.Fatal("sample editor label is missing")
	}
	h.tt.ClickAt(sample.X+10, sample.Y+22)
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.a.regexPlayground != nil {
		t.Fatal("Escape within the tool did not close it")
	}
}

func TestRegexToolSourceNavigationCanRefocusSourceWhileSampleFocused(t *testing.T) {
	h, d, p := openRegexToolHarness(t)
	h.tt.Type("sample text")
	h.tt.Frame()
	sampleBefore := p.sample.Buffer.String()
	if sampleBefore != "sample text" {
		t.Fatalf("sample editor was not focused initially: %q", sampleBefore)
	}

	h.a.userOpen(d.path)
	h.tt.Frame()
	h.tt.Type("source edit")
	h.tt.Frame()
	if !strings.Contains(d.buf.String(), "source edit") {
		t.Fatal("source navigation did not return keyboard focus to the source editor")
	}
	if p.sample.Buffer.String() != sampleBefore {
		t.Fatalf("typing in the source changed the sample: %q", p.sample.Buffer.String())
	}
	if h.a.regexPlayground != p || !h.tt.HasText(locales.RegexToolWindow()) {
		t.Fatal("source navigation closed the modeless regex tool")
	}
}

func TestRegexToolCloseDoesNotRestoreClosedSourceGroup(t *testing.T) {
	h, d, p := openRegexToolHarness(t)
	sourceGroup := p.group
	h.a.splitActive(sideRight)
	otherGroup := h.a.focus
	h.open("b.go")
	other := otherGroup.eds[workspacePath(h.a, "b.go")]
	if other == nil {
		t.Fatal("other group did not open its editor")
	}
	h.a.closeTab(sourceGroup, d.path)
	if h.a.layout.find(sourceGroup) != nil {
		t.Fatal("closing the source tab did not collapse its group")
	}
	if h.a.regexPlayground != nil {
		t.Fatal("closing the source editor left its playground alive")
	}
	if h.a.focus != otherGroup || h.a.focusEditor != other {
		t.Fatalf("closing tool restored a stale source group: focus=%p editor=%p", h.a.focus, h.a.focusEditor)
	}
}

func TestRegexToolClosesWhenItsSourceTabCloses(t *testing.T) {
	h, d, p := openRegexToolHarness(t)
	group := p.group
	h.open("b.go")
	other := group.eds[workspacePath(h.a, "b.go")]
	h.a.closeTab(group, d.path)
	if h.a.layout.find(group) == nil {
		t.Fatal("closing the source tab unexpectedly removed its group")
	}
	if h.a.regexPlayground != nil || h.a.focusEditor != other {
		t.Fatalf("closing the source tab left stale playground focus: playground=%p editor=%p", h.a.regexPlayground, h.a.focusEditor)
	}
}

func TestRegexToolEmptyPatternCollapsesDiagram(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\n"}))
	d := h.open("a.go")
	st := h.a.focus.eds[d.path]
	h.a.openRegexPlaygroundFor(h.a.focus, d, st)
	h.tt.Frame()
	if h.a.regexPlayground.pattern != "" || h.tt.HasText(locales.RegexDiagramTitle()) || h.tt.HasText(locales.RegexDiagramUnavailable()) {
		t.Fatal("empty pattern reserved the railroad structure area")
	}
}
