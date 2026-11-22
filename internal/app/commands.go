package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"see/internal/tasks"
	"see/locales"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type command struct {
	name, mods, key string
	shortcutMods    ui.Modifiers
	shortcutKey     ui.Key
	run             func(*App)
}

var platformCommands = commandsFor(runtime.GOOS)

func commands() []command { return platformCommands }

func commandsFor(goos string) []command {
	backMods, forwardMods := "Ctrl+", "Ctrl+Shift+"
	if goos != "darwin" {
		backMods, forwardMods = "Ctrl+Alt+", "Ctrl+Alt+Shift+"
	}
	replaceLabel, replaceKeyName, replaceMods, replaceKey := fileReplaceShortcutFor(goos)
	docItemsLabel, docItemsKeyName, docItemsMods, docItemsKey, groupFilesLabel, groupFilesKeyName, groupFilesMods, groupFilesKey := jumpBarShortcutsFor(goos)
	scopeLabel, scopeKeyName, scopeMods, scopeKey := editAllScopeShortcut(goos)
	counterpartLabel, counterpartMods := counterpartShortcut(goos)
	methodLabel, methodMods := methodShortcut(goos)
	maximizeLabel, maximizeMods := maximizeEditorShortcut(goos)
	cmds := []command{
		{"Bigger", "Cmd+", "=", ui.Cmd, ui.KeyEqual, func(a *App) { a.changeEditorFontSize(1) }},
		{"Smaller", "Cmd+", "-", ui.Cmd, ui.KeyMinus, func(a *App) { a.changeEditorFontSize(-1) }},
		{"Reset Editor Font Size", "", "", 0, 0, func(a *App) { a.setEditorFontSize(13) }},
		{"New File", "Cmd+", "N", ui.Cmd, ui.KeyN, func(a *App) { a.newFile() }},
		{"Command Palette", "Cmd+Shift+", "P", ui.Cmd | ui.Shift, ui.KeyP, func(a *App) { a.openPalette() }},
		{"New Window", "Cmd+Shift+", "N", ui.Cmd | ui.Shift, ui.KeyN, func(a *App) {
			a.host.OpenProjectWindow("")
		}},
		{"Open Folder", "Cmd+", "O", ui.Cmd, ui.KeyO, func(a *App) { a.openFolderDialog() }},
		{"Open Quickly", "Cmd+Shift+", "O", ui.Cmd | ui.Shift, ui.KeyO, func(a *App) { a.openQuickly() }},
		{"Show Problems", "Cmd+Shift+", "M", ui.Cmd | ui.Shift, ui.KeyM, (*App).showProblems},
		{"Show Quick Help", "Alt+Cmd+", "K", ui.Alt | ui.Cmd, ui.KeyK, func(a *App) { a.showQuickHelpAtCaret() }},
		{"Test Regex", "", "", 0, 0, func(a *App) { a.openRegexPlayground() }},
		{"Toggle Quick Help Inspector", "Alt+Cmd+", "I", ui.Alt | ui.Cmd, ui.KeyI, func(a *App) { a.toggleInspector() }},
		{"Next Problem", "", "F8", 0, ui.KeyF8, func(a *App) { a.goToProblem(1) }},
		{"Previous Problem", "Shift+", "F8", ui.Shift, ui.KeyF8, func(a *App) { a.goToProblem(-1) }},
		{"Go to Line", "Ctrl+", "G", ui.Ctrl, ui.KeyG, func(a *App) {
			if a.activeEditor() != nil {
				a.openQuickPanel(":")
			}
		}},
		{"Reveal in Project Navigator", "Cmd+Shift+", "J", ui.Cmd | ui.Shift, ui.KeyJ, func(a *App) {
			a.revealInProjectNavigator(a.activeProjectFile())
		}},
		{"Search File Symbols", "Alt+Cmd+", "O", ui.Alt | ui.Cmd, ui.KeyO, func(a *App) { a.openQuickPanel("@") }},
		{"Search Workspace Symbols", "Cmd+", "T", ui.Cmd, ui.KeyT, func(a *App) { a.openQuickPanel("#") }},
		{"Open Markdown Preview", "Cmd+Shift+", "V", ui.Cmd | ui.Shift, ui.KeyV, func(a *App) { a.openMarkdownTab(a.focus) }},
		{"Find in Workspace", "Cmd+Shift+", "F", ui.Cmd | ui.Shift, ui.KeyF, func(a *App) {
			a.sidebarOff, a.navigator, a.find.focus = false, navFind, true
		}},
		{"Replace in Workspace", "Cmd+Shift+", "H", ui.Cmd | ui.Shift, ui.KeyH, func(a *App) {
			a.sidebarOff, a.navigator, a.find.focus, a.find.showReplace = false, navFind, true, true
		}},
		{"Show Outline", "", "", 0, 0, func(a *App) { a.sidebarOff, a.navigator = false, navOutline }},
		{"Show Bookmarks", "", "", 0, 0, func(a *App) { a.sidebarOff, a.navigator = false, navBookmarks }},
		{"Toggle Bookmark", "Alt+Cmd+", "B", ui.Alt | ui.Cmd, ui.KeyB, func(a *App) { a.toggleBookmark() }},
		{"Show Document Items", docItemsLabel, docItemsKeyName, docItemsMods, docItemsKey, func(a *App) { a.focus.jumpSymbols = true }},
		{"Show Group Files", groupFilesLabel, groupFilesKeyName, groupFilesMods, groupFilesKey, func(a *App) { a.focus.jumpFiles = true }},
		{"Next Counterpart", counterpartLabel, "↓", counterpartMods, ui.KeyDown, func(a *App) { a.switchCounterpart(1) }},
		{"Previous Counterpart", counterpartLabel, "↑", counterpartMods, ui.KeyUp, func(a *App) { a.switchCounterpart(-1) }},
		{"Show Git", "", "", 0, 0, func(a *App) { a.sidebarOff, a.navigator = false, navGit; a.refreshGit() }},
		{"Git: Show History", "", "", 0, 0, func(a *App) {
			a.sidebarOff, a.navigator, a.git.historyClosed = false, navGit, false
			a.refreshGit()
		}},
		{"Edit Search Results", "", "", 0, 0, func(a *App) { a.openSearchEdit() }},
		{"Git: Show Log", "", "", 0, 0, func(a *App) { a.openGitLog() }},
		{"Show File History", "", "", 0, 0, func(a *App) { a.showActiveFileHistory() }},
		{"Show Local History", "", "", 0, 0, func(a *App) { a.showActiveLocalHistory() }},
		{"Git: Reveal HEAD in Log", "", "", 0, 0, func(a *App) { a.revealLogRef("HEAD") }},
		{"Git: Jump to Date…", "", "", 0, 0, func(a *App) { a.openJumpToDate() }},
		{"Show Line's Commit in Git Log", "", "", 0, 0, func(a *App) { a.revealLineCommit() }},
		{"Toggle Minimap", "", "", 0, 0, func(a *App) { a.cfg.HideMinimap = !a.cfg.HideMinimap; a.settingsChanged() }},
		{"Toggle Inline Git Blame", "", "", 0, 0, func(a *App) { a.cfg.HideInlineBlame = !a.cfg.HideInlineBlame; a.settingsChanged() }},
		{"Show Invisibles", "", "", 0, 0, func(a *App) { a.toggleInvisibles() }},
		{"Toggle Type Hints", "", "", 0, 0, func(a *App) { a.toggleTypeHints() }},
		{"Toggle Inline Diff", "", "", 0, 0, func(a *App) { a.toggleDiffLayout() }},
		{"Next Change", "", "", 0, 0, func(a *App) { a.goToChange(1) }},
		{"Previous Change", "", "", 0, 0, func(a *App) { a.goToChange(-1) }},
		{"Discard Change", "", "", 0, 0, func(a *App) { a.discardChange() }},
		{"Git: Fetch", "", "", 0, 0, func(a *App) { a.gitFetch() }},
		{"Git: Pull", "", "", 0, 0, func(a *App) { a.gitPull() }},
		{"Git: Push", "", "", 0, 0, func(a *App) { a.gitPush() }},
		{"Git: Switch Branch…", "", "", 0, 0, func(a *App) { a.openBranchPicker(a.git.picker.anchor) }},
		{"Filter Files", "Alt+Cmd+", "J", ui.Alt | ui.Cmd, ui.KeyJ, func(a *App) {
			a.sidebarOff, a.navigator, a.filter.focus = false, navFiles, true
		}},
		{"Show or Hide the Navigator", "Cmd+", "B", ui.Cmd, ui.KeyB, func(a *App) { a.sidebarOff = !a.sidebarOff }},
		{"Go Back", backMods, "-", 0, ui.KeyMinus, func(a *App) { a.goBack() }},
		{"Go Forward", forwardMods, "-", 0, ui.KeyMinus, func(a *App) { a.goForward() }},
		{"Focus Next Editor", "", "F6", 0, ui.KeyF6, func(a *App) { a.cycleEditorGroup(1) }},
		{"Focus Previous Editor", "Shift+", "F6", ui.Shift, ui.KeyF6, func(a *App) { a.cycleEditorGroup(-1) }},
		{"Move Tab to Next Editor", "", "", 0, 0, func(a *App) { a.moveTabToNextEditor() }},
		{"Next Tab", "Ctrl+", "Tab", ui.Ctrl, ui.KeyTab, func(a *App) { a.cycleTab(1) }},
		{"Previous Tab", "Ctrl+Shift+", "Tab", ui.Ctrl | ui.Shift, ui.KeyTab, func(a *App) { a.cycleTab(-1) }},
		{"Save", "Cmd+", "S", ui.Cmd, ui.KeyS, func(a *App) { a.saveActive() }},
		{"Save All", "Cmd+Shift+", "S", ui.Cmd | ui.Shift, ui.KeyS, func(a *App) { a.saveAll() }},
		{"Reopen Closed Tab", "Cmd+Shift+", "T", ui.Cmd | ui.Shift, ui.KeyT, func(a *App) { a.reopenClosedTab() }},
		{"Close Tab", "Cmd+", "W", ui.Cmd, ui.KeyW, func(a *App) {
			// As Xcode: the tab, and the window once no tab is left.
			if p := a.focus.active; p != "" {
				a.closeDoc(p)
			} else {
				a.host.CloseWindow()
			}
		}},
		{"Close Other Tabs", "", "", 0, 0, func(a *App) { a.closeTabs(a.focus, a.focus.active, closeOtherTabs) }},
		{"Close Tabs to the Right", "", "", 0, 0, func(a *App) { a.closeTabs(a.focus, a.focus.active, closeRightTabs) }},
		{"Close Saved Tabs", "", "", 0, 0, func(a *App) { a.closeTabs(a.focus, a.focus.active, closeSavedTabs) }},
		{"Add Editor on Right", "Cmd+", "D", ui.Cmd, ui.KeyD, func(a *App) { a.splitActive(sideRight) }},
		{"Add Editor Below", "Cmd+Shift+", "D", ui.Cmd | ui.Shift, ui.KeyD, func(a *App) { a.splitActive(sideBottom) }},
		{"Maximize Editor", maximizeLabel, "Return", maximizeMods, ui.KeyEnter, func(a *App) { a.toggleMaximizeEditor() }},
		{"Show or Hide the Terminal", "Cmd+Shift+", "Y", ui.Cmd | ui.Shift, ui.KeyY, func(a *App) { a.toggleTerminal() }},
		{"Go: Build Workspace", "Cmd+Shift+", "B", ui.Cmd | ui.Shift, ui.KeyB, func(a *App) { a.runGoTask(tasks.Build) }},
		{"Go: Test Workspace", "", "", 0, 0, func(a *App) { a.runGoTask(tasks.Test) }},
		{"Tasks: Show", "", "", 0, 0, func(a *App) { a.showTasks() }},
		{"Tasks: Stop", "", "", 0, 0, func(a *App) { a.stopGoTask() }},
		{"Show Output", "", "", 0, 0, func(a *App) { a.term.open = true; a.term.view = panelOutput; a.term.focus = nil }},
		{"New Terminal", "Ctrl+Shift+", "`", ui.Ctrl | ui.Shift, ui.KeyBackquote, func(a *App) { a.newTerminalTab() }},
		{"Open in External Terminal", "Cmd+Shift+", "C", ui.Cmd | ui.Shift, ui.KeyC, func(a *App) { a.openExternalTerminal() }},
		{"Settings", "Cmd+", ",", ui.Cmd, ui.KeyComma, func(a *App) {
			a.host.OpenSettings()
		}},
		{"Keep Preview Tab", "", "", 0, 0, func(a *App) {
			if a.focus != nil {
				a.focus.preview = ""
			}
		}},
		{"Shell Command: Install 'see' Command in PATH", "", "", 0, 0, func(a *App) { a.installSee() }},
		{"Shell Command: Uninstall 'see' Command from PATH", "", "", 0, 0, func(a *App) { a.uninstallSee() }},
	}
	cmds = append(cmds,
		command{"Show Signature Help", "Ctrl+Shift+", "Space", ui.Ctrl | ui.Shift, ui.KeySpace, func(a *App) { a.showSignatureAtCaret() }},
		command{"Find in File", "Cmd+", "F", 0, 0, func(a *App) {
			if st := a.activeEditor(); st != nil {
				st.Run(editor.Find)
			}
		}},
		command{"Replace in File", replaceLabel, replaceKeyName, replaceMods, replaceKey, func(a *App) {
			if st := a.activeEditor(); st != nil {
				st.Run(editor.FindReplace)
			}
		}},
		command{"Find References", "Shift+", "F12", ui.Shift, ui.KeyF12, func(a *App) { a.findUsages(false) }},
		command{"Pin References", "", "", 0, 0, func(a *App) { a.pinReferencesCommand() }},
		command{"Find Callers", "", "", 0, 0, func(a *App) { a.findUsages(true) }},
		command{"Show Call Hierarchy", "Shift+Alt+", "H", ui.Shift | ui.Alt, ui.KeyH, func(a *App) { a.showCallHierarchy() }},
		command{"Next Method", methodLabel, "↓", methodMods, ui.KeyDown, func(a *App) { a.goToMethod(1) }},
		command{"Previous Method", methodLabel, "↑", methodMods, ui.KeyUp, func(a *App) { a.goToMethod(-1) }},
		command{"Go to Definition", "", "F12", 0, 0, func(a *App) {
			if st := a.activeEditor(); st != nil && st.OnDefinition != nil {
				st.OnDefinition(st.Buffer.Offset(st.CursorPos()))
			}
		}},
		command{"Peek Definition", "Alt+", "F12", ui.Alt, ui.KeyF12, func(a *App) { a.navigateActive("peek") }},
		command{"Go to Implementation", "Cmd+", "F12", ui.Cmd, ui.KeyF12, func(a *App) { a.navigateActive("implementation") }},
		command{"Go to Type Definition", "", "", 0, 0, func(a *App) { a.navigateActive("typeDefinition") }},
		command{"Show Code Actions", "Cmd+", ".", ui.Cmd, ui.KeyPeriod, func(a *App) { a.showCodeActions() }},
		command{"Rename Symbol", "", "F2", 0, ui.KeyF2, func(a *App) { a.renameSymbol() }},
		command{"Format Document", "Shift+Alt+", "F", ui.Shift | ui.Alt, ui.KeyF, func(a *App) { a.formatDocument() }},
		command{"Format Selection", "", "", 0, 0, func(a *App) { a.formatSelection() }},
		command{"Edit All in Scope", scopeLabel, scopeKeyName, scopeMods, scopeKey, func(a *App) { a.editAllInScope() }},
		command{"Expand Selection", "Shift+Alt+", "↑", ui.Shift | ui.Alt, ui.KeyUp, func(a *App) { a.expandSelection() }},
		command{"Shrink Selection", "Shift+Alt+", "↓", ui.Shift | ui.Alt, ui.KeyDown, func(a *App) { a.shrinkSelection() }},
	)
	for n := 1; n <= 9; n++ {
		label := fmt.Sprintf("Select Tab %d", n)
		if n == 9 {
			label = "Select Last Tab"
		}
		cmds = append(cmds, command{label, "Cmd+", fmt.Sprint(n), ui.Cmd, ui.Key1 + ui.Key(n-1), func(a *App) { a.activateTabNumber(n) }})
	}
	cmds = append(cmds, foldingCommands()...)
	for i := range cmds {
		cmds[i].shortcutMods = effectiveShortcutModifiers(goos, cmds[i].mods)
	}
	return cmds
}

