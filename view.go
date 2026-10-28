package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"nsr/internal/search"
	"nsr/internal/workspace"
)

// editorBindings take from the editor the keys that are the app's.
func editorBindings() []editor.Binding {
	none := func(m ui.Modifiers, k ui.Key) editor.Binding {
		return editor.Binding{Mods: m, Key: k, Command: editor.NoCommand}
	}
	bs := []editor.Binding{
		none(ui.Cmd, ui.KeyW),
		none(ui.Cmd, ui.KeyO),
		none(ui.Cmd, ui.KeyP),
		none(ui.Cmd|ui.Shift, ui.KeyO),
		none(ui.Cmd|ui.Shift, ui.KeyF),
		none(ui.Cmd, ui.KeyBackslash),
		none(ui.Cmd, ui.KeyD), // split, as iTerm; the reader selects no next occurrence to edit
		none(ui.Cmd|ui.Shift, ui.KeyD),
		none(ui.Ctrl, ui.KeyTab),
		none(ui.Ctrl|ui.Shift, ui.KeyTab),
		none(ui.Ctrl, ui.KeyMinus),
		none(ui.Ctrl|ui.Shift, ui.KeyMinus),
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

var startTrace = os.Getenv("NSR_TRACE_STARTUP") != ""

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
	ui.Column(c).Fill().Children(func() {
		a.toolbar(c, vibrant)
		ui.Row(c).Grow(1).FillWidth().AlignItems(ui.Stretch).Children(func() {
			if a.sidebarOff {
				a.editorArea(c)
				return
			}
			if ui.Split(c, &a.sidebarW, func() { a.navigatorPane(c, vibrant) }, func() { a.editorArea(c) }).Grow(1).FillHeight().Changed() {
				// Changed holds for every frame of a drag: save once it stops.
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
	case c.Shortcut(ui.Cmd, ui.KeyO):
		a.openFolderDialog()
	case c.Shortcut(ui.Cmd, ui.KeyComma) && a.openSettings != nil:
		a.openSettings()
	case c.Shortcut(ui.Cmd|ui.Shift, ui.Key1):
		a.showWelcome = true
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyO), c.Shortcut(ui.Cmd, ui.KeyP):
		a.openQuickly()
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyF):
		a.sidebarOff, a.navigator = false, navFind
	case c.Shortcut(ui.Cmd, ui.Key1):
		a.sidebarOff, a.navigator = false, navFiles
	case c.Shortcut(ui.Cmd, ui.Key0):
		a.sidebarOff = !a.sidebarOff
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyY), c.Shortcut(ui.Ctrl, ui.KeyBackquote):
		a.toggleTerminal() // Xcode's and VS Code's keys
	case c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyBackquote):
		a.newTerminalTab()
	case c.Shortcut(ui.Cmd, ui.KeyBackslash), c.Shortcut(ui.Cmd, ui.KeyD):
		if inTerm {
			a.splitTerminal()
		} else {
			a.splitActive(sideRight)
		}
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyBackslash), c.Shortcut(ui.Cmd|ui.Shift, ui.KeyD):
		a.splitActive(sideBottom)
	case !inTerm && c.Shortcut(ui.Cmd, ui.KeyW):
		if p := a.activePath(); p != "" {
			a.closeDoc(p)
		}
	}
	if inTerm {
		return
	}
	back := c.Shortcut(ui.Ctrl, ui.KeyMinus) || c.Shortcut(ui.Ctrl|ui.Cmd, ui.KeyLeft)
	fwd := c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyMinus) || c.Shortcut(ui.Ctrl|ui.Cmd, ui.KeyRight)
	next := c.Shortcut(ui.Ctrl, ui.KeyTab)
	prev := c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyTab)
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
		// The editor opens its find bar itself while it has the focus;
		// from elsewhere, open the active document's.
		if st := a.activeEditor(); st != nil {
			st.Find(st.SelectedText())
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
	b := ui.ButtonBase(c).Label(label).Tooltip(tip).Padding(5).Radius(6).KeepFocus()
	color := colMuted
	if on {
		color = colAccent
	}
	if b.Hovered() {
		b.Background(colPill)
	}
	b.Children(func() { ui.Icon(c, icon).Size(16, 16).TextColor(color) })
	return b
}

// ----- Toolbar -----

// toolbarHeight is the title bar's height, the toolbar's: compact, as
// Xcode's in its smaller size.
const toolbarHeight = 38

