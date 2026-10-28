package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"see/internal/search"
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
		{"Toggle Markdown Preview", "Cmd+Shift+", "V", ui.Cmd | ui.Shift, ui.KeyV, func(a *app) { a.toggleMarkdownPreview(a.focus) }},
		{"Find in Workspace", "Cmd+Shift+", "F", ui.Cmd | ui.Shift, ui.KeyF, func(a *app) { a.sidebarOff, a.navigator = false, navFind }},
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
	}
	cmds = append(cmds,
		command{"Find in File", "Cmd+", "F", 0, 0, func(a *app) {
			if st := a.activeEditor(); st != nil {
				st.Find(st.SelectedText())
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

type commandPalette struct {
	open  bool
	query string
	sel   int
	list  ui.ListState
}

func (a *app) openPalette() {
	a.quick.open = false
	a.palette = commandPalette{open: true}
	a.palette.list.Selected = &a.palette.sel
}
func (a *app) commandPanel(c *ui.Context) {
	p := &a.palette
	all := commands()
	names := make([]string, len(all))
	for i, cmd := range all {
		names[i] = cmd.name
	}
	results := search.Fuzzy(names, p.query, len(names))
	p.sel = max(0, min(p.sel, len(results)-1))
	choose := func(i int) {
		if i < 0 || i >= len(results) {
			return
		}
		for _, cmd := range all {
			if cmd.name == results[i].Path {
				p.open = false
				cmd.run(a)
				return
			}
		}
	}
	ui.Overlay(c, func() {
		backdrop := ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).AlignItems(ui.Center).Background(ui.RGBA(0, 0, 0, 0.18))
		backdrop.OnClick(func() { p.open = false })
		backdrop.Children(func() {
			panel := ui.Column(c).Margin(90, 0, 0, 0).Width(640).MaxHeight(460).Radius(14).Padding(8).Gap(6).Material(glass.Glass{}).Border(1, colPopoverEdge).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.5))
			panel.OnClick(func() {})
			panel.Children(func() {
				in := ui.TextInput(c, &p.query).Placeholder("Command Palette").FontSize(20).AutoFocus().Background(ui.Transparent).BorderWidth(0)
				switch {
				case in.Submitted():
					choose(p.sel)
				case in.Shortcut(0, ui.KeyEscape):
					p.open = false
				case in.Shortcut(0, ui.KeyDown):
					p.sel = min(p.sel+1, len(results)-1)
					p.list.ScrollTo(p.sel, ui.Center)
				case in.Shortcut(0, ui.KeyUp):
					p.sel = max(p.sel-1, 0)
					p.list.ScrollTo(p.sel, ui.Center)
				}
				if len(results) == 0 {
					ui.Text(c, "No Matches").TextColor(colMuted).Padding(8)
					return
				}
				ui.List(c, &p.list, len(results), func(i int) {
					name := results[i].Path
					row := ui.Row(c.Key(name)).FillWidth().Padding(8).Gap(8).Radius(8).AlignItems(ui.Center)
					if i == p.sel {
						row.Background(colAccent)
					}
					row.OnClick(func() { choose(i) })
					row.Children(func() {
						ui.Text(c, name).Grow(1).SingleLine()
						for _, cmd := range all {
							if cmd.name == name && cmd.key != "" {
								ui.Text(c, keyLabel(cmd.mods, cmd.key)).TextColor(colMuted)
								break
							}
						}
					})
				}).Grow(1).FillWidth().MaxHeight(380).Label("Commands")
			})
		})
	})
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
	if strings.Contains(filepath.ToSlash(exe), "/go-build") {
		a.note("Build a permanent executable before installing the see command.", true)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		a.note("Could not locate your home directory: "+err.Error(), true)
		return
	}
	dir := filepath.Join(home, ".local", "bin")
	path := filepath.Join(dir, "see")
	if err = os.MkdirAll(dir, 0755); err == nil {
		err = os.Symlink(exe, path)
	}
	if err != nil {
		if target, e := os.Readlink(path); e == nil && target == exe {
			a.note("The see command is already installed at "+path, false)
			return
		}
		a.note(fmt.Sprintf("Could not install %s: %v", path, err), true)
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
