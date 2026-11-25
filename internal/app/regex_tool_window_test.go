package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if h.a.regexPlayground == nil || !h.tt.HasText(locales.RegexToolWindow()) {
		t.Fatal("workspace regex tool did not open")
	}
	return h, d, h.a.regexPlayground
}

func dragTester(t *testing.T, h *harness, bounds ui.Rect, dx, dy float32) {
	t.Helper()
	x, y := bounds.X+bounds.W/2, bounds.Y+bounds.H/2
	h.tt.Press(x, y)
	h.tt.Move(x+dx, y+dy)
	h.tt.Release(x+dx, y+dy)
	h.tt.Frame()
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
		{locales.RegexToolResizeNorth(), ui.CursorResizeNS, 0, -8, false, true},
		{locales.RegexToolResizeSouth(), ui.CursorResizeNS, 0, 8, false, true},
		{locales.RegexToolResizeWest(), ui.CursorResizeEW, -8, 0, true, false},
		{locales.RegexToolResizeEast(), ui.CursorResizeEW, 8, 0, true, false},
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
			h.tt.Move(x, y)
			if got := h.tt.Cursor(); got != test.cursor {
				t.Fatalf("cursor at %s = %v, want %v", test.label, got, test.cursor)
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

func TestRegexToolWindowClampsToSmallEditorWorkspace(t *testing.T) {
	h, _, _ := openRegexToolHarness(t)
	h.tt.SetSize(500, 320)
	h.tt.Frame()
	window, ok := h.tt.Find(locales.RegexToolWindow())
	if !ok || window.W <= 0 || window.X+window.W > 500 || window.Y+window.H > 320 {
		t.Fatalf("regex tool is not bounded to the small window: %+v found=%v", window, ok)
	}
	if h.a.regexToolWindow.rect.W >= regexToolMinWidth || h.a.regexToolWindow.rect.H >= regexToolMinHeight {
		t.Fatalf("small workspace did not shrink effective minimum: %+v", h.a.regexToolWindow.rect)
	}
}

func TestRegexToolWindowClampsWhenSidebarExpands(t *testing.T) {
	h, _, _ := openRegexToolHarness(t)
	h.a.regexToolWindow.rect = ui.Rect{X: 400, Y: 80, W: 680, H: 420}
	h.a.regexToolWindow.initialized = true
	h.a.sidebarW = 460
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()
	if right := h.a.regexToolWindow.rect.X + h.a.regexToolWindow.rect.W; right > 1100-460-regexToolMargin+1 {
		t.Fatalf("floating tool escaped the editor workspace after sidebar expansion: %+v", h.a.regexToolWindow.rect)
	}
}

func TestRegexToolDockDividerAndUndockPreservePlayground(t *testing.T) {
	h, _, p := openRegexToolHarness(t)
	p.sample.Buffer.Set("kept sample")
	h.tt.Frame()
	if err := h.tt.Click(locales.RegexToolDock()); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if !h.a.regexToolWindow.docked || h.a.regexPlayground != p || p.sample.Buffer.String() != "kept sample" {
		t.Fatal("docking changed playground state")
	}
	divider, ok := h.tt.Find(locales.RegexToolDockDivider())
	if !ok {
		t.Fatal("dock divider is missing")
	}
	before := h.a.regexToolWindow.dockHeight
	dragTester(t, h, divider, 0, 24)
	if got := h.a.regexToolWindow.dockHeight; got != before-24 {
		t.Fatalf("dock height after divider drag = %v, want %v", got, before-24)
	}
	if err := h.tt.Click(locales.RegexToolUndock()); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.a.regexToolWindow.docked || h.a.regexPlayground != p || p.sample.Buffer.String() != "kept sample" {
		t.Fatal("undocking changed playground state")
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
	p := h.a.regexPlayground
	if p == nil {
		t.Fatal("regex playground did not open")
	}
	p.sample.Buffer.Set("private sample text")
	h.a.regexToolWindow.rect = ui.Rect{X: 31, Y: 47, W: 620, H: 390}
	h.a.regexToolWindow.initialized = true
	h.a.regexToolWindow.docked = true
	h.a.regexToolWindow.dockHeight = 300
	h.a.captureSession()
	saved := cfg.Workspaces[h.a.root]
	if saved.RegexToolX != 31 || saved.RegexToolY != 47 || saved.RegexToolWidth != 620 || saved.RegexToolHeight != 390 || !saved.RegexToolDocked || saved.RegexToolDockHeight != 300 {
		t.Fatalf("workspace session did not capture independent geometry: %+v", saved)
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
	if !state.initialized || state.rect != (ui.Rect{X: 31, Y: 47, W: 620, H: 390}) || !state.docked || state.dockHeight != 300 {
		t.Fatalf("workspace geometry did not restore: %+v", state)
	}
	d2 := h2.open("a.go")
	st2 := h2.a.focus.eds[d2.path]
	st2.GoTo(st2.Buffer.Pos(strings.Index(d2.buf.String(), "word")))
	h2.a.openRegexPlaygroundFor(h2.a.focus, d2, st2)
	h2.tt.Frame()
	if h2.a.regexPlayground == nil || h2.a.regexPlayground.sample.Buffer.String() != "" || h2.a.regexToolWindow.rect != state.rect || !h2.a.regexToolWindow.docked {
		t.Fatalf("reopening tool did not reuse geometry without sample text: state=%+v playground=%+v", h2.a.regexToolWindow, h2.a.regexPlayground)
	}
}

func TestRegexToolGeometryRestoresWithoutSavedEditorLayout(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	cfg := &config.Config{ConfirmClose: true, Workspaces: map[string]config.Workspace{
		root: {
			RegexToolX: 42, RegexToolY: 53, RegexToolWidth: 610, RegexToolHeight: 380,
			RegexToolDocked: true, RegexToolDockHeight: 275,
		},
	}}
	h := newHarnessWithConfig(t, root, cfg)
	state := h.a.regexToolWindow
	if h.a.regexPlayground != nil || !state.initialized || state.rect != (ui.Rect{X: 42, Y: 53, W: 610, H: 380}) || !state.docked || state.dockHeight != 275 {
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
	h.a.regexToolWindow.docked = true
	h.a.regexToolWindow.dockHeight = 280
	h.a.openWorkspace(rootB)
	if h.a.regexPlayground != nil || h.a.regexToolWindow.initialized || h.a.regexToolWindow.docked {
		t.Fatal("workspace switch carried active tool state into a new workspace")
	}
	h.a.regexToolWindow.rect = ui.Rect{X: 25, Y: 35, W: 510, H: 310}
	h.a.regexToolWindow.initialized = true
	h.a.regexToolWindow.docked = false
	h.a.regexToolWindow.dockHeight = 220
	h.a.openWorkspace(rootA)
	if !h.a.regexToolWindow.initialized || h.a.regexToolWindow.rect != (ui.Rect{X: 70, Y: 80, W: 600, H: 400}) || !h.a.regexToolWindow.docked || h.a.regexToolWindow.dockHeight != 280 {
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
	group := p.group
	h.open("b.go")
	other := group.eds[workspacePath(h.a, "b.go")]
	if other == nil {
		t.Fatal("second editor did not open")
	}
	otherDoc := h.a.docs[workspacePath(h.a, "b.go")]
	tool, ok := h.tt.Find(locales.RegexToolWindow())
	if !ok {
		t.Fatal("floating tool not found")
	}
	h.tt.ClickAt(tool.X-20, tool.Y-50)
	h.tt.Type("X")
	h.tt.Frame()
	if !strings.Contains(otherDoc.buf.String(), "X") {
		t.Fatal("typing outside the tool did not reach the editor")
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
	h.a.closeRegexPlayground(p)
	if h.a.focus != otherGroup || h.a.focusEditor != other {
		t.Fatalf("closing tool restored a stale source group: focus=%p editor=%p", h.a.focus, h.a.focusEditor)
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
