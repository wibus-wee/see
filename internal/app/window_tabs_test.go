package app

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"see/locales"
)

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
