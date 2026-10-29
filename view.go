package main

import (
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// editorBindings take from the editor the keys that are the app's.
func editorBindings() []editor.Binding {
	none := func(m ui.Modifiers, k ui.Key) editor.Binding {
		return editor.Binding{Mods: m, Key: k, Command: editor.NoCommand}
	}
	bs := []editor.Binding{
		none(ui.Cmd, ui.KeyW),
		none(ui.Cmd, ui.KeyS),
		none(ui.Cmd|ui.Shift, ui.KeyS),
		none(ui.Cmd, ui.KeyO),
		none(ui.Cmd, ui.KeyN),
		none(ui.Cmd, ui.KeyP),
		none(ui.Cmd, ui.KeyB),
		none(ui.Cmd|ui.Shift, ui.KeyP),
		none(ui.Cmd|ui.Shift, ui.KeyO),
		none(ui.Cmd|ui.Shift, ui.KeyF),
		none(ui.Cmd|ui.Shift, ui.KeyV),
		none(ui.Cmd|ui.Shift, ui.KeyC),
		none(ui.Cmd, ui.KeyBackslash),
		none(ui.Cmd, ui.KeyD), // split, as iTerm; the reader selects no next occurrence to edit
		none(ui.Cmd|ui.Shift, ui.KeyD),
		none(ui.Ctrl, ui.KeyTab),
		none(ui.Ctrl|ui.Shift, ui.KeyTab),
		none(ui.Ctrl, ui.KeyMinus),
		none(ui.Ctrl|ui.Shift, ui.KeyMinus),
	}
	for k := ui.Key1; k <= ui.Key9; k++ {
		bs = append(bs, none(ui.Cmd, k))
	}
	if runtime.GOOS != "darwin" {
		// Back and forward, as VS Code on Windows; the editor would move
		// by subwords.
		bs = append(bs, none(ui.Alt, ui.KeyLeft), none(ui.Alt, ui.KeyRight),
			none(ui.Ctrl, ui.KeyPageUp), none(ui.Ctrl, ui.KeyPageDown))
	}
	return bs
}

// startTime is when the process started, for the startup trace.
var startTime = time.Now()

var startTrace = os.Getenv("SEE_TRACE_STARTUP") != ""

func (a *app) view(c *ui.Context) {
	if startTrace {
		startTrace = false
		a.logf("startup: first frame built %v after process start", time.Since(startTime).Round(time.Millisecond))
	}
	a.applyAppearance(c)
	c.SetTheme(a.theme)
	vibrant := c.Vibrancy()
	if vibrant {
		c.Root().Background(ui.Transparent)
	}
	// Cmd held makes hovers links to definitions; letting it go ends one.
	a.hover.cmd = c.Modifiers()&ui.Cmd != 0
	if !a.hover.cmd {
		a.hideLink()
	}
	if a.root == "" {
		// The main window hides without a folder (windows.sync); the
		// welcome window shows instead.
		if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyN) {
			a.host.openProjectWindow("")
		} else if c.Shortcut(ui.Cmd, ui.KeyO) {
			a.openFolderDialog()
		}
		a.welcomeWindow(c)
		return
	}
	a.shortcuts(c)
	root := ui.Column(c).Fill()
	target := float32(1)
	if a.sidebarOff {
		target = 0
	}
	progress := min(max(root.AnimateTo("sidebar", target, ui.Animation{Spring: ui.SpringOptions{Duration: 250 * time.Millisecond}}), 0), 1)
	root.Children(func() {
		a.toolbar(c, progress, vibrant)
		if a.confirm != nil && a.confirm.inWindow {
			q := a.confirm
			a.banner(c, colors(c).warnBg, q.message, q.actions)
		}
		if a.deleteConfirm != nil {
			q := a.deleteConfirm
			a.banner(c, colors(c).warnBg, q.message, q.actions)
		}
		ui.Row(c).Grow(1).FillWidth().AlignItems(ui.Stretch).Children(func() {
			if progress == 0 {
				a.editorArea(c)
				return
			}
			shownWidth := a.sidebarW * progress
			if ui.Split(c, &shownWidth, func() { a.navigatorPane(c, vibrant) }, func() { a.editorArea(c) }).Grow(1).FillHeight().Changed() && !a.sidebarOff && progress == 1 {
				// Changed holds for every frame of a drag: save once it stops.
				a.sidebarW = shownWidth
				a.cfg.SidebarWidth = a.sidebarW
				a.saveConfigSoon()
			}
		})
	})
	if a.quick.open {
		a.quickOpenPanel(c)
	}
}