func effectiveShortcutModifiers(goos, label string) ui.Modifiers {
	var mods ui.Modifiers
	for _, part := range strings.Split(strings.TrimSuffix(label, "+"), "+") {
		switch part {
		case "":
		case "Cmd":
			if goos == "darwin" {
				mods |= ui.Super
			} else {
				mods |= ui.Ctrl
			}
		case "Ctrl":
			mods |= ui.Ctrl
		case "Shift":
			mods |= ui.Shift
		case "Alt":
			mods |= ui.Alt
		default:
			panic(fmt.Sprintf("unknown command modifier %q", part))
		}
	}
	return mods
}

func editAllScopeShortcut(goos string) (string, string, ui.Modifiers, ui.Key) {
	if goos == "darwin" {
		return "Ctrl+Cmd+", "E", ui.Ctrl | ui.Super, ui.KeyE
	}
	return "Ctrl+Shift+", "E", ui.Ctrl | ui.Shift, ui.KeyE
}

// jumpBarShortcuts are Xcode's Show Document Items (Control-6) and Show
// Group Files (Control-5); Control and a digit select tabs elsewhere.
func jumpBarShortcuts() (string, string, ui.Modifiers, ui.Key, string, string, ui.Modifiers, ui.Key) {
	return jumpBarShortcutsFor(runtime.GOOS)
}

