package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/search"
	"see/internal/workspace"
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
		none(ui.Cmd, ui.KeyP),
		none(ui.Cmd, ui.KeyB),
		none(ui.Cmd|ui.Shift, ui.KeyP),
		none(ui.Cmd|ui.Shift, ui.KeyO),
		none(ui.Cmd|ui.Shift, ui.KeyF),
		none(ui.Cmd|ui.Shift, ui.KeyV),
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

var startTrace = os.Getenv("SEE_TRACE_STARTUP") != "" || os.Getenv("NSR_TRACE_STARTUP") != ""

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
		if c.Shortcut(ui.Cmd, ui.KeyO) {
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
		if a.confirm != nil {
			q := a.confirm
			a.banner(c, colWarnBg, q.message, q.actions)
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
			face.Background(colSelectedRow)
		}
		face.Children(func() { ui.Icon(c, icon).Size(16, 16).TextColor(colText) })
	})
	return b
}

func iconButtonWithState(c *ui.Context, icon *ui.SVG, label, tip string, on, disabled bool, radius float32) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(tip).Padding(6).Radius(radius).Shrink(0).KeepFocus().Disabled(disabled)
	color := colMuted
	if on {
		color = colAccent
	}
	if !disabled && (b.Hovered() || b.Pressed()) {
		b.Background(colPill)
	}
	b.Children(func() { ui.Icon(c, icon).Size(16, 16).TextColor(color) })
	return b
}

// ----- Toolbar -----

// toolbarHeight leaves room for the glass controls and window buttons.
const toolbarHeight = 48

// toolbar aligns the navigator controls with the sidebar and groups the
// document navigation and editor actions over the workspace.
func (a *app) toolbar(c *ui.Context, sidebarProgress float32, vibrant bool) {
	bar := c.TitleBar()
	closedWidth := max(bar.Left, 8) + 44
	// Split places its one-point divider after the sidebar; include it in
	// the header so both divider lines share the same x position.
	width := closedWidth + (max(closedWidth, a.sidebarW+1)-closedWidth)*sidebarProgress
	windowWidth, _ := c.Size()
	const quickWidth = 132
	center := (width + windowWidth - bar.Right) / 2
	projectWidth := center - quickWidth/2 - width - 82
	centerQuick := projectWidth >= 60 && center+quickWidth/2+120+bar.Right <= windowWidth
	row := ui.Row(c).FillWidth().Height(max(bar.Height, toolbarHeight)).
		AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(colHairline).DragWindow()
	row.Children(func() {
		sidebarHeader := ui.Row(c).Width(width).MinWidth(bar.Left+44).Shrink(1).Height(toolbarHeight).Padding(0, 0, 0, max(bar.Left, 8)+12).
			AlignItems(ui.Center)
		if !a.sidebarOff {
			sidebarHeader.Background(sidebarColor(vibrant)).BorderWidth(0, 1, 0, 0).BorderColor(colSeparator)
		} else {
			sidebarHeader.Background(colToolbar)
		}
		sidebarHeader.Children(func() {
			if glassToolbarButton(c, icSidebar, "Show Navigator", "Hide or Show the Navigator", !a.sidebarOff).Clicked() {
				a.sidebarOff = !a.sidebarOff
			}
		})
		ui.Row(c).Grow(1).Shrink(1).FillHeight().Padding(0, bar.Right+8, 0, 6).Gap(6).AlignItems(ui.Center).Background(colToolbar).Children(func() {
			ui.Row(c).Height(32).Radius(16).Padding(0, 3).AlignItems(ui.Center).Material(glass.Glass{}).Children(func() {
				back := iconButtonWithState(c, icBack, "Go Back", "Go Back", false, len(a.hist.Back) == 0, 13)
				if back.Clicked() {
					a.goBack()
				}
				ui.Box(c).Size(1, 16).Background(colSeparator)
				fwd := iconButtonWithState(c, icForward, "Go Forward", "Go Forward", false, len(a.hist.Forward) == 0, 13)
				if fwd.Clicked() {
					a.goForward()
				}
			})
			project := ui.Column(c).Shrink(1).Gap(0).AlignItems(ui.Start).Justify(ui.Center)
			if centerQuick {
				project.MaxWidth(projectWidth)
			}
			project.Children(func() {
				a.projectTitle(c)
				a.activityView(c)
			})
			ui.Box(c).Grow(1)
			if !centerQuick {
				a.quickButton(c, false, 0, 0)
			}
			ui.Box(c).Grow(1)
			ui.Row(c).Height(32).Radius(16).Padding(0, 3).AlignItems(ui.Center).Material(glass.Glass{}).Children(func() {
				if iconButton(c, icSplitH, "Split Right", "Add Editor on Right ("+keyLabel("Cmd+", "D")+")", false).Clicked() {
					a.splitActive(sideRight)
				}
				if iconButton(c, icSplitV, "Split Down", "Add Editor Below ("+keyLabel("Cmd+Shift+", "D")+")", false).Clicked() {
					a.splitActive(sideBottom)
				}
			})
			if glassToolbarButton(c, icPanel, "Show Terminal", "Hide or Show the Terminal ("+keyLabel("Cmd+Shift+", "Y")+")", a.term.open).Clicked() {
				a.toggleTerminal()
			}
		})
		if centerQuick {
			a.quickButton(c, true, center-quickWidth/2, (max(bar.Height, toolbarHeight)-32)/2)
		}
	})
}

func (a *app) quickButton(c *ui.Context, centered bool, x, y float32) {
	quick := ui.ButtonBase(c).Height(32).Padding(0, 12).Radius(16).Shrink(1).KeepFocus().
		Label("Open Quickly").Tooltip("Open Quickly (" + keyLabel("Cmd+Shift+", "O") + ")").Material(glass.Glass{Interactive: true})
	if centered {
		quick.Absolute().Left(x).Top(y).Width(132).Center()
	}
	quick.Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icSearch).Size(15, 15).TextColor(colText)
			ui.Text(c, "Open Quickly").FontSize(12).SingleLine().Shrink(1)
		})
	})
	if quick.Clicked() {
		a.openQuickly()
	}
}

