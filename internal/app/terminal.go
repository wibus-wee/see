package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"see/internal/config"
	"see/internal/workspace"
	"see/locales"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// The bottom panel: terminals, in tabs as VS Code has them, each tab
// split into terminals side by side, where Xcode keeps its debug area.
// They run with MyGo's terminal plugin, Ghostty's emulator.

type panelView uint8

const (
	panelTerminal panelView = iota
	panelOutput
	panelTasks
	panelReferences
	panelCalls
)

// termPanel is the bottom panel's state.
type termPanel struct {
	open      bool
	view      panelView
	maximized bool
	tabs      []*termTab
	active    *termTab
	pct       float32 // the editors' share of the height, as a percentage
	nextID    int
	loading   bool  // the emulator's library is loading
	loadErr   error // it could not
	focus     *termPane
	focused   bool // a terminal had the keyboard in the last frame
}

// termTab is a tab of the panel: terminals side by side.
type termTab struct {
	id     int
	panes  []*termPane
	active *termPane
}

// termPane is one terminal.
type termPane struct {
	id     int
	term   *terminal.Terminal
	title  string
	handle ui.Handle
	pct    float32 // its share of the room it splits with those after it
}

// newTerm makes a terminal running the user's shell in dir, or in the
// workspace when dir is empty or gone. Its callbacks come from the
// terminal's goroutines: they go through post, and are dropped once the
// pane or the workspace is gone.
func (a *App) newTerm(p *termPane, dir string) error {
	gen := a.gen
	var t *terminal.Terminal
	t, err := terminal.New(terminal.Options{
		Dir: a.termDir(dir),
		Command: func() []string {
			if a.cfg.TermShell == "" {
				return nil
			}
			return []string{a.cfg.TermShell}
		}(),
		Font: a.terminalFont(),
		Cursor: func() terminal.CursorStyle {
			switch a.cfg.TermCursor {
			case "bar":
				return terminal.CursorBar
			case "underline":
				return terminal.CursorUnderline
			}
			return terminal.CursorBlock
		}(),
		NoBlink:     a.cfg.TermCursorNoBlink,
		OptionAsAlt: a.cfg.TermOptionAsAlt,
		CopyOnSelect: map[bool]terminal.CopyOnSelect{
			true: terminal.CopyOnSelectClipboard,
		}[a.cfg.TermCopyOnSelect],
		RightClick: func() terminal.RightClickAction {
			switch a.cfg.TermRightClick {
			case "paste":
				return terminal.RightClickPaste
			case "copyOrPaste":
				return terminal.RightClickCopyOrPaste
			}
			return terminal.RightClickContextMenu
		}(),
		ClipboardRead: func() terminal.ClipboardAccess {
			switch a.cfg.TermClipboard {
			case "allow":
				return terminal.ClipboardAllow
			case "deny":
				return terminal.ClipboardDeny
			}
			return terminal.ClipboardAsk
		}(),
		Scrollback: func() int {
			v := a.cfg.TermScrollback
			if v == 0 {
				v = 4
			}
			if v < 0 {
				return -1
			}
			return v << 20
		}(),
		Env:             a.cfg.TermEnv,
		MinimumContrast: float32(a.cfg.TermMinContrast),
		OnTitle: func(title string) {
			a.post.Post(func() {
				if a.gen == gen {
					p.title = title
				}
			})
		},
		OnExit: func(int) {
			a.post.Post(func() {
				if a.gen == gen {
					a.closePane(p) // as VS Code closes a terminal whose shell ended
				}
			})
		},
		OnBell: func() {
			a.post.Post(func() {
				if a.gen == gen {
					a.notifyTerminal(p.title, "")
				}
			})
		},
		OnNotify: func(title, body string) {
			a.post.Post(func() {
				if a.gen == gen {
					a.notifyTerminal(title, body)
				}
			})
		},
		OpenLink: func(link string) bool { return a.openTermLink(p, link) },
	})
	if err != nil {
		return err
	}
	p.term = t
	return nil
}