func jumpBarShortcutsFor(goos string) (string, string, ui.Modifiers, ui.Key, string, string, ui.Modifiers, ui.Key) {
	if goos == "darwin" {
		return "Ctrl+", "6", ui.Ctrl, ui.Key6, "Ctrl+", "5", ui.Ctrl, ui.Key5
	}
	return "Ctrl+Shift+", ".", ui.Ctrl | ui.Shift, ui.KeyPeriod, "Ctrl+Shift+", ";", ui.Ctrl | ui.Shift, ui.KeySemicolon
}

func fileReplaceShortcut() (string, string, ui.Modifiers, ui.Key) {
	return fileReplaceShortcutFor(runtime.GOOS)
}

func fileReplaceShortcutFor(goos string) (string, string, ui.Modifiers, ui.Key) {
	if goos == "darwin" {
		return "Alt+Cmd+", "F", ui.Alt | ui.Cmd, ui.KeyF
	}
	return "Ctrl+", "H", ui.Ctrl, ui.KeyH
}

func counterpartShortcut(goos string) (string, ui.Modifiers) {
	if goos == "darwin" {
		return "Ctrl+Cmd+", ui.Ctrl | ui.Cmd
	}
	return "Ctrl+Shift+", ui.Ctrl | ui.Shift
}

