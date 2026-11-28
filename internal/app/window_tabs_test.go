package app

import (
	"testing"

	"see/internal/config"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui/editor"
	"see/locales"
)

type fakeProjectTabWindow struct {
	handle    uintptr
	destroyed bool
	updates   int
	vibrancy  []mygo.Vibrancy
}

func (w *fakeProjectTabWindow) Show()                 {}
func (w *fakeProjectTabWindow) Focus()                {}
func (w *fakeProjectTabWindow) Update(func())         { w.updates++ }
func (w *fakeProjectTabWindow) IsDestroyed() bool     { return w.destroyed }
func (w *fakeProjectTabWindow) NativeHandle() uintptr { return w.handle }
func (w *fakeProjectTabWindow) SetVibrancy(v mygo.Vibrancy) {
	w.vibrancy = append(w.vibrancy, v)
}

type fakeProjectWindowTabGroup struct {
	groups   map[uintptr][]uintptr
	added    [][2]uintptr
	selected []uintptr
}

func (g *fakeProjectWindowTabGroup) windowsInTabGroup(window uintptr) []uintptr {
	return append([]uintptr(nil), g.groups[window]...)
}

func (g *fakeProjectWindowTabGroup) addWindowToTabGroup(anchor, window uintptr) {
	g.added = append(g.added, [2]uintptr{anchor, window})
	g.groups[anchor] = append(g.groups[anchor], window)
}

func (g *fakeProjectWindowTabGroup) selectWindow(window uintptr) {
	g.selected = append(g.selected, window)
}

func TestProjectWindowMenuMergesAndKeepsNativeActions(t *testing.T) {
	merged := false
	menu := projectWindowMenu(true, true, func() { merged = true })
	if menu.Role != mygo.RoleWindowMenu {
		t.Fatalf("Window menu role = %q", menu.Role)
	}
	if len(menu.Submenu) != 5 {
		t.Fatalf("Window menu items = %d, want 5", len(menu.Submenu))
	}
	if menu.Submenu[0].Role != mygo.RoleMinimize || menu.Submenu[1].Role != mygo.RoleZoom || menu.Submenu[4].Role != mygo.RoleFront {
		t.Fatalf("native Window menu actions were not preserved: %+v", menu.Submenu)
	}
	merge := menu.Submenu[3]
	if merge.Label != locales.Catalog.Format(i18n.Locale(), "msgMergeAllWindows", nil) || merge.Label == "" || merge.Disabled {
		t.Fatalf("Merge All Windows item = %+v", merge)
	}
	for locale, want := range map[string]string{"en": "Merge All Windows", "zh-Hans": "合并所有窗口"} {
		if got := locales.Catalog.Format(locale, "msgMergeAllWindows", nil); got != want {
			t.Errorf("%s Merge All Windows = %q, want %q", locale, got, want)
		}
	}
	merge.Click(merge, nil)
	if !merged {
		t.Fatal("choosing Merge All Windows did not invoke the merge")
	}
}

func TestProjectWindowMenuDisablesMergeWhenUnavailable(t *testing.T) {
	called := false
	menu := projectWindowMenu(true, false, func() { called = true })
	merge := menu.Submenu[3]
	if !merge.Disabled {
		t.Fatal("Merge All Windows was enabled when merging was unavailable")
	}
	merge.Click(merge, nil)
	if called {
		t.Fatal("disabled Merge All Windows invoked the merge")
	}
}

func TestProjectWindowMenuUsesDefaultOutsideMacOS(t *testing.T) {
	menu := projectWindowMenu(false, true, func() { t.Fatal("non-macOS menu invoked Merge All Windows") })
	if menu.Role != mygo.RoleWindowMenu || menu.Submenu != nil {
		t.Fatalf("non-macOS Window menu = %+v", menu)
	}
}

func TestProjectWindowHandlesExcludeEmptyDestroyedAndDuplicateWindows(t *testing.T) {
	primary := &projectWindow{app: &App{}, win: &fakeProjectTabWindow{handle: 1}}
	projectA := &projectWindow{app: &App{root: "/a"}, win: &fakeProjectTabWindow{handle: 2}}
	projectB := &projectWindow{app: &App{root: "/b"}, win: &fakeProjectTabWindow{handle: 3}}
	emptyWelcome := &projectWindow{app: &App{showWelcome: true}, win: &fakeProjectTabWindow{handle: 4}}
	documentOnly := &projectWindow{app: &App{docs: map[string]*doc{"/file.txt": {path: "/file.txt"}}}, win: &fakeProjectTabWindow{handle: 5}}
	tabOnly := &projectWindow{app: &App{layout: &layout{g: &group{order: []string{"diff"}}}}, win: &fakeProjectTabWindow{handle: 6}}
	destroyed := &projectWindow{app: &App{root: "/closed"}, win: &fakeProjectTabWindow{handle: 7, destroyed: true}}
	duplicate := &projectWindow{app: &App{root: "/duplicate"}, win: &fakeProjectTabWindow{handle: 2}}
	withoutNativeHandle := &projectWindow{app: &App{root: "/unsupported"}, win: fakeProjectWindowWithoutHandle{}}

	got := projectWindowHandles([]*projectWindow{nil, primary, projectA, projectB, emptyWelcome, documentOnly, tabOnly, destroyed, duplicate, withoutNativeHandle})
	want := []uintptr{2, 3, 5, 6}
	if len(got) != len(want) {
		t.Fatalf("project window handles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("project window handles = %v, want %v", got, want)
		}
	}
}