// withTerminal runs fn once the emulator's library is loaded, which may
// download it the first time under go run and go test, off the UI
// thread; packaged apps carry it.
func (a *App) withTerminal(fn func()) {
	tp := &a.term
	if tp.loadErr != nil || tp.loading {
		return
	}
	if termLoaded {
		fn()
		return
	}
	tp.loading = true
	gen := a.gen
	go func() {
		err := terminal.Load()
		a.post.Post(func() {
			tp.loading = false
			if err != nil {
				tp.loadErr = err
				return
			}
			termLoaded = true
			if a.gen == gen {
				fn()
			}
		})
	}()
}

var termLoaded bool

// toggleTerminal shows or hides the panel, with a terminal in it.
func (a *App) toggleTerminal() {
	tp := &a.term
	if tp.open {
		tp.open, tp.maximized = false, false
		tp.focused = false
		tp.focus = nil
		if a.focus != nil {
			a.focusEditor = a.focus.eds[a.focus.active]
		}
		return
	}
	tp.open = true
	tp.view = panelTerminal
	a.focusEditor = nil
	if len(tp.tabs) == 0 {
		a.newTerminalTab()
	} else if tp.active != nil {
		tp.focus = tp.active.active
	}
}

// newTerminalTab opens a tab with a new terminal at the workspace root.
func (a *App) newTerminalTab() { a.newTerminalTabAt(a.root) }

// newTerminalTabAt opens a tab with a new terminal in dir.
func (a *App) newTerminalTabAt(dir string) {
	if dir == "" {
		return
	}
	a.term.open = true
	a.term.view = panelTerminal
	a.focusEditor = nil
	a.withTerminal(func() {
		tp := &a.term
		tp.nextID++
		p := &termPane{id: tp.nextID, title: a.shellTitle()}
		if err := a.newTerm(p, dir); err != nil {
			a.note(locales.MsgCouldNotStartATerminal()+err.Error(), true)
			return
		}
		tab := &termTab{id: tp.nextID, panes: []*termPane{p}, active: p}
		tp.tabs = append(tp.tabs, tab)
		tp.active, tp.focus = tab, p
	})
}

// splitTerminal adds a terminal beside the active one of the tab.
func (a *App) splitTerminal() {
	tp := &a.term
	tab := tp.active
	if tab == nil {
		a.newTerminalTab()
		return
	}
	a.withTerminal(func() {
		tp.nextID++
		p := &termPane{id: tp.nextID, title: a.shellTitle()}
		if err := a.newTerm(p, a.paneDir(tab.active)); err != nil {
			a.note(locales.MsgCouldNotStartATerminal()+err.Error(), true)
			return
		}
		at := slices.Index(tab.panes, tab.active) + 1
		tab.panes = slices.Insert(tab.panes, at, p)
		// Split the room evenly.
		for i, q := range tab.panes {
			q.pct = 100 / float32(len(tab.panes)-i)
		}
		tab.active, tp.focus = p, p
		tp.open = true
	})
}

// closePane closes a terminal: its tab goes with its last terminal, and
// the panel with its last tab.
func (a *App) closePane(p *termPane) {
	tp := &a.term
	for ti, tab := range tp.tabs {
		i := slices.Index(tab.panes, p)
		if i < 0 {
			continue
		}
		if p.term != nil {
			p.term.Close()
		}
		tab.panes = slices.Delete(tab.panes, i, i+1)
		for j, q := range tab.panes {
			q.pct = 100 / float32(len(tab.panes)-j)
		}
		if len(tab.panes) == 0 {
			tp.tabs = slices.Delete(tp.tabs, ti, ti+1)
			if tp.active == tab {
				tp.active = nil
				if n := len(tp.tabs); n > 0 {
					tp.active = tp.tabs[min(ti, n-1)]
				}
			}
		} else if tab.active == p {
			tab.active = tab.panes[min(i, len(tab.panes)-1)]
		}
		if tp.focus == p {
			tp.focus = nil
			if tp.active != nil {
				tp.focus = tp.active.active
			} else if a.focus != nil {
				a.focusEditor = a.focus.eds[a.focus.active]
			}
		}
		if len(tp.tabs) == 0 {
			tp.open, tp.maximized = false, false
		}
		return
	}
}