func maximizeEditorShortcut(goos string) (string, ui.Modifiers) {
	if goos == "darwin" {
		return "Ctrl+Cmd+Shift+", ui.Ctrl | ui.Cmd | ui.Shift
	}
	return "Ctrl+Alt+Shift+", ui.Ctrl | ui.Alt | ui.Shift
}

func (a *App) openPalette() {
	a.openQuickPanel(">")
}

func (a *App) installSee() {
	exe, err := os.Executable()
	if err != nil {
		a.note(locales.MsgCouldNotLocateTheApplication()+err.Error(), true)
		return
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		a.note(locales.MsgCouldNotLocateTheApplication()+err.Error(), true)
		return
	}
	if strings.Contains(filepath.ToSlash(exe), "/go-build") || strings.Contains(filepath.ToSlash(exe), "/.mygo/dev/") {
		a.note(locales.MsgBuildAPermanentExecutableBeforeInstallingTheSee(), true)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		a.note(locales.MsgCouldNotLocateYourHomeDirectory()+err.Error(), true)
		return
	}
	dir := filepath.Join(home, ".local", "bin")
	path, already, err := installSeeCommand(exe, dir)
	if err != nil {
		a.note(fmt.Sprintf(locales.MsgCouldNotInstallSV(), path, err), true)
		return
	}
	msg := "Installed see at " + path
	if already {
		msg = "The see command is already installed at " + path
	}
	found := false
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(p) == dir {
			found = true
		}
	}
	if !found {
		msg += ". Add " + dir + " to your PATH."
	}
	a.note(msg, false)
}