// projectTitle is the toolbar's project name, as Peek's: a click opens a
// native menu of the recent folders, with the one open checked, so the
// window's title is also the way between projects.
func (a *app) projectTitle(c *ui.Context) {
	b := ui.ButtonBase(c).Label("Switch Project").Tooltip(tildePath(a.root)).
		Padding(0, 5, 0, 0).Radius(6).Shrink(1).Bind(&a.titleButton)
	if b.Hovered() || b.Pressed() {
		b.Background(colPill)
	}
	b.OnClick(func() {
		if a.popupMenu == nil {
			return
		}
		r := a.titleButton.Bounds(c)
		a.projectMenu(int(r.X), int(r.Y+r.H+4))
	})
	b.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, filepath.Base(a.root)).Bold().SingleLine().Shrink(1)
			ui.Icon(c, icChevron).Size(9, 9).TextColor(colDim).Shrink(0)
		})
	})
}

// projectMenu opens the recent folders at x, y in the window, as Xcode's
// Open Recent: the folder open is checked, and folders with the same name
// show their parent's too.
func (a *app) projectMenu(x, y int) {
	var items []*mygo.MenuItem
	for _, p := range a.cfg.Recent {
		p := p
		items = append(items, &mygo.MenuItem{
			Label:   projectLabel(a.cfg.Recent, p),
			ToolTip: p,
			Type:    mygo.MenuItemCheckbox,
			Checked: p == a.root,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.post(func() { a.openRecent(p) })
			},
		})
	}
	if len(items) > 0 {
		items = append(items, mygo.Separator())
	}
	items = append(items, &mygo.MenuItem{
		Label:       "Open Folder…",
		Accelerator: "CmdOrCtrl+O",
		Click: func(*mygo.MenuItem, *mygo.Window) {
			a.post(a.openFolderDialog)
		},
	})
	a.popupMenu(x, y, items)
}

// projectLabel names a recent folder by its last part, and its parent's
// too where another recent folder has the same name, as Peek does.
func projectLabel(recent []string, p string) string {
	name := filepath.Base(p)
	for _, r := range recent {
		if r != p && filepath.Base(r) == name {
			return name + " — " + tildePath(filepath.Dir(p))
		}
	}
	return name
}

// modeSwitch is the segmented control in the middle of the toolbar, as
// Xcode's and Peek's: a track, and the chosen side raised from it. It
// switches the navigator, and shows the sidebar as it does.
func (a *app) modeSwitch(c *ui.Context) {
	t := c.Theme()
	const pad, inset = 12, 2
	var widest float32
	for _, label := range []string{"Files", "Find"} {
		w, _ := c.MeasureText(0, ui.Span{Text: label, Size: t.FontSize})
		widest = max(widest, w)
	}
	track, chosen := t.Text.Alpha(0.06), t.Background
	if t.Dark {
		track, chosen = t.Text.Alpha(0.10), t.Text.Alpha(0.24)
	}
	parts := ui.SegmentedBase(c, &a.navigator, 2)
	parts.Track.Height(24).AlignItems(ui.Stretch).Padding(inset).Gap(inset).Radius(7).Background(track)
	if c.Preferences().HighContrast {
		parts.Track.Border(1, t.Border)
	}
	parts.Track.Children(func() {
		for i, label := range []string{"Files", "Find"} {
			s := parts.Segment(i).Width(widest + 2*pad).Radius(5).TextColor(t.Text)
			if i == a.navigator {
				s.Background(chosen).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.15))
			}
			s.Children(func() { ui.Text(c, label).FontSize(t.FontSize).SingleLine() })
		}
	})
	if parts.Track.Changed() {
		a.sidebarOff = false
	}
}

// activityView is the pill in the middle of the toolbar, as Xcode's: what
// the app is doing, and what went wrong.
func (a *app) activityView(c *ui.Context) {
	state, serverErr, name := a.lspState, a.lspErr, "gopls"
	retry := a.retryLSP
	if a.focus != nil && typeScriptLanguage(a.activePath()) != "" {
		state, serverErr, name = a.tsState, a.tsErr, "tsrs"
		retry = a.retryTypeScriptLSP
	}
	pill := ui.Row(c).Height(16).MaxWidth(260).Shrink(1).Gap(5).AlignItems(ui.Center)
	pill.Children(func() {
		// The dot tells the state at a glance, as Peek's toolbar does: the
		// accent while work goes on, red when it went wrong.
		dot := colMuted
		switch {
		case a.finding, a.find.running, state == lspStarting:
			dot = colAccent
		case a.noticeError, state == lspFailed, a.watchErr != nil:
			dot = colError
		}
		if dot != colMuted {
			ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0)
		}
		switch {
		case a.finding:
			ui.Spinner(c).FontSize(11)
			ui.Text(c, "Finding definition…").FontSize(12).TextColor(colMuted).SingleLine()
		case a.notice != "":
			t := ui.Text(c, a.notice).FontSize(12).SingleLine().Shrink(1).Tooltip(a.notice)
			if a.noticeError {
				t.TextColor(colError)
			}
			if state == lspFailed && ui.Button(c, "Retry").Label("Retry "+name).Padding(0, 6).FontSize(11).Clicked() {
				retry()
			}
		case a.find.running:
			ui.Spinner(c).FontSize(11)
			ui.Text(c, "Searching…").FontSize(12).TextColor(colMuted)
		case state == lspStarting:
			ui.Spinner(c).FontSize(11)
			ui.Text(c, "Starting "+name+"…").FontSize(12).TextColor(colMuted)
		case state == lspFailed:
			msg := name + " unavailable"
			if serverErr != nil {
				msg = name + ": " + shortErr(serverErr)
			}
			ui.Text(c, msg).FontSize(12).TextColor(colError).SingleLine().Shrink(1)
			if ui.Button(c, "Retry").Label("Retry "+name).Padding(0, 6).FontSize(11).Clicked() {
				retry()
			}
		case a.watchErr != nil:
			ui.Text(c, "Not watching for changes").FontSize(12).TextColor(colMuted).Tooltip(a.watchErr.Error())
		case state == lspReady:
			ui.Text(c, "Ready · "+name).FontSize(12).TextColor(colMuted)
		default:
			if a.git.branch != "" {
				ui.Icon(c, icGit).Size(12, 12).TextColor(colMuted)
				ui.Text(c, a.git.branch).FontSize(12).TextColor(colMuted).SingleLine().Shrink(1).Tooltip("Git branch: " + a.git.branch)
			} else {
				ui.Text(c, "Ready").FontSize(12).TextColor(colMuted)
			}
		}
	})
}

// ----- Navigators -----