// shortcuts handles the app's keys wherever the focus is.
func (a *app) shortcuts(c *ui.Context) {
	// While a terminal has the keyboard, the shell gets the keys with
	// Control and Alt: the app asks for none of them, as asking takes
	// the key (Alt+Left moves by words there, Control+- undoes).
	inTerm := a.term.open && a.term.focused
	switch {
	case c.Shortcut(ui.Cmd|ui.Shift, ui.Key1):
		a.showWelcome = true
	case c.Shortcut(ui.Cmd, ui.KeyP):
		a.openQuickly()
	case c.Shortcut(ui.Cmd, ui.Key0):
		a.sidebarOff = !a.sidebarOff
	case c.Shortcut(ui.Ctrl, ui.KeyBackquote):
		a.toggleTerminal()
	case c.Shortcut(ui.Cmd, ui.KeyBackslash):
		if inTerm {
			a.splitTerminal()
		} else {
			a.splitActive(sideRight)
		}
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyBackslash):
		a.splitActive(sideBottom)
	}
	for _, cmd := range commands() {
		if cmd.shortcutKey == 0 || (inTerm && ((cmd.shortcutMods&ui.Ctrl != 0 && cmd.shortcutKey != ui.KeyBackquote) || cmd.shortcutKey == ui.KeyW)) {
			continue
		}
		if c.Shortcut(cmd.shortcutMods, cmd.shortcutKey) {
			if inTerm && cmd.shortcutKey == ui.KeyD && cmd.shortcutMods == ui.Cmd {
				a.splitTerminal()
			} else {
				cmd.run(a)
			}
		}
	}
	if inTerm {
		return
	}
	back := c.Shortcut(ui.Ctrl|ui.Cmd, ui.KeyLeft)
	fwd := c.Shortcut(ui.Ctrl|ui.Cmd, ui.KeyRight)
	next := false
	prev := false
	if runtime.GOOS == "darwin" {
		next = c.Shortcut(ui.Cmd|ui.Shift, ui.KeyBracketRight) || next
		prev = c.Shortcut(ui.Cmd|ui.Shift, ui.KeyBracketLeft) || prev
	} else {
		back = c.Shortcut(ui.Alt, ui.KeyLeft) || back
		fwd = c.Shortcut(ui.Alt, ui.KeyRight) || fwd
		next = c.Shortcut(ui.Ctrl, ui.KeyPageDown) || next
		prev = c.Shortcut(ui.Ctrl, ui.KeyPageUp) || prev
	}
	switch {
	case back:
		a.goBack()
	case fwd:
		a.goForward()
	case next:
		a.cycleTab(1)
	case prev:
		a.cycleTab(-1)
	}
	if c.Shortcut(ui.Cmd, ui.KeyF) {
		if st := a.activeEditor(); st != nil {
			st.Run(editor.Find)
		}
	}
	if c.Shortcut(0, ui.KeyEscape) {
		a.choices = nil
		if !a.quick.open {
			a.git.peek = nil
		}
	}
}

// shortcutLabel shows Cmd+key as the platform writes it.
func shortcutLabel(key string) string { return keyLabel("Cmd+", key) }

// keyLabel shows a shortcut as the platform writes it: ⇧⌘O on macOS,
// Ctrl+Shift+O elsewhere.
func keyLabel(mods, key string) string {
	if runtime.GOOS == "darwin" {
		return strings.NewReplacer("Shift+", "⇧", "Ctrl+", "⌃", "Alt+", "⌥", "Cmd+", "⌘").Replace(mods) + key
	}
	return strings.ReplaceAll(mods, "Cmd+", "Ctrl+") + key
}

// iconButton is a borderless toolbar button showing an icon, as AppKit's.
func iconButton(c *ui.Context, icon *ui.SVG, label, tip string, on bool) ui.Element {
	return iconButtonWithState(c, icon, label, tip, on, false, 6)
}

func glassToolbarButton(c *ui.Context, icon *ui.SVG, label, tip string, on bool) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(tip).Size(32, 32).Radius(16).Shrink(0).KeepFocus().
		Center().Material(glass.Glass{Interactive: true})
	b.Children(func() {
		face := ui.Box(c).Size(24, 24).Radius(12).Center()
		if on {
			face.Background(colors(c).selectedRow)
		}
		face.Children(func() { symbolIcon(c, icon, 16, colors(c).text) })
	})
	return b
}

func iconButtonWithState(c *ui.Context, icon *ui.SVG, label, tip string, on, disabled bool, radius float32) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(tip).Padding(6).Radius(radius).Shrink(0).KeepFocus().Disabled(disabled)
	color := colors(c).muted
	if on {
		color = c.Theme().Accent
	}
	if !disabled && (b.Hovered() || b.Pressed()) {
		b.Background(colors(c).pill)
	}
	b.Children(func() { symbolIcon(c, icon, 16, color) })
	return b
}
