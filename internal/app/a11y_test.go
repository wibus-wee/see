package app

import (
	"os"
	"path/filepath"
	"testing"
	"unicode"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"see/locales"
)

// TestAccessibleNamesFollowTheLocale checks that the names and tips of the
// controls follow the app's language: Chinese with Chinese, not the
// English the code is written in.
func TestAccessibleNamesFollowTheLocale(t *testing.T) {
	i18n.SetLocaleFunc(func() string { return "zh-Hans" })
	defer i18n.SetLocaleFunc(nil)

	root := writeTree(t, map[string]string{
		"文件.go": "package a\n\nfunc A() {}\n",
		"说明.md": "# 你好\n",
	})
	h := newHarness(t, root)
	h.tt.Frame()
	check := func(view string, tt *ui.Tester, role ui.Role, name string) {
		t.Helper()
		if name == "" || !hasHan(name) {
			t.Fatalf("%s: %q is not translated", view, name)
		}
		if _, ok := tt.AccessByRole(role, name); !ok {
			t.Fatalf("%s: no %v named %q: %v", view, role, name, tt.AccessAll())
		}
	}
	for _, c := range []struct {
		role ui.Role
		name string
	}{
		{ui.RoleToggleButton, locales.MsgNavigator()},
		{ui.RoleToggleButton, locales.Terminal()},
		{ui.RoleRadio, problemText("problemsList")},
		{ui.RoleRadio, outlineText("outlineTitle")},
		{ui.RoleRadio, bookmarkText("bookmarksTitle")},
		{ui.RoleButton, locales.MsgBack()},
		{ui.RoleButton, locales.MsgForward()},
		{ui.RoleButton, commandLabel("editorGroup.splitRight")},
		{ui.RoleButton, commandLabel("editorGroup.splitDown")},
		{ui.RoleButton, locales.MsgOpenQuickly()},
		{ui.RoleButton, commandLabel("file.new")},
		{ui.RoleButton, locales.MsgNewFolder()},
		{ui.RoleButton, locales.MsgCollapseAllFolders()},
		{ui.RoleButton, locales.MsgRefresh()},
	} {
		check("main", h.tt, c.role, c.name)
	}

	h.a.settings.pane = "editor"
	settings := ui.NewTester(h.a.settingsView, 900, 680)
	settings.Frame()
	for _, name := range []string{locales.MsgBack(), locales.MsgForward()} {
		check("settings", settings, ui.RoleButton, name)
	}
	if _, ok := settings.AccessByRole(ui.RoleTextField, locales.PageGuideColumn()); !ok {
		t.Errorf("settings: no Page Guide field named %q: %v", locales.PageGuideColumn(), settings.AccessAll())
	}

	h.a.openQuickPanel("")
	h.tt.Frame()
	if _, ok := h.tt.AccessByRole(ui.RoleTextField, locales.MsgOpenQuickly()); !ok {
		t.Errorf("quick open has no field named %q: %v", locales.MsgOpenQuickly(), h.tt.AccessAll())
	}
	h.a.quick.open = false
}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// TestGitControlsFollowTheLocale checks the Git navigator's buttons, which
// name actions rather than show text, in the app's language.
func TestGitControlsFollowTheLocale(t *testing.T) {
	i18n.SetLocaleFunc(func() string { return "zh-Hans" })
	defer i18n.SetLocaleFunc(nil)

	root := gitRepo(t, map[string]string{"文件.txt": "base\n"})
	path := filepath.Join(root, "文件.txt")
	if err := os.WriteFile(path, []byte("staged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "文件.txt")
	if err := os.WriteFile(path, []byte("unstaged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	h.readGit()
	h.a.sidebarOff, h.a.navigator = false, navGit
	h.tt.Frame()
	for _, name := range []string{
		locales.MsgStageAllChanges(),
		locales.MsgUnstageAllChanges(),
		locales.MsgStashChanges(),
		locales.MsgRefresh(),
	} {
		if _, ok := h.tt.AccessByRole(ui.RoleButton, name); !ok {
			t.Errorf("Git navigator has no button named %q: %v", name, h.tt.AccessAll())
		}
	}
}

// TestInteractiveElementsHaveNames checks each view the app shows for
// controls a pointer can press that tell assistive technology nothing:
// every button, link, field, choice and row names itself, and the test's
// view of each is the one screen readers get.
func TestInteractiveElementsHaveNames(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go":      "package a\n\nfunc A() {}\n",
		"README.md": "# hi\n",
	})
	h := newHarness(t, root)
	audit := func(view string, tt *ui.Tester) {
		tt.Frame()
		for _, a := range tt.AccessAll() {
			if !interactiveRole(a) || a.Name != "" {
				continue
			}
			t.Errorf("%s: %v at %v has no accessible name", view, a.Role, a.Bounds)
		}
	}
	audit("main", h.tt)
	h.open("a.go")
	audit("editor", h.tt)
	for _, nav := range []struct {
		name string
		id   int
	}{
		{"find", navFind}, {"git", navGit}, {"problems", navProblems},
		{"outline", navOutline}, {"bookmarks", navBookmarks},
	} {
		h.a.sidebarOff, h.a.navigator = false, nav.id
		audit(nav.name, h.tt)
	}
	// The general pane asks the system to open the app at login, which
	// waits on the app's loop: the harness has no loop to answer.
	for _, pane := range []string{"editor", "terminal", "notifications", "intelligence", "advanced", "search", "shortcuts"} {
		h.a.settings.pane = pane
		audit("settings/"+pane, ui.NewTester(h.a.settingsView, 900, 680))
	}
	audit("welcome", ui.NewTester(h.a.welcomeView, 800, 600))
	views := map[string]func(*ui.Context){
		"inspector":  h.a.inspectorView,
		"references": h.a.referencesView,
		"tasks":      h.a.taskView,
		"log":        h.a.logView,
	}
	for name, view := range views {
		audit(name, ui.NewTester(view, 600, 500))
	}
	h.a.openQuickPanel("")
	audit("quick", h.tt)
	h.a.quick.open = false
	h.a.openPalette()
	audit("palette", h.tt)
	h.a.quick.open = false
	if b, ok := h.tt.AccessByRole(ui.RoleToggleButton, locales.MsgNavigator()); !ok || !b.Checked {
		t.Errorf("the toolbar's navigator button is not a toggle button pressed: %+v", b)
	}
}

// interactiveRole tells whether assistive technology expects the role to
// name itself: a control to press or choose, a field, or a row of a list.
func interactiveRole(a ui.AccessInfo) bool {
	switch a.Role {
	case ui.RoleButton, ui.RoleLink, ui.RoleToggleButton, ui.RoleRadio,
		ui.RoleTab, ui.RoleMenuItem, ui.RoleMenuItemCheckBox, ui.RoleMenuItemRadio,
		ui.RoleTextField, ui.RoleComboBox, ui.RoleCheckBox, ui.RoleSwitch,
		ui.RoleSlider, ui.RoleStepper, ui.RoleTreeItem, ui.RoleListItem,
		ui.RoleRow, ui.RoleCell, ui.RoleColumnHeader, ui.RoleRowHeader:
		return true
	}
	return a.Focusable
}