// navigatorPane is the sidebar, over the window's material where there
// is one: the navigator selector bar, then the navigator chosen.
func (a *app) navigatorPane(c *ui.Context, vibrant bool) {
	pane := ui.Column(c).Fill().Background(sidebarColor(vibrant))
	pane.Children(func() {
		a.navigatorBar(c)
		if a.navigator == navGit {
			a.gitNavigator(c)
		} else if a.navigator == navFind {
			a.findNavigator(c)
		} else {
			a.projectNavigator(c)
		}
	})
}

func sidebarColor(vibrant bool) ui.Color {
	if vibrant {
		return colSidebar.Alpha(0.78)
	}
	return colSidebar
}

// navigatorBar switches between the file and find navigators.
func (a *app) navigatorBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().Padding(4, 8).BorderWidth(0, 0, 1, 0).
		BorderColor(colHairline).Children(func() {
		parts := ui.SegmentedBase(c, &a.navigator, 3)
		parts.Track.FillWidth().Height(30).Padding(3).Gap(2).Radius(15).Background(t.Text.Alpha(0.06)).Label("Navigator")
		if c.Preferences().HighContrast {
			parts.Track.Border(1, t.Border)
		}
		segments := []struct {
			icon  *ui.SVG
			label string
		}{
			{icFolder, "Show the Project Navigator"},
			{icSearch, "Show the Find Navigator"},
			{icGit, "Show the Git Navigator"},
		}
		parts.Track.Children(func() {
			for i, s := range segments {
				i, s := i, s
				seg := parts.Segment(i).Width(38).Height(24).Radius(12).Label(s.label)
				if i == a.navigator {
					seg.Material(glass.Glass{Interactive: true})
				}
				seg.Children(func() {
					color := colMuted
					if i == a.navigator {
						color = colText
					}
					ui.Icon(c, s.icon).Size(14, 14).TextColor(color)
				})
			}
		})
	})
}

func (a *app) projectNavigator(c *ui.Context) {
	a.syncFilter()
	a.treeHeading(c)
	switch {
	case a.filtering():
		switch {
		case a.quick.err != nil:
			ui.Column(c).Grow(1).Padding(8, 12).Gap(6).Children(func() {
				ui.Text(c, "Could not list the files: "+describeErr(a.quick.err)).TextColor(colError).FontSize(12)
				if ui.Button(c, "Retry").Clicked() {
					a.retryFiles()
				}
			})
		case a.quick.files == nil:
			ui.Row(c).Padding(8, 12).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "Listing files…").TextColor(colMuted)
			})
		case a.filter.matches == 0:
			ui.Column(c).Grow(1).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				ui.Text(c, "No Results").TextColor(colMuted)
			})
		default:
			a.fileTree(c, a.filter.roots)
		}
	default:
		rootDir := a.dirs[a.root]
		switch {
		case rootDir == nil || (!rootDir.loaded && rootDir.loading):
			ui.Row(c).Padding(8, 12).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "Loading…").TextColor(colMuted)
			})
		case !rootDir.loaded:
			a.dirError(c, a.root, rootDir.err)
		case len(rootDir.entries) == 0:
			ui.Text(c, "This folder is empty.").TextColor(colMuted).Padding(8, 12)
		default:
			a.fileTree(c, entryPaths(rootDir))
		}
	}
	if a.filter.toList {
		a.filter.toList = false
		if a.tree.Rows() > 0 {
			a.tree.List.Handle.Focus(c)
		}
	}
	a.filterBar(c)
}

// treeHeading is the navigator's title, as a source list's section: small
// and muted, with what the filter matches at its side, and the actions on
// the tree at its end.
func (a *app) treeHeading(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(4, 10, 3, 10).Gap(6).AlignItems(ui.Center).Children(func() {
		folderGlyph(c, true, 15)
		ui.Text(c, filepath.Base(a.root)).FontSize(12).Bold().SingleLine().Shrink(1)
		if a.filtering() {
			detail := fmt.Sprintf("%d of %d", a.filter.matches, len(a.quick.files))
			if a.quick.files == nil {
				detail = "Filtering…"
			}
			ui.Text(c, detail).FontSize(11).TextColor(colDim).SingleLine().Shrink(1)
		}
		ui.Box(c).Grow(1)
		if glyphButton(c, icCollapse, "Collapse All Folders", "Collapse All Folders", false, a.filtering()).Clicked() {
			a.tree.Open.Clear()
			a.reveal = ""
			a.treeRow = -1
		}
		if glyphButton(c, icRefresh, "Refresh", "Read the folders again", false, false).Clicked() {
			a.refreshAll()
		}
	})
}

// glyphButton is an icon button of a heading: smaller than the toolbar's,
// as the actions beside a source list's title.
func glyphButton(c *ui.Context, icon *ui.SVG, label, tip string, on, disabled bool) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(tip).Padding(3).Radius(4).Shrink(0).KeepFocus().Disabled(disabled)
	color := colMuted
	if on {
		color = colAccent
	}
	if !disabled && (b.Hovered() || b.Pressed()) {
		b.Background(colPill)
	}
	b.Children(func() { ui.Icon(c, icon).Size(13, 13).TextColor(color) })
	return b
}

// filterBar is the field at the bottom of the navigator, as Xcode's and
// Peek's: it filters the tree, Down moves into it, and Escape clears it.
func (a *app) filterBar(c *ui.Context) {
	ui.Row(c).Shrink(0).FillWidth().Padding(6, 10, 8, 10).BorderWidth(1, 0, 0, 0).BorderColor(colHairline).Children(func() {
		field := ui.SearchField(c, &a.filter.query).FillWidth().Radius(14).Label("Filter Files").Bind(&a.filter.field)
		if a.filter.focus {
			a.filter.focus = false
			a.filter.field.Focus(c)
		}
		if field.Changed() {
			a.filter.forQuery, a.filter.tree, a.filter.roots = "", nil, nil
		}
		if field.Shortcut(0, ui.KeyDown) {
			a.filter.toList = true
		}
	})
}

// retryFiles lists the workspace's files again after listing failed.
func (a *app) retryFiles() {
	a.quick.files, a.quick.err, a.quick.listing = nil, nil, false
	a.filter.forQuery = ""
	a.ensureFiles()
}