type fakeProjectWindowWithoutHandle struct{}

func (fakeProjectWindowWithoutHandle) Show()                     {}
func (fakeProjectWindowWithoutHandle) Focus()                    {}
func (fakeProjectWindowWithoutHandle) Update(func())             {}
func (fakeProjectWindowWithoutHandle) IsDestroyed() bool         { return false }
func (fakeProjectWindowWithoutHandle) SetVibrancy(mygo.Vibrancy) {}

func TestProjectWindowMergeUsesActiveAnchorAndPreservesDirtyBuffer(t *testing.T) {
	buffer := editor.NewBuffer("saved")
	buffer.Set("unsaved edit")
	if !buffer.Modified() {
		t.Fatal("test buffer is not dirty")
	}
	dirtyDoc := &doc{path: "/b/file.txt", buf: buffer}
	appA := &App{root: "/a"}
	appB := &App{root: "/b", docs: map[string]*doc{dirtyDoc.path: dirtyDoc}}
	projectA := &projectWindow{app: appA, win: &fakeProjectTabWindow{handle: 10}}
	projectB := &projectWindow{app: appB, win: &fakeProjectTabWindow{handle: 20}}
	beforeText, beforeVersion := buffer.Snapshot().String(), buffer.Version()
	beforeDoc, beforeBuffer := appB.docs[dirtyDoc.path], dirtyDoc.buf
	windows := projectWindowHandles([]*projectWindow{projectA, projectB})
	tabs := &fakeProjectWindowTabGroup{groups: map[uintptr][]uintptr{}}

	if !canMergeWindowHandles(windows, projectWindowHandle(projectB), tabs) {
		t.Fatal("separate project windows could not be merged")
	}
	if !mergeWindowHandles(windows, projectWindowHandle(projectB), tabs) {
		t.Fatal("project windows were not merged")
	}
	if len(tabs.added) != 1 || tabs.added[0] != [2]uintptr{20, 10} {
		t.Fatalf("tab additions = %v, want active project 20 to anchor project 10", tabs.added)
	}
	if len(tabs.selected) != 1 || tabs.selected[0] != 20 {
		t.Fatalf("selected windows = %v, want active project 20", tabs.selected)
	}
	if appB.docs[dirtyDoc.path] != beforeDoc || dirtyDoc.buf != beforeBuffer || !buffer.Modified() || buffer.Snapshot().String() != beforeText || buffer.Version() != beforeVersion {
		t.Fatal("merging windows changed project state or its unsaved buffer")
	}
}

func TestProjectWindowMergeSkipsExistingTabsAndIsIdempotent(t *testing.T) {
	tabs := &fakeProjectWindowTabGroup{groups: map[uintptr][]uintptr{2: {1, 2}}}
	windows := []uintptr{1, 2}
	if canMergeWindowHandles(windows, 2, tabs) {
		t.Fatal("Merge All Windows was enabled for a single existing tab group")
	}
	if mergeWindowHandles(windows, 2, tabs) {
		t.Fatal("already-grouped project windows were merged again")
	}
	if canMergeWindowHandles(windows, 2, tabs) {
		t.Fatal("Merge All Windows remained enabled for the shared tab group")
	}
	if len(tabs.added) != 0 || len(tabs.selected) != 0 {
		t.Fatalf("existing tab group was modified: added=%v selected=%v", tabs.added, tabs.selected)
	}
	tabs.groups[2] = []uintptr{2}
	if !canMergeWindowHandles(windows, 2, tabs) {
		t.Fatal("Merge All Windows did not become available after a tab was detached")
	}
}

func TestApplyVibrancyToProjectsUpdatesEveryOpenWindow(t *testing.T) {
	first := &fakeProjectTabWindow{handle: 1}
	second := &fakeProjectTabWindow{handle: 2}
	destroyed := &fakeProjectTabWindow{handle: 3, destroyed: true}
	projects := []*projectWindow{{app: &App{}, win: first}, {app: &App{}, win: second}, {app: &App{}, win: destroyed}}

	cfg := config.Load("")
	cfg.SetBetaEnabled(config.BetaSidebarVibrancy, true)
	if got := projectWindowVibrancy(cfg); got != mygo.VibrancySidebar {
		t.Fatalf("default beta material = %q", got)
	}
	cfg.SetBetaSetting(config.BetaSidebarVibrancy, "material", "glass")
	if got := projectWindowVibrancy(cfg); got != mygo.VibrancyGlass {
		t.Fatalf("configured beta material = %q", got)
	}
	cfg.SetBetaSetting(config.BetaSidebarVibrancy, "material", "sidebar")
	applyVibrancyToProjects(projects, cfg)
	for i, window := range []*fakeProjectTabWindow{first, second} {
		if len(window.vibrancy) != 1 || window.vibrancy[0] != mygo.VibrancySidebar || window.updates != 1 {
			t.Fatalf("window %d vibrancy=%v updates=%d", i, window.vibrancy, window.updates)
		}
	}
	if len(destroyed.vibrancy) != 0 || destroyed.updates != 0 {
		t.Fatalf("destroyed window vibrancy=%v updates=%d", destroyed.vibrancy, destroyed.updates)
	}

	cfg.SetBetaEnabled(config.BetaSidebarVibrancy, false)
	applyVibrancyToProjects(projects, cfg)
	if got := first.vibrancy[1]; got != mygo.VibrancyNone {
		t.Fatalf("disabled material = %q, want none", got)
	}
}