// termSession is what of the panel a session keeps: each tab's shells,
// their directories and titles, which tab was active, and whether the
// panel showed terminals at all.
func (a *App) termSession() (tabs []config.TermTab, active int, open bool) {
	tp := &a.term
	if tp.view != panelTerminal {
		return nil, 0, false
	}
	for _, tab := range tp.tabs {
		saved := config.TermTab{}
		for j, p := range tab.panes {
			sp := config.TermPane{Title: p.title}
			if p.term != nil {
				sp.Dir = p.term.Dir()
			}
			if tab.active == p {
				saved.Active = j
			}
			saved.Panes = append(saved.Panes, sp)
		}
		if len(saved.Panes) == 0 {
			continue
		}
		if tp.active == tab {
			active = len(tabs)
		}
		tabs = append(tabs, saved)
	}
	if len(tabs) == 0 {
		return nil, 0, false
	}
	return tabs, active, tp.open
}

// openRestoredTerminals recreates the panel's tabs of the last session:
// a fresh shell at each one's directory, with its title. The first
// failure ends the restore; terminals opened before it stay.
func (a *App) openRestoredTerminals(s config.Workspace) {
	if len(s.Terminals) == 0 || a.root == "" {
		return
	}
	tp := &a.term
	tp.open, tp.view = s.PanelOpen, panelTerminal
	tp.focus = nil
	a.withTerminal(func() {
		if a.root == "" || len(tp.tabs) > 0 {
			return
		}
		for i, saved := range s.Terminals {
			tp.nextID++
			tab := &termTab{id: tp.nextID}
			for j, sp := range saved.Panes {
				tp.nextID++
				p := &termPane{id: tp.nextID, title: sp.Title}
				if err := a.newTerm(p, sp.Dir); err != nil {
					a.logf("session: restoring terminal: %v", err)
					tp.open = s.PanelOpen && len(tp.tabs) > 0
					return
				}
				tab.panes = append(tab.panes, p)
				if j == saved.Active {
					tab.active = p
				}
			}
			if len(tab.panes) == 0 {
				continue
			}
			if tab.active == nil {
				tab.active = tab.panes[0]
			}
			for j, p := range tab.panes {
				p.pct = 100 / float32(len(tab.panes)-j)
			}
			tp.tabs = append(tp.tabs, tab)
			if i == s.ActiveTerminal {
				tp.active = tab
			}
		}
		if tp.active == nil && len(tp.tabs) > 0 {
			tp.active = tp.tabs[0]
		}
		if len(tp.tabs) == 0 {
			tp.open = false
		}
	})
}

// closeTerminals ends every terminal, as the workspace closes.
func (a *App) closeTerminals() {
	for _, tab := range a.term.tabs {
		for _, p := range tab.panes {
			if p.term != nil {
				p.term.Close()
			}
		}
	}
	pct := a.term.pct
	a.term = termPanel{pct: pct, loadErr: a.term.loadErr}
}

// termDir is where a restored or new terminal starts: dir while it
// still is a directory of the workspace, the workspace's root else.
func (a *App) termDir(dir string) string {
	if dir == "" || a.root == "" || !workspace.Within(a.root, dir) {
		return a.root
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return a.root
	}
	return dir
}

// paneDir is the working directory the pane's shell reported (OSC 7),
// or the workspace's root when the shell reported none, as one not
// running yet. A split starts where the split pane runs.
func (a *App) paneDir(p *termPane) string {
	if p != nil && p.term != nil && p.term.Dir() != "" {
		return p.term.Dir()
	}
	return a.root
}

// shellTitle is a new pane's title before its shell's first OSC title:
// the name of the shell that will run.
func (a *App) shellTitle() string {
	if a.cfg.TermShell != "" {
		return filepath.Base(a.cfg.TermShell)
	}
	return shellName()
}

func shellName() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return filepath.Base(sh)
	}
	return "shell"
}

// termLink matches a path printed with a line, and a column: main.go:12:3.
var termLink = regexp.MustCompile(`^(.+?)(?::(\d+))?(?::(\d+))?$`)