func (a *app) fileTree(c *ui.Context, roots []string) {
	a.tree.Indent = 12
	tree := ui.Outline(c, &a.tree, roots, a.children, func(p string) { a.treeItem(c, p) }).Grow(1).FillWidth().Padding(0, 6).Label("Files")
	// Choose the row of the active document once it shows.
	if a.reveal != "" {
		for i := range a.tree.Rows() {
			if a.tree.Item(i) == a.reveal {
				a.treeRow = i
				a.tree.List.ScrollTo(i, ui.Center)
				a.reveal = ""
				break
			}
		}
	}
	// Enter opens the file chosen, or opens or closes the folder; arrows
	// only move the choice.
	if tree.Submitted() && a.treeRow >= 0 && a.treeRow < a.tree.Rows() {
		p := a.tree.Item(a.treeRow)
		if a.isTreeDir(p) {
			a.toggleDir(p)
		} else {
			a.openFromTree(p, false)
		}
	}
}

// The chosen row is already visible, so opening it need not reposition the tree.
func (a *app) openFromTree(path string, preview bool) {
	a.git.diff = nil
	a.git.diffSeq++
	if preview {
		a.previewOpen(path)
	} else {
		a.userOpen(path)
	}
	for i := range a.tree.Rows() {
		if a.tree.Item(i) == path {
			a.treeRow = i
			break
		}
	}
	a.reveal = ""
}

// isTreeDir reports whether a row is a folder, in the tree or in a
// filter's tree.
func (a *app) isTreeDir(p string) bool {
	if a.filtering() {
		return a.filter.tree[p] != nil
	}
	e, ok := a.entry(p)
	return ok && e.Dir
}

func (a *app) toggleDir(p string) {
	if a.tree.Open.Has(p) {
		a.tree.Open.Remove(p)
	} else {
		a.tree.Open.Add(p)
	}
}

func (a *app) treeItem(c *ui.Context, p string) {
	switch {
	case strings.HasSuffix(p, loadingSuffix):
		ui.Text(c, "Loading…").TextColor(colMuted).FontSize(12)
		return
	case strings.HasSuffix(p, errorSuffix):
		dir := strings.TrimSuffix(p, errorSuffix)
		msg := "Could not read this folder"
		if d := a.dirs[dir]; d != nil && d.err != nil {
			msg += ": " + describeErr(d.err)
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, msg).TextColor(colError).FontSize(12).SingleLine().Shrink(1)
			if ui.Button(c, "Retry").Padding(0, 6).Clicked() {
				a.retryDir(dir)
			}
		})
		return
	}
	e, _ := a.entry(p)
	if a.filtering() {
		e = workspace.Entry{Name: filepath.Base(p), Path: p, Dir: a.filter.tree[p] != nil}
	}
	name := e.Name
	if name == "" {
		name = filepath.Base(p)
	}
	dim := (e.Dir && workspace.Ignored(name)) || strings.HasPrefix(name, ".")
	// Extend the hit area into the outline's row padding, keeping the
	// disclosure arrow as a separate target.
	row := ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(6).Margin(-3, 0).MarginEnd(-8).Padding(3, 0)
	row.Children(func() {
		if e.Dir {
			folderGlyph(c, a.tree.Open.Has(p), 16)
		} else {
			fileIcon(c, name, 16)
		}
		t := ui.Text(c, name).SingleLine().Shrink(1)
		if dim {
			t.TextColor(colDim)
		}
		if e.Link {
			t.Italic()
			ui.Text(c, "↗").TextColor(colDim).FontSize(11)
		}
		if d := a.docs[p]; d != nil && d.gone {
			t.Strikethrough()
		}
		if rel, err := filepath.Rel(a.root, p); err == nil {
			status := a.git.decor[filepath.Join(a.realRoot, rel)]
			if status != 0 {
				ui.Box(c).Grow(1)
				ui.Row(c).Padding(0, 20, 0, 0).Shrink(0).Children(func() {
					if e.Dir {
						ui.Box(c).Size(5, 5).Radius(3).Background(a.gitStatusColor(status))
					} else {
						ui.Text(c, string(status)).FontSize(11).TextColor(a.gitStatusColor(status))
					}
				})
			}
		}
	})
	services := c.Services()
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("Copy Path").Chosen() {
			services.WriteClipboard(p)
		}
		if m.Item("Show in Finder").Chosen() {
			mygo.Shell.ShowItemInFolder(p)
		}
		if !e.Dir && m.Item("Open with External Editor").Chosen() {
			_ = mygo.Shell.OpenPath(p)
		}
		if !e.Dir {
			m.Separator()
			if m.Item("Open").Chosen() {
				a.openFromTree(p, false)
			}
		}
	})
	if e.Dir {
		row.OnClick(func() { a.toggleDir(p) })
	} else {
		row.OnClick(func() { a.openFromTree(p, true) })
		if row.DoubleClicked() {
			a.openFromTree(p, true)
			a.focus.preview = ""
		}
		row.Drag(&tabDrag{path: p})
	}
}

func (a *app) dirError(c *ui.Context, dir string, err error) {
	ui.Column(c).Padding(8, 12).Gap(6).Children(func() {
		ui.Text(c, "Could not read this folder: "+describeErr(err)).TextColor(colError)
		if ui.Button(c, "Retry").Clicked() {
			a.retryDir(dir)
		}
	})
}

// findNavigator is Xcode's Find navigator: the query, its options, and
// the matches by file, found with ripgrep.
func (a *app) findNavigator(c *ui.Context) {
	f := &a.find
	ui.Column(c).FillWidth().Padding(8, 10).Gap(6).Children(func() {
		field := ui.SearchField(c, &f.opts.Query).Label("Find in workspace").FillWidth().Bind(&f.field)
		if f.focus {
			f.focus = false
			f.field.Focus(c)
		}
		if field.Changed() {
			a.findSoon()
		}
		if field.Submitted() {
			a.runFind()
		}
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			changed := false
			changed = ui.Toggle(c, &f.opts.CaseSensitive, "Aa").Label("Match Case").Tooltip("Match Case").KeepFocus().Changed() || changed
			changed = ui.Toggle(c, &f.opts.WholeWord, "W").Label("Whole Words").Tooltip("Match Whole Words").KeepFocus().Changed() || changed
			changed = ui.Toggle(c, &f.opts.Regexp, ".*").Label("Regular Expression").Tooltip("Regular Expression").KeepFocus().Changed() || changed
			ui.Box(c).Grow(1)
			ui.Toggle(c, &f.showFilters, "Filters").Tooltip("Files to include and exclude").KeepFocus()
			if changed {
				a.runFind()
			}
		})
		if f.showFilters {
			if ui.TextInput(c, &f.opts.Include).Placeholder("Include: *.go, cmd/**").FillWidth().Changed() {
				a.findSoon()
			}
			if ui.TextInput(c, &f.opts.Exclude).Placeholder("Exclude: *_test.go").FillWidth().Changed() {
				a.findSoon()
			}
		}
		a.findSummary(c)
	})
	if len(f.results) == 0 {
		return
	}
	roots := make([]findKey, len(f.results))
	for i, r := range f.results {
		roots[i] = findKey{path: r.Path, match: -1}
	}
	f.tree.Indent = 6
	list := ui.Outline(c, &f.tree, roots, a.findChildren, func(k findKey) { a.findRow(c, k) }).Grow(1).FillWidth().Padding(0, 6).Label("Find results")
	if list.Submitted() && f.row >= 0 && f.row < f.tree.Rows() {
		a.openMatch(f.tree.Item(f.row))
	}
}