func (a *App) uninstallSee() {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		a.note(locales.MsgCouldNotLocateTheApplication()+err.Error(), true)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		a.note(locales.MsgCouldNotLocateYourHomeDirectory()+err.Error(), true)
		return
	}
	path, removed, err := uninstallSeeCommand(exe, filepath.Join(home, ".local", "bin"))
	if err != nil {
		a.note(fmt.Sprintf(locales.MsgCouldNotUninstallSV(), path, err), true)
	} else if !removed {
		a.note(locales.MsgTheSeeCommandIsNotInstalledAt()+path, false)
	} else {
		a.note(locales.MsgUninstalledSeeFrom()+path, false)
	}
}

func uninstallSeeLink(exe, dir string) (path string, removed bool, err error) {
	path = filepath.Join(dir, "see")
	target, err := os.Readlink(path)
	if os.IsNotExist(err) {
		return path, false, nil
	}
	if err != nil {
		return path, false, err
	}
	if target != exe {
		return path, false, fmt.Errorf("link points to %s, not the running application", target)
	}
	if err := os.Remove(path); err != nil {
		return path, false, err
	}
	return path, true, nil
}

func installSeeLink(exe, dir string) (path string, already bool, err error) {
	path = filepath.Join(dir, "see")
	if err = os.MkdirAll(dir, 0755); err == nil {
		err = os.Symlink(exe, path)
	}
	if err != nil {
		if target, e := os.Readlink(path); e == nil && target == exe {
			return path, true, nil
		}
		return path, false, err
	}
	return path, false, nil
}

// toggleInvisibles shows spaces and tabs as dots and arrows everywhere,
// as Xcode's Editor > Invisibles; off, they show only in the selection.
func (a *App) toggleInvisibles() {
	a.cfg.ShowInvisibles = !a.cfg.ShowInvisibles
	a.applyEditorPrefsAll()
	a.settingsChanged()
	a.host.RefreshMenu()
}