// openTermLink opens a file path a terminal printed, which Cmd+click
// opens, in the editor, at its line and column; other links open with
// the system. It runs on the UI thread.
func (a *App) openTermLink(p *termPane, link string) bool {
	if strings.Contains(link, "://") {
		return false
	}
	m := termLink.FindStringSubmatch(link)
	if m == nil {
		return false
	}
	path := m[1]
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	if !filepath.IsAbs(path) {
		dir := a.root
		if p.term != nil && p.term.Dir() != "" {
			dir = p.term.Dir()
		}
		path = filepath.Join(dir, path)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	var pos *editor.Pos
	if m[2] != "" {
		line, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		pos = &editor.Pos{Line: max(line-1, 0), Col: max(col-1, 0)}
	}
	a.cancelNav()
	a.openFile(path, pos)
	if st := a.activeEditor(); st != nil {
		a.focusEditor = st
	}
	return true
}

// ----- View -----

// editorsAndPanel lays out the editors over the bottom panel, which the
// divider between them resizes, as Xcode's debug area.
func (a *App) editorsAndPanel(c *ui.Context, editors func()) {
	tp := &a.term
	if tp.pct <= 0 {
		tp.pct = float32(a.cfg.EditorPanelPct())
	}
	switch {
	case !tp.open:
		editors()
	case tp.maximized:
		a.terminalPanel(c)
	default:
		if ui.SplitVerticalPercent(c, &tp.pct, editors, func() { a.terminalPanel(c) }).Grow(1).FillWidth().Changed() {
			a.cfg.PanelPct = float64(tp.pct)
			a.saveConfigSoon()
		}
	}
}

func (a *App) terminalPanel(c *ui.Context) {
	tp := &a.term
	panel := ui.Row(c).Fill().AlignItems(ui.Stretch).Background(colors(c).Editor).BorderWidth(1, 0, 0, 0).BorderColor(colors(c).Hairline)
	panel.Children(func() {
		ui.Column(c).Grow(1).FillHeight().Children(func() {
			switch {
			case tp.view == panelTasks:
				a.taskView(c)
			case tp.view == panelReferences:
				a.referencesView(c)
			case tp.view == panelOutput:
				a.logView(c)
			case tp.view == panelCalls:
				a.callsPanel(c)
			case tp.loadErr != nil:
				a.message(c, locales.TerminalUnavailable(), tp.loadErr.Error(), "", nil)
			case tp.loading:
				ui.Row(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
					ui.Spinner(c)
					ui.Text(c, locales.MsgLoadingTheTerminal()).TextColor(colors(c).Muted)
				})
			case tp.active != nil:
				a.paneSplit(c, tp.active, tp.active.panes)
			}
		})
		a.termTabList(c)
	})
	tp.focused = tp.view == panelTerminal && panel.FocusWithin()
}

// paneSplit shows panes side by side, each taking its share of the room
// it splits with those after it.
func (a *App) paneSplit(c *ui.Context, tab *termTab, panes []*termPane) {
	if len(panes) == 1 {
		a.termPaneView(c, tab, panes[0])
		return
	}
	p := panes[0]
	ui.SplitPercent(c.Key(p), &p.pct, func() { a.termPaneView(c, tab, p) }, func() { a.paneSplit(c, tab, panes[1:]) }).Grow(1).FillHeight()
}

func (a *App) termPaneView(c *ui.Context, tab *termTab, p *termPane) {
	tp := &a.term
	box := ui.Column(c.Key(p)).Grow(1).Fill()
	if len(tab.panes) > 1 && tab.active == p {
		box.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 1}, c.Theme().Accent, 0)
		})
	}
	box.Children(func() {
		v := terminal.View(c, p.term).Grow(1).Fill().Label(locales.MsgTerminal()+p.title).FocusBind(&tp.focus, p)
		if v.FocusWithin() {
			tab.active = p
		}
	})
}