func (a *app) findSummary(c *ui.Context) {
	f := &a.find
	var msg string
	color := colMuted
	switch {
	case f.err != nil && f.err == search.ErrNoRipgrep:
		msg, color = "Finding needs ripgrep: "+search.InstallHint()+".", colError
	case f.err != nil:
		msg, color = f.err.Error(), colError
	case f.running && f.stats.Matches == 0:
		msg = "Searching…"
	case strings.TrimSpace(f.ran.Query) == "":
		return
	case f.stats.Matches == 0 && !f.running:
		msg = "No results"
	default:
		msg = fmt.Sprintf("%d results in %d files", f.stats.Matches, f.stats.Files)
		if f.stats.Truncated {
			msg += " (stopped at the limit)"
		}
	}
	ui.Text(c, msg).FontSize(11).TextColor(color)
}

func (a *app) findRow(c *ui.Context, k findKey) {
	file := a.findFile(k.path)
	if file == nil {
		return
	}
	if k.match < 0 {
		ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Children(func() {
			fileIcon(c, filepath.Base(k.path), 14)
			ui.Text(c, filepath.Base(k.path)).Bold().SingleLine().Shrink(0)
			if dir := filepath.Dir(relPath(a.root, k.path)); dir != "." {
				ui.Text(c, dir).FontSize(11).TextColor(colDim).SingleLine().Shrink(1)
			}
			ui.Box(c).Grow(1)
			ui.Text(c, fmt.Sprint(len(file.Matches))).FontSize(11).TextColor(colMuted).Padding(0, 6).Radius(8).Background(colPill)
		})
		return
	}
	m := file.Matches[k.match]
	row := ui.Row(c).Grow(1).Gap(0).AlignItems(ui.Center)
	row.Children(func() {
		// A text drops the spaces at its ends: the joins get them as room.
		gap := func(n int) {
			if n > 0 {
				ui.Box(c).Width(float32(n) * 3.5).Shrink(0)
			}
		}
		pre, post := m.Preview[:m.PreStart], m.Preview[m.PreEnd:]
		trimmed := strings.TrimRight(pre, " \t")
		ui.Text(c, trimmed).FontSize(12).TextColor(colMuted).SingleLine().Shrink(1)
		gap(len(pre) - len(trimmed))
		ui.Text(c, m.Preview[m.PreStart:m.PreEnd]).FontSize(12).Bold().Background(colMatch).Radius(3).SingleLine().Shrink(0)
		trimmed = strings.TrimLeft(post, " \t")
		gap(len(post) - len(trimmed))
		ui.Text(c, trimmed).FontSize(12).TextColor(colMuted).SingleLine().Shrink(1)
	})
	row.Tooltip(fmt.Sprintf("Line %d", m.Line+1))
	row.OnClick(func() { a.openMatch(k) })
}

// ----- The editor area: groups split as the user arranges them -----

// tabDrag is a tab, or a file of the tree, being dragged: onto a tab bar
// it goes among the tabs, onto an editor's edge it splits the editor.
type tabDrag struct {
	from *group // nil for a file of the tree
	path string
}

func (a *app) editorArea(c *ui.Context) {
	ui.Column(c).Fill().Background(colEditor).Children(func() {
		a.editorsAndPanel(c, func() {
			if a.git.diff != nil {
				a.gitDiffView(c)
			} else {
				ui.Column(c).Fill().Children(func() { a.layoutView(c, a.layout) })
			}
		})
	})
}

func (a *app) layoutView(c *ui.Context, n *layout) {
	if n.g != nil {
		a.groupView(c, n.g)
		return
	}
	first, second := func() { a.layoutView(c, n.a) }, func() { a.layoutView(c, n.b) }
	if n.vertical {
		ui.SplitVerticalPercent(c.Key(n), &n.pct, first, second).Grow(1).FillWidth()
	} else {
		ui.SplitPercent(c.Key(n), &n.pct, first, second).Grow(1).FillHeight()
	}
}

func (a *app) groupView(c *ui.Context, g *group) {
	focused := g == a.focus && len(a.groups()) > 1
	col := ui.Column(c.Key(g)).Grow(1).Fill()
	if focused {
		col.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 2}, colAccent, 0)
		})
	}
	col.Children(func() {
		if len(g.order) > 0 {
			if len(g.order) > 1 {
				a.tabBar(c, g)
			}
			a.jumpBar(c, g)
		}
		if g == a.focus {
			if a.trustAsk {
				a.trustBanner(c)
			}
			if a.choices != nil && a.choices.g == g {
				a.choicesPanel(c)
			}
		}
		a.dropArea(c, g, func() { a.groupContent(c, g) })
		if st := g.eds[g.active]; st != nil {
			pos := st.CursorPos()
			line := st.Buffer.Line(pos.Line)
			column := utf8.RuneCountInString(line[:min(pos.Col, len(line))]) + 1
			ui.Row(c).FillWidth().Height(24).Padding(0, 10).AlignItems(ui.Center).
				BorderWidth(1, 0, 0, 0).BorderColor(colSeparator).Background(colEditor).Children(func() {
				ui.Box(c).Grow(1)
				ui.Text(c, fmt.Sprintf("Line %d  ·  Column %d", pos.Line+1, column)).FontSize(11).TextColor(colMuted).SingleLine()
			})
		}
	})
}

