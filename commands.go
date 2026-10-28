package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type command struct {
	name, mods, key string
	shortcutMods    ui.Modifiers
	shortcutKey     ui.Key
	run             func(*app)
}

func commands() []command {
	cmds := []command{
		{"Command Palette", "Cmd+Shift+", "P", ui.Cmd | ui.Shift, ui.KeyP, func(a *app) { a.openPalette() }},
		{"Open Folder", "Cmd+", "O", ui.Cmd, ui.KeyO, func(a *app) { a.openFolderDialog() }},
		{"Open Quickly", "Cmd+Shift+", "O", ui.Cmd | ui.Shift, ui.KeyO, func(a *app) { a.openQuickly() }},
		{"Search Workspace Symbols", "Cmd+", "T", ui.Cmd, ui.KeyT, func(a *app) { a.openQuickPanel("#") }},
		{"Toggle Markdown Preview", "Cmd+Shift+", "V", ui.Cmd | ui.Shift, ui.KeyV, func(a *app) { a.toggleMarkdownPreview(a.focus) }},
		{"Find in Workspace", "Cmd+Shift+", "F", ui.Cmd | ui.Shift, ui.KeyF, func(a *app) {
			a.sidebarOff, a.navigator, a.find.focus = false, navFind, true
		}},
		{"Show Git", "", "", 0, 0, func(a *app) { a.sidebarOff, a.navigator = false, navGit; a.refreshGit() }},
		{"Filter Files", "Alt+Cmd+", "J", ui.Alt | ui.Cmd, ui.KeyJ, func(a *app) {
			a.sidebarOff, a.navigator, a.filter.focus = false, navFiles, true
		}},
		{"Show or Hide the Navigator", "Cmd+", "B", ui.Cmd, ui.KeyB, func(a *app) { a.sidebarOff = !a.sidebarOff }},
		{"Go Back", "Ctrl+", "-", ui.Ctrl, ui.KeyMinus, func(a *app) { a.goBack() }},
		{"Go Forward", "Ctrl+Shift+", "-", ui.Ctrl | ui.Shift, ui.KeyMinus, func(a *app) { a.goForward() }},
		{"Next Tab", "Ctrl+", "Tab", ui.Ctrl, ui.KeyTab, func(a *app) { a.cycleTab(1) }},
		{"Previous Tab", "Ctrl+Shift+", "Tab", ui.Ctrl | ui.Shift, ui.KeyTab, func(a *app) { a.cycleTab(-1) }},
		{"Save", "Cmd+", "S", ui.Cmd, ui.KeyS, func(a *app) { a.saveActive() }},
		{"Save All", "Cmd+Shift+", "S", ui.Cmd | ui.Shift, ui.KeyS, func(a *app) { a.saveAll() }},
		{"Close Tab", "Cmd+", "W", ui.Cmd, ui.KeyW, func(a *app) {
			if p := a.activePath(); p != "" {
				a.closeDoc(p)
			}
		}},
		{"Add Editor on Right", "Cmd+", "D", ui.Cmd, ui.KeyD, func(a *app) { a.splitActive(sideRight) }},
		{"Add Editor Below", "Cmd+Shift+", "D", ui.Cmd | ui.Shift, ui.KeyD, func(a *app) { a.splitActive(sideBottom) }},
		{"Show or Hide the Terminal", "Cmd+Shift+", "Y", ui.Cmd | ui.Shift, ui.KeyY, func(a *app) { a.toggleTerminal() }},
		{"Show Output", "", "", 0, 0, func(a *app) { a.term.open = true; a.term.output = true; a.term.focus = nil }},
		{"New Terminal", "Ctrl+Shift+", "`", ui.Ctrl | ui.Shift, ui.KeyBackquote, func(a *app) { a.newTerminalTab() }},
		{"Settings", "Cmd+", ",", ui.Cmd, ui.KeyComma, func(a *app) {
			if a.openSettings != nil {
				a.openSettings()
			}
		}},
		{"Keep Preview Tab", "", "", 0, 0, func(a *app) {
			if a.focus != nil {
				a.focus.preview = ""
			}
		}},
		{"Shell Command: Install 'see' Command in PATH", "", "", 0, 0, func(a *app) { a.installSee() }},
		{"Shell Command: Uninstall 'see' Command from PATH", "", "", 0, 0, func(a *app) { a.uninstallSee() }},
	}
	cmds = append(cmds,
		command{"Find in File", "Cmd+", "F", 0, 0, func(a *app) {
			if st := a.activeEditor(); st != nil {
				st.Run(editor.Find)
			}
		}},
		command{"Go to Definition", "", "F12", 0, 0, func(a *app) {
			if st := a.activeEditor(); st != nil && st.OnDefinition != nil {
				st.OnDefinition(st.Buffer.Offset(st.CursorPos()))
			}
		}},
	)
	for n := 1; n <= 9; n++ {
		label := fmt.Sprintf("Select Tab %d", n)
		if n == 9 {
			label = "Select Last Tab"
		}
		cmds = append(cmds, command{label, "Cmd+", fmt.Sprint(n), ui.Cmd, ui.Key1 + ui.Key(n-1), func(a *app) { a.activateTabNumber(n) }})
	}
	return cmds

}

func (a *app) openPalette() {
	a.openQuickPanel(">")
}

func (a *app) installSee() {
	exe, err := os.Executable()
	if err != nil {
		a.note("Could not locate the application: "+err.Error(), true)
		return
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		a.note("Could not locate the application: "+err.Error(), true)
		return
	}
	if strings.Contains(filepath.ToSlash(exe), "/go-build") || strings.Contains(filepath.ToSlash(exe), "/.mygo/dev/") {
		a.note("Build a permanent executable before installing the see command.", true)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		a.note("Could not locate your home directory: "+err.Error(), true)
		return
	}
	dir := filepath.Join(home, ".local", "bin")
	path, already, err := installSeeLink(exe, dir)
	if err != nil {
		a.note(fmt.Sprintf("Could not install %s: %v", path, err), true)
		return
	}
	if already {
		a.note("The see command is already installed at "+path, false)
		return
	}
	msg := "Installed see at " + path
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

func (a *app) uninstallSee() {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		a.note("Could not locate the application: "+err.Error(), true)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		a.note("Could not locate your home directory: "+err.Error(), true)
		return
	}
	path, removed, err := uninstallSeeLink(exe, filepath.Join(home, ".local", "bin"))
	if err != nil {
		a.note(fmt.Sprintf("Could not uninstall %s: %v", path, err), true)
	} else if !removed {
		a.note("The see command is not installed at "+path, false)
	} else {
		a.note("Uninstalled see from "+path, false)
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