// termTabList is the panel's tabs, down its right side as VS Code lists
// its terminals, under a row of small buttons.
func (a *App) termTabList(c *ui.Context) {
	tp := &a.term
	ui.Column(c).Width(168).Shrink(0).FillHeight().Background(colors(c).TabBar).BorderWidth(0, 0, 0, 1).BorderColor(colors(c).Hairline).Children(func() {
		ui.Row(c).FillWidth().Height(26).Padding(0, 4).Gap(0).AlignItems(ui.Center).Children(func() {
			small := func(icon *ui.SVG, label, tip string) bool {
				return iconButton(c, icon, label, tip, false).Padding(3).Clicked()
			}
			if small(icPlus, commandLabel("terminal.new"), commandLabel("terminal.new")+" ("+keyLabel("Ctrl+Shift+", "`")+")") {
				a.newTerminalTab()
			}
			if small(icSplitH, locales.MsgSplitTerminal(), locales.MsgSplitTerminal()+" ("+keyLabel("Cmd+", "D")+")") {
				a.splitTerminal()
			}
			if tab := tp.active; tab != nil && small(icTrash, locales.MsgKillTerminal(), locales.MsgKillTheTerminal()) {
				a.closePane(tab.active)
			}
			ui.Box(c).Grow(1)
			maxIcon, maxTip := icMaximize, locales.MsgMaximizeThePanel()
			if tp.maximized {
				maxIcon, maxTip = icRestore, locales.MsgRestoreThePanel()
			}
			compact := func(icon *ui.SVG, label, tip string) bool {
				b := ui.ButtonBase(c).Label(label).Tooltip(tip).Size(22, 22).Radius(6).Shrink(0).KeepFocus().Center()
				if b.Hovered() || b.Pressed() {
					b.Background(colors(c).Pill)
				}
				b.Children(func() { symbolIcon(c, icon, 12, colors(c).Muted) })
				return b.Clicked()
			}
			if compact(maxIcon, maxTip, maxTip) {
				tp.maximized = !tp.maximized
			}
			if compact(icClose, locales.MsgHideThePanel(), locales.MsgHideThePanel()+" ("+keyLabel("Cmd+Shift+", "Y")+")") {
				tp.open, tp.maximized = false, false
			}
		})
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(0, 4, 4, 4).Gap(1).Children(func() {
				row := ui.Row(c.Key("output")).FillWidth().Height(24).Padding(0, 6).Gap(6).Radius(5).AlignItems(ui.Center)
				if tp.view == panelOutput {
					row.Background(colors(c).SelectedRow)
				}
				row.OnClick(func() { tp.view = panelOutput; tp.focus = nil })
				row.Children(func() { symbolIcon(c, icLines, 16, colors(c).Muted); ui.Text(c, locales.MsgOutput()).FontSize(12) })
				taskRow := ui.Row(c.Key("tasks")).FillWidth().Height(24).Padding(0, 6).Radius(5).AlignItems(ui.Center)
				if tp.view == panelTasks {
					taskRow.Background(colors(c).SelectedRow)
				}
				taskRow.OnClick(a.showTasks)
				taskRow.Children(func() { ui.Text(c, taskMessage("tasksTitle")).FontSize(12) })
				refsRow := ui.Row(c.Key("references")).FillWidth().Height(24).Padding(0, 6).Gap(6).Radius(5).AlignItems(ui.Center)
				if tp.view == panelReferences {
					refsRow.Background(colors(c).SelectedRow)
				}
				refsRow.OnClick(func() { tp.view = panelReferences; tp.focus = nil })
				refsRow.Children(func() {
					symbolIcon(c, icPin, 16, colors(c).Muted)
					ui.Text(c, locales.MsgReferences()).FontSize(12)
				})
				callsRow := ui.Row(c.Key("calls")).FillWidth().Height(24).Padding(0, 6).Gap(6).Radius(5).AlignItems(ui.Center)
				if tp.view == panelCalls {
					callsRow.Background(colors(c).SelectedRow)
				}
				callsRow.OnClick(func() { tp.view = panelCalls; tp.focus = nil })
				callsRow.Children(func() {
					symbolIcon(c, icRelated, 16, colors(c).Muted)
					ui.Text(c, a.hierarchyText("")).FontSize(12)
				})
				for i, tab := range tp.tabs {
					a.termTabRow(c, i, tab)
				}
			})
		})
	})
}

// termTabRow shows each split as a selectable row in its terminal group.
func (a *App) termTabRow(c *ui.Context, i int, tab *termTab) {
	for j, p := range tab.panes {
		a.termPaneRow(c, i, j, tab, p, len(tab.panes) > 1)
	}
}