// dropArea takes tabs and files dropped onto an editor: on its edges they
// open in a new editor there, in its middle in this one.
func (a *app) dropArea(c *ui.Context, g *group, content func()) {
	area := ui.Column(c).Grow(1).FillWidth()
	_, over := ui.DragOver[*tabDrag](area)
	zone := sideNone
	if x, y, in := area.PointerPosition(); in || over {
		b := area.LayoutBounds()
		zone = zoneAt(x, y, b.W, b.H)
	}
	if v, ok := ui.Drop[*tabDrag](area); ok {
		a.dropTab(v, g, zone, len(g.order))
	}
	area.Children(func() {
		content()
		if over {
			b := area.LayoutBounds()
			hl := ui.Box(c).Absolute().Background(colDropZone).Radius(6).Border(2, colAccent).PassThrough()
			switch zone {
			case sideLeft:
				hl.Left(4).Top(4).Bottom(4).Width(b.W/2 - 8)
			case sideRight:
				hl.Right(4).Top(4).Bottom(4).Width(b.W/2 - 8)
			case sideTop:
				hl.Left(4).Right(4).Top(4).Height(b.H/2 - 8)
			case sideBottom:
				hl.Left(4).Right(4).Bottom(4).Height(b.H/2 - 8)
			default:
				hl.Left(4).Right(4).Top(4).Bottom(4)
			}
		}
	})
}

// zoneAt says where over an editor of w×h a drop at x, y goes: within a
// quarter of an edge, beside it; elsewhere into the editor.
func zoneAt(x, y, w, h float32) side {
	if w <= 0 || h <= 0 {
		return sideNone
	}
	fx, fy := x/w, y/h
	edge := min(fx, 1-fx, fy, 1-fy)
	if edge > 0.25 {
		return sideNone
	}
	switch edge {
	case fx:
		return sideLeft
	case 1 - fx:
		return sideRight
	case fy:
		return sideTop
	}
	return sideBottom
}

func (a *app) dropTab(v *tabDrag, g *group, s side, at int) {
	a.cancelNav()
	if v.from == nil || !v.from.has(v.path) {
		// A file from the tree.
		target := g
		if s != sideNone {
			target = a.splitBeside(g, s)
		}
		a.openIn(target, v.path, goTo{})
		return
	}
	a.moveTab(v.from, v.path, g, s, at)
}

func (a *app) tabBar(c *ui.Context, g *group) {
	// Names shown by more than one tab get their folder too.
	count := map[string]int{}
	for _, p := range g.order {
		count[filepath.Base(p)]++
	}
	g.tabSel = slices.Index(g.order, g.active)
	tabs := ui.TabsBase(c, &g.tabSel, len(g.order))
	tabs.List.Background(colTabBar).FillWidth().Height(30).AlignItems(ui.Stretch).BorderWidth(0, 0, 1, 0).BorderColor(colHairline).Label("Open files")
	if v, ok := ui.Drop[*tabDrag](tabs.List); ok {
		a.dropTab(v, g, sideNone, len(g.order))
	}
	tabs.List.Children(func() {
		for i, p := range g.order {
			d := a.docs[p]
			if d == nil {
				continue
			}
			active := p == g.active
			tab := tabs.Tab(i).Grow(1).Basis(0).MaxWidth(260).MinWidth(90).Padding(0, 8).Gap(6).
				AlignItems(ui.Center).Justify(ui.Center).BorderWidth(0, 1, 0, 0).BorderColor(colHairline)
			if tab.DoubleClicked() && g.preview == p {
				g.preview = ""
			}
			tab.Drag(&tabDrag{from: g, path: p})
			if v, over := ui.DragOver[*tabDrag](tab); over && v.path != p {
				tab.DrawOver(func(pt *ui.Painter, r ui.Rect) {
					pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: 2, H: r.H}, colAccent, 0)
				})
			}
			if v, ok := ui.Drop[*tabDrag](tab); ok {
				a.dropTab(v, g, sideNone, i)
			}
			hovered := tab.Hovered()
			switch {
			case active:
				tab.Background(colTabActive)
			case hovered:
				tab.Background(colTabHover)
			}
			tab.Tooltip(relPath(a.root, p))
			tab.Children(func() {
				// The close button shows on the left, as Xcode's, while the
				// tab is hovered.
				cl := ui.ButtonBase(c).Label("Close "+d.name).Size(16, 16).Radius(4).Center().KeepFocus()
				if hovered || active {
					cl.Children(func() { ui.Icon(c, icClose).Size(10, 10).TextColor(colMuted) })
					if cl.Hovered() {
						cl.Background(colPill)
					}
				}
				cl.OnClick(func() { a.closeTab(g, p) })
				fileIcon(c, d.name, 13)
				if d.buf != nil && d.buf.Modified() {
					ui.Text(c, "●").FontSize(10).TextColor(colAccent)
				}
				name := ui.Text(c, d.name).FontSize(12).SingleLine().Shrink(1)
				if g.preview == p {
					name.Italic()
				}
				switch {
				case d.gone:
					name.Strikethrough().TextColor(colMuted)
				case !active:
					name.TextColor(colMuted)
				}
				if count[d.name] > 1 {
					ui.Text(c, filepath.Base(filepath.Dir(p))).FontSize(11).TextColor(colDim).SingleLine().Shrink(2)
				}
				if d.loading {
					ui.Spinner(c).FontSize(10)
				}
				ui.Box(c).Size(16, 16).Shrink(0) // balances the close button
			})
		}
	})
	if tabs.List.Changed() && g.tabSel >= 0 && g.tabSel < len(g.order) {
		a.userActivateIn(g, g.order[g.tabSel])
	}
}