// toolbar is the window's unified title bar and toolbar, as Xcode's: the
// navigator's button, the folder's name, the activity view in the middle
// and the editor's buttons.
func (a *app) toolbar(c *ui.Context, vibrant bool) {
	bar := c.TitleBar()
	row := ui.Row(c).FillWidth().Height(max(bar.Height, toolbarHeight)).Padding(0, bar.Right+8, 0, max(bar.Left, 8)+4).Gap(4).
		AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(colHairline).DragWindow()
	if !vibrant {
		row.Background(colToolbar)
	}
	row.Children(func() {
		if iconButton(c, icSidebar, "Show Navigator", "Hide or Show the Navigator ("+keyLabel("Cmd+", "0")+")", false).Clicked() {
			a.sidebarOff = !a.sidebarOff
		}
		ui.Text(c, filepath.Base(a.root)).Bold().SingleLine().MarginStart(4).Width(max(0, min(200, a.sidebarW-110))).Tooltip(tildePath(a.root))
		ui.Box(c).Grow(1)
		a.activityView(c)
		ui.Box(c).Grow(1)
		if iconButton(c, icSearch, "Open Quickly", "Open Quickly ("+keyLabel("Cmd+Shift+", "O")+")", false).Clicked() {
			a.openQuickly()
		}
		if iconButton(c, icSplitH, "Split Right", "Add Editor on Right ("+keyLabel("Cmd+", "D")+")", false).Clicked() {
			a.splitActive(sideRight)
		}
		if iconButton(c, icSplitV, "Split Down", "Add Editor Below ("+keyLabel("Cmd+Shift+", "D")+")", false).Clicked() {
			a.splitActive(sideBottom)
		}
		if iconButton(c, icPanel, "Show Terminal", "Hide or Show the Terminal ("+keyLabel("Cmd+Shift+", "Y")+")", a.term.open).Clicked() {
			a.toggleTerminal()
		}
	})
}

// activityView is the pill in the middle of the toolbar, as Xcode's: what
// the app is doing, and what went wrong.
func (a *app) activityView(c *ui.Context) {
	pill := ui.Row(c).Height(24).MinWidth(240).MaxWidth(520).Shrink(1).Padding(0, 10).Gap(8).Radius(6).
		AlignItems(ui.Center).Background(colPill)
	pill.Children(func() {
		ui.Text(c, filepath.Base(a.root)).FontSize(12).Bold().SingleLine().Shrink(0)
		ui.Text(c, "│").FontSize(12).TextColor(colDim)
		switch {
		case a.finding:
			ui.Spinner(c).FontSize(11)
			ui.Text(c, "Finding definition…").FontSize(12).TextColor(colMuted).SingleLine()
		case a.notice != "":
			t := ui.Text(c, a.notice).FontSize(12).SingleLine().Shrink(1).Tooltip(a.notice)
			if a.noticeError {
				t.TextColor(colError)
			}
			if a.lspState == lspFailed && ui.Button(c, "Retry").Label("Retry gopls").Padding(0, 6).FontSize(11).Clicked() {
				a.retryLSP()
			}
		case a.find.running:
			ui.Spinner(c).FontSize(11)
			ui.Text(c, "Searching…").FontSize(12).TextColor(colMuted)
		case a.lspState == lspStarting:
			ui.Spinner(c).FontSize(11)
			ui.Text(c, "Starting gopls…").FontSize(12).TextColor(colMuted)
		case a.lspState == lspFailed:
			msg := "gopls unavailable"
			if a.lspErr != nil {
				msg = "gopls: " + shortErr(a.lspErr)
			}
			ui.Text(c, msg).FontSize(12).TextColor(colError).SingleLine().Shrink(1)
			if ui.Button(c, "Retry").Label("Retry gopls").Padding(0, 6).FontSize(11).Clicked() {
				a.retryLSP()
			}
		case a.watchErr != nil:
			ui.Text(c, "Not watching for changes").FontSize(12).TextColor(colMuted).Tooltip(a.watchErr.Error())
		case a.lspState == lspReady:
			ui.Text(c, "Ready · gopls").FontSize(12).TextColor(colMuted)
		default:
			ui.Text(c, "Ready").FontSize(12).TextColor(colMuted)
		}
	})
}

// ----- Navigators -----