func (a *App) termPaneRow(c *ui.Context, i, j int, tab *termTab, p *termPane, nested bool) {
	tp := &a.term
	active := tp.active == tab && tab.active == p && tp.view == panelTerminal
	row := ui.Row(c.Key(p)).Label(fmt.Sprintf(locales.MsgTerminalDD(), i+1, j+1)).FillWidth().Height(24).Padding(0, 4, 0, 6).Gap(6).Radius(5).AlignItems(ui.Center)
	hovered := row.Hovered()
	switch {
	case active:
		row.Background(colors(c).SelectedRow)
	case hovered:
		row.Background(colors(c).Pill)
	}
	row.OnClick(func() {
		tp.active = tab
		tab.active = p
		tp.view = panelTerminal
		tp.focus = p
	})
	row.Children(func() {
		if nested {
			branch := "├"
			if j == 0 {
				branch = "┌"
			} else if j == len(tab.panes)-1 {
				branch = "└"
			}
			ui.Text(c, branch).FontSize(16).TextColor(colors(c).Muted)
		}
		symbolIcon(c, icPrompt, 16, colors(c).Muted)
		title := p.title
		if title == "" {
			title = a.shellTitle()
		}
		t := ui.Text(c, title).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Grow(1).Shrink(1)
		if !active {
			t.TextColor(colors(c).Muted)
		}
		if hovered {
			split := ui.ButtonBase(c).Label(fmt.Sprintf(locales.MsgSplitTerminalDD(), i+1, j+1)).Tooltip(fmt.Sprintf(locales.MsgSplitTerminalDD(), i+1, j+1)).Size(20, 20).Radius(3).Center().KeepFocus()
			split.Children(func() { symbolIcon(c, icSplitH, 16, colors(c).Muted) })
			split.OnClick(func() {
				tp.active, tab.active = tab, p
				tp.view = panelTerminal
				a.splitTerminal()
			})
		}
		cl := ui.ButtonBase(c).Label(fmt.Sprintf(locales.MsgCloseTerminalDD(), i+1, j+1)).Tooltip(fmt.Sprintf(locales.MsgCloseTerminalDD(), i+1, j+1)).Size(20, 20).Radius(3).Center().KeepFocus()
		if hovered {
			cl.Children(func() { symbolIcon(c, icTrash, 16, colors(c).Muted) })
		}
		cl.OnClick(func() { a.closePane(p) })
	})
}

var (
	icPlus     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>`))
	icTrash    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M9 7V4.5h6V7M6.5 7l1 13h9l1-13M10 11v6M14 11v6"/></svg>`))
	icMaximize = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 15l6-6 6 6"/></svg>`))
	icRestore  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6"/></svg>`))
	icPrompt   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 7l5 5-5 5M12 18h7"/></svg>`))
)

func (a *App) terminalFont() terminal.Font {
	f := terminal.Font{Family: a.cfg.TermFontFamily, Size: float32(a.cfg.TerminalFontSize()), Weight: a.cfg.TermFontWeight, Thicken: a.cfg.TermThicken}
	if a.cfg.TermFontFallback != "" {
		if f.Family == "" {
			f.Family = "monospace"
		}
		f.Family += ", " + a.cfg.TermFontFallback
	}
	if a.cfg.TermLineHeight >= -20 && a.cfg.TermLineHeight <= 100 {
		f.LineHeight = float32(1 + a.cfg.TermLineHeight/100)
	}
	if a.cfg.TermNoLigatures {
		f.Features = append([]string{"-liga", "-calt"}, f.Features...)
	}
	if len(a.cfg.TermFontFeatures) > 0 {
		f.Features = append(slices.Clone(a.cfg.TermFontFeatures), f.Features...)
	}
	return f
}

// applyTerminalPrefs gives running terminals the Terminal pane's settings.
func (a *App) applyTerminalPrefs() {
	cos := terminal.CopyOnSelect(0)
	if a.cfg.TermCopyOnSelect {
		cos = terminal.CopyOnSelectClipboard
	}
	for _, tab := range a.term.tabs {
		for _, p := range tab.panes {
			if p.term != nil {
				p.term.SetFont(a.terminalFont())
				p.term.SetCopyOnSelect(cos)
				p.term.SetMinimumContrast(float32(a.cfg.TermMinContrast))
			}
		}
	}
}