// jumpBar is Xcode's jump bar: back and forward, then the path of the
// document, whose folders reveal themselves in the navigator.
func (a *app) jumpBar(c *ui.Context, g *group) {
	p := g.active
	if p == "" {
		return
	}
	ui.Row(c).FillWidth().Height(26).Padding(0, 6).Gap(2).AlignItems(ui.Center).Background(colEditor).
		BorderWidth(0, 0, 1, 0).BorderColor(colSeparator).Children(func() {
		folderGlyph(c, false, 13)
		ui.Text(c, filepath.Base(a.root)).FontSize(12).TextColor(colMuted).SingleLine()
		ui.Icon(c, icChevron).Size(9, 9).TextColor(colDim)
		parts := strings.Split(relPath(a.root, p), "/")
		if !within(a.root, p) {
			parts = []string{p}
		}
		for i, part := range parts {
			if i > 0 {
				ui.Icon(c, icChevron).Size(9, 9).TextColor(colDim)
			}
			last := i == len(parts)-1
			crumb := ui.Row(c.Key(i)).Gap(4).AlignItems(ui.Center).Padding(1, 3).Radius(4)
			crumb.Children(func() {
				if last {
					fileIcon(c, part, 12)
				} else {
					folderGlyph(c, false, 12)
				}
				t := ui.Text(c, part).FontSize(12).SingleLine()
				if !last {
					t.TextColor(colMuted)
				} else {
					if g.preview == p {
						t.Italic()
					}
					if d := a.docs[p]; d != nil && d.buf != nil && d.buf.Modified() {
						ui.Text(c, "●").FontSize(9).TextColor(colAccent)
					}
				}
			})
			if last && crumb.DoubleClicked() && g.preview == p {
				g.preview = ""
			}
			if !last {
				dir := filepath.Join(append([]string{a.root}, parts[:i+1]...)...)
				if crumb.Hovered() {
					crumb.Background(colPill)
				}
				crumb.OnClick(func() {
					a.sidebarOff, a.navigator = false, navFiles
					a.revealInTree(filepath.Join(dir, "x"))
					a.reveal = dir
				})
			}
		}
		if len(g.order) == 1 || isMarkdown(p) {
			ui.Box(c).Grow(1)
		}
		if len(g.order) == 1 {
			if glyphButton(c, icClose, "Close "+filepath.Base(p), "Close File", false, false).Clicked() {
				a.closeTab(g, p)
			}
		}
		if isMarkdown(p) {
			label := "Show Markdown Preview"
			if g.markdownPreview[p] {
				label = "Show Markdown Source"
			}
			if iconButton(c, icMarkdown, label, label+" ("+keyLabel("Cmd+Shift+", "V")+")", g.markdownPreview[p]).Clicked() {
				a.focus = g
				a.toggleMarkdownPreview(g)
			}
		}
	})
}

func (a *app) groupContent(c *ui.Context, g *group) {
	d := a.docs[g.active]
	if d == nil {
		ui.Column(c).Grow(1).FillWidth().Center().Gap(6).Children(func() {
			ui.Text(c, "No Editor").FontSize(20).TextColor(colDim)
			ui.Text(c, "Choose a file in the navigator, or press "+keyLabel("Cmd+Shift+", "O")+" to open one quickly.").FontSize(12).TextColor(colDim)
		})
		return
	}
	a.document(c, g, d)
}

func (a *app) document(c *ui.Context, g *group, d *doc) {
	st := g.eds[d.path]
	switch {
	case d.err != nil:
		a.message(c, "Could not open "+d.name, describeErr(d.err), "Retry", func() { a.reloadDoc(d) })
	case d.loading && st == nil:
		ui.Row(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "Loading "+d.name+"…").TextColor(colMuted)
		})
	case d.image != nil:
		a.imagePreview(c, d)
	case d.kind == workspace.Binary:
		a.message(c, d.name+" is a binary file", fmt.Sprintf("It is not shown as source (%s).", size(d.size)), "", nil)
	case d.kind == workspace.TooLarge:
		a.message(c, d.name+" is too large to show",
			fmt.Sprintf("It is %s; files up to %s open here.", size(d.size), size(workspace.MaxSize)), "", nil)
	case d.kind == workspace.BadEncoding:
		a.message(c, d.name+" is not UTF-8 text",
			"Its text uses another encoding, or it is not text.", "Show with Replacement Characters", func() {
				d.lossy = true
				a.reloadDoc(d)
			})
	case st != nil:
		if v := d.buf.Version(); v != d.lspVersion {
			a.invalidateHover()
			d.lspVersion = v
			if m := a.serverFor(d.path); m != nil {
				m.changed(d.uri, d.buf.Snapshot())
			}
			if d.buf.Modified() && g.preview == d.path {
				g.preview = ""
			}
		}
		if d.conflict {
			a.banner(c, colWarnBg, "This file changed on disk. Your edits are preserved.", []action{{"Overwrite", func() { a.saveDoc(d, true) }}})
		}
		if d.gone {
			a.banner(c, colWarnBg, "This file was deleted or moved on disk. The text below is how it was.", []action{
				{"Reload", func() { a.reloadDoc(d) }},
				{"Close", func() { a.closeTab(g, d.path) }},
			})
		}
		if d.plain || d.lossy {
			msg := "Large file: syntax highlighting is off."
			if d.lossy {
				msg = "Bytes that are not UTF-8 show as �. This file is read-only to protect its encoding."
			}
			ui.Text(c, msg).FontSize(11).TextColor(colMuted).Padding(3, 12).FillWidth().Background(colTabBar)
		}
		if isMarkdown(d.path) && g.markdownPreview[d.path] {
			a.markdownView(c, g, d)
			return
		}
		ed := editor.View(c.Key(d.path), st).Grow(1).FillWidth()
		switch {
		case a.focusEditor == st:
			// A navigation went here: the keys read on from it.
			ed.Focus()
			a.focusEditor = nil
		case g == a.focus && !a.term.open:
			ed.AutoFocus()
		}
		if ed.FocusWithin() && a.focus != g {
			a.focus = g // the editor clicked in is the one keys go to
		}
	}
}

type action struct {
	label string
	fn    func()
}

func (a *app) banner(c *ui.Context, bg ui.Color, msg string, actions []action) {
	ui.Row(c).FillWidth().Padding(6, 12).Gap(8).AlignItems(ui.Center).Background(bg).Children(func() {
		ui.Text(c, msg).FontSize(12).Grow(1).Shrink(1)
		for _, ac := range actions {
			ui.Button(c, ac.label).OnClick(ac.fn)
		}
	})
}

func (a *app) message(c *ui.Context, title, detail, button string, fn func()) {
	ui.Column(c).Grow(1).FillWidth().Center().Gap(8).Padding(24).Children(func() {
		ui.Text(c, title).FontSize(16).Bold()
		ui.Text(c, detail).TextColor(colMuted).MaxWidth(520).TextAlign(ui.Center)
		if button != "" {
			ui.Button(c, button).OnClick(fn)
		}
	})
}