// navigatorPane is the sidebar, over the window's material where there
// is one: the navigator selector bar, then the navigator chosen.
func (a *app) navigatorPane(c *ui.Context, vibrant bool) {
	pane := ui.Column(c).Fill()
	if !vibrant {
		pane.Background(colSidebar)
	}
	pane.Children(func() {
		ui.Row(c).FillWidth().Padding(2, 8).Gap(2).Justify(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(colHairline).Children(func() {
			if iconButton(c, icFolder, "Project Navigator", "Show the Project Navigator ("+keyLabel("Cmd+", "1")+")", a.navigator == navFiles).Clicked() {
				a.navigator = navFiles
			}
			if iconButton(c, icSearch, "Find Navigator", "Show the Find Navigator ("+keyLabel("Cmd+Shift+", "F")+")", a.navigator == navFind).Clicked() {
				a.navigator = navFind
			}
		})
		if a.navigator == navFind {
			a.findNavigator(c)
		} else {
			a.projectNavigator(c)
		}
	})
}

func (a *app) projectNavigator(c *ui.Context) {
	ui.Row(c).Padding(8, 12, 4, 12).AlignItems(ui.Center).Gap(6).Children(func() {
		folderGlyph(c, true, 15)
		ui.Text(c, filepath.Base(a.root)).Bold().SingleLine().Grow(1).Tooltip(a.root)
		if iconButton(c, icRefresh, "Refresh", "Read the folders again", false).Clicked() {
			a.refreshAll()
		}
	})
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

func (a *app) fileTree(c *ui.Context, roots []string) {
	a.tree.Indent = 14
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
		if e, ok := a.entry(p); ok && e.Dir {
			a.toggleDir(p)
		} else if ok {
			a.userOpen(p)
		}
	}
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
	name := e.Name
	if name == "" {
		name = filepath.Base(p)
	}
	dim := (e.Dir && workspace.Ignored(name)) || strings.HasPrefix(name, ".")
	row := ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(6)
	row.Children(func() {
		if e.Dir {
			folderGlyph(c, a.tree.Open.Has(p), 15)
		} else {
			fileIcon(c, name, 15)
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
	})
	if e.Dir {
		row.OnClick(func() { a.toggleDir(p) })
	} else {
		row.OnClick(func() { a.userOpen(p) })
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
		field := ui.SearchField(c, &f.opts.Query).Label("Find in workspace").FillWidth()
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
			ui.Column(c).Fill().Children(func() { a.layoutView(c, a.layout) })
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
			a.tabBar(c, g)
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
				name := ui.Text(c, d.name).FontSize(12).SingleLine().Shrink(1)
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
		back := iconButton(c, icBack, "Go Back", "Go Back", false).Padding(3).Disabled(len(a.hist.Back) == 0)
		if back.Clicked() {
			a.focus = g
			a.goBack()
		}
		fwd := iconButton(c, icForward, "Go Forward", "Go Forward", false).Padding(3).Disabled(len(a.hist.Forward) == 0)
		if fwd.Clicked() {
			a.focus = g
			a.goForward()
		}
		ui.Box(c).Width(6)
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
				}
			})
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
		if d.gone {
			a.banner(c, colWarnBg, "This file was deleted or moved on disk. The text below is how it was.", []action{
				{"Reload", func() { a.reloadDoc(d) }},
				{"Close", func() { a.closeTab(g, d.path) }},
			})
		}
		if d.plain || d.lossy {
			msg := "Large file: syntax highlighting is off."
			if d.lossy {
				msg = "Bytes that are not UTF-8 show as �."
			}
			ui.Text(c, msg).FontSize(11).TextColor(colMuted).Padding(3, 12).FillWidth().Background(colTabBar)
		}
		ed := editor.View(c.Key(d.path), st).Grow(1).FillWidth()
		switch {
		case a.focusEditor == st:
			// A navigation went here: the keys read on from it.
			ed.Focus()
			a.focusEditor = nil
		case g == a.focus:
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
		"Go to Definition and symbol help run gopls in “"+filepath.Base(a.root)+"”. gopls loads the code with the go command, which can download modules and toolchains. Reading works without it.",
		[]action{{"Trust Folder", a.trust}, {"Not Now", func() { a.trustAsk, a.afterTrust = false, nil }}})
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

// quickOpenPanel is Xcode's Open Quickly: a field and the files it
// matches, on a pane of glass over the window.
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
				switch {
				case q.listing:
					ui.Row(c).Padding(8).Gap(8).Children(func() {
						ui.Spinner(c)
						ui.Text(c, "Listing files…").TextColor(colMuted)
					})
				case q.err != nil:
					ui.Text(c, q.err.Error()).TextColor(colError).Padding(8)
				case len(q.results) == 0:
					ui.Text(c, "No Matches").TextColor(colMuted).Padding(8)
				default:
					ui.Box(c).Height(1).FillWidth().Background(colSeparator)
					ui.List(c, &q.list, len(q.results), func(i int) { a.quickRow(c, i) }).Grow(1).FillWidth().MaxHeight(380).Label("Files")
				}
			})
		})
	})
}

func (a *app) quickRow(c *ui.Context, i int) {
	q := &a.quick
	r := q.results[i]
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