func (a *app) trustBanner(c *ui.Context) {
	a.banner(c, colInfoBg,
		"Go to Definition and symbol help run language servers in “"+filepath.Base(a.root)+"”. Servers may run project tools and download dependencies or toolchains. Reading works without them.",
		[]action{{"Trust Folder", a.trust}, {"Not Now", func() { a.trustAsk, a.afterTrust = false, nil; a.trustDismissed = true }}})
}

func (a *app) choicesPanel(c *ui.Context) {
	ch := a.choices
	ui.Column(c).FillWidth().Padding(6, 12).Gap(2).Background(colTabBar).BorderWidth(0, 0, 1, 0).BorderColor(colSeparator).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("%d definitions", len(ch.locs))).Bold().Grow(1)
			ui.Button(c, "Cancel").OnClick(func() { a.choices = nil })
		})
		for i, t := range ch.locs {
			label := fmt.Sprintf("%s:%d", relPath(a.root, t.path), t.pos.Line+1)
			b := ui.Button(c.Key(i), label).FillWidth()
			if i == 0 {
				b.AutoFocus()
			}
			b.OnClick(func() { a.jump(ch.g, ch.from, t, ch.enc) })
		}
	})
}

// ----- Open Quickly -----

// quickOpenPanel searches files, or commands when the query starts with >.
func (a *app) quickOpenPanel(c *ui.Context) {
	q := &a.quick
	a.rankQuick()
	if c.Shortcut(0, ui.KeyEscape) {
		q.open = false
		return
	}
	ui.Overlay(c, func() {
		backdrop := ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).AlignItems(ui.Center).Background(ui.RGBA(0, 0, 0, 0.18))
		backdrop.OnClick(func() { q.open = false })
		backdrop.Children(func() {
			panel := ui.Column(c).Margin(90, 0, 0, 0).Width(640).MaxHeight(460).Radius(14).Padding(8).Gap(6).
				Material(glass.Glass{}).Border(1, colPopoverEdge).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.5))
			panel.OnClick(func() {}) // clicks on the panel stay on it
			panel.Children(func() {
				ui.Row(c).Padding(4, 8).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icSearch).Size(20, 20).TextColor(colMuted)
					in := ui.TextInput(c, &q.query).Placeholder("Open Quickly").FontSize(20).Grow(1).AutoFocus().Background(ui.Transparent).BorderWidth(0)
					// The field has the keyboard: it takes the list's keys.
					switch {
					case in.Submitted():
						a.chooseQuick(q.sel)
					case in.Shortcut(0, ui.KeyEscape):
						q.open = false
					case in.Shortcut(0, ui.KeyDown):
						q.sel = min(q.sel+1, len(q.results)-1)
						q.list.ScrollTo(q.sel, ui.Center)
					case in.Shortcut(0, ui.KeyUp):
						q.sel = max(q.sel-1, 0)
						q.list.ScrollTo(q.sel, ui.Center)
					}
				})
				commandMode := strings.HasPrefix(q.query, ">")
				switch {
				case !commandMode && a.root == "":
					ui.Text(c, "Open a folder to search files, or type > to search commands.").TextColor(colMuted).Padding(8)
				case !commandMode && q.listing:
					ui.Row(c).Padding(8).Gap(8).Children(func() {
						ui.Spinner(c)
						ui.Text(c, "Listing files…").TextColor(colMuted)
					})
				case !commandMode && q.err != nil:
					ui.Text(c, q.err.Error()).TextColor(colError).Padding(8)
				case len(q.results) == 0:
					ui.Text(c, "No Matches").TextColor(colMuted).Padding(8)
				default:
					ui.Box(c).Height(1).FillWidth().Background(colSeparator)
					label := "Files"
					if commandMode {
						label = "Commands"
					}
					ui.List(c, &q.list, len(q.results), func(i int) { a.quickRow(c, i) }).Grow(1).FillWidth().MaxHeight(380).Label(label)
				}
			})
		})
	})
}

func (a *app) quickRow(c *ui.Context, i int) {
	q := &a.quick
	r := q.results[i]
	if strings.HasPrefix(q.query, ">") {
		row := ui.Row(c.Key(r.Path)).FillWidth().Padding(8).Gap(8).Radius(8).AlignItems(ui.Center)
		if i == q.sel {
			row.Background(colAccent)
		}
		row.OnClick(func() { a.chooseQuick(i) })
		row.Children(func() {
			ui.Text(c, r.Path).Grow(1).SingleLine()
			for _, cmd := range commands() {
				if cmd.name == r.Path && cmd.key != "" {
					ui.Text(c, keyLabel(cmd.mods, cmd.key)).TextColor(colMuted)
					break
				}
			}
		})
		return
	}
	dir, name := filepath.Split(filepath.FromSlash(r.Path))
	row := ui.Row(c).FillWidth().Padding(5, 8).Gap(10).AlignItems(ui.Center).Radius(8)
	if i == q.sel {
		row.Background(colAccent)
	}
	row.OnClick(func() { a.chooseQuick(i) })
	row.Children(func() {
		fileIcon(c, name, 22)
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			nameStart := len(r.Path) - len(name)
			highlightText(c, name, r.Hits, nameStart, i == q.sel)
			sub := ui.Text(c, strings.TrimSuffix(filepath.ToSlash(dir), "/")).FontSize(11).SingleLine()
			if i == q.sel {
				sub.TextColor(ui.RGBA(0xff, 0xff, 0xff, 0.75))
			} else {
				sub.TextColor(colMuted)
			}
		})
	})
}

// highlightText shows name with the characters the query matched bold,
// hits being byte offsets of the whole path and name starting at off.
func highlightText(c *ui.Context, name string, hits []int, off int, selected bool) {
	color := colText
	if selected {
		color = ui.RGB(0xff, 0xff, 0xff)
	}
	ui.Row(c).Children(func() {
		run, bold := "", false
		flush := func() {
			if run == "" {
				return
			}
			t := ui.Text(c, run).FontSize(13).TextColor(color).SingleLine()
			if bold {
				t.Bold().Underline()
			}
			run = ""
		}
		for i, r := range name {
			hit := slices.Contains(hits, off+i)
			if hit != bold {
				flush()
				bold = hit
			}
			run += string(r)
		}
		flush()
	})
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 80 {
		s = s[:77] + "…"
	}
	return s
}

func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}
