package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// termPanel is the bottom panel's state.
type termPanel struct {
	open      bool
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
	id    int
	term  *terminal.Terminal
	title string
	pct   float32 // its share of the room it splits with those after it
}

// newTerm makes a terminal running the user's shell in the workspace.
// Its callbacks come from the terminal's goroutines: they go through
// post, and are dropped once the pane or the workspace is gone.
func (a *app) newTerm(p *termPane) error {
	gen := a.gen
	var t *terminal.Terminal
	t, err := terminal.New(terminal.Options{
		Dir:  a.root,
		Font: terminal.Font{Size: float32(a.cfg.termFontSize())},
		CopyOnSelect: map[bool]terminal.CopyOnSelect{
			true: terminal.CopyOnSelectClipboard,
		}[a.cfg.TermCopyOnSelect],
		OnTitle: func(title string) {
			a.post(func() {
				if a.gen == gen {
					p.title = title
				}
			})
		},
		OnExit: func(int) {
			a.post(func() {
				if a.gen == gen {
					a.closePane(p) // as VS Code closes a terminal whose shell ended
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
func (a *app) withTerminal(fn func()) {
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
		a.post(func() {
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
func (a *app) toggleTerminal() {
	tp := &a.term
	if tp.open {
		tp.open, tp.maximized = false, false
		return
	}
	tp.open = true
	if len(tp.tabs) == 0 {
		a.newTerminalTab()
	} else if tp.active != nil {
		tp.focus = tp.active.active
	}
}

// newTerminalTab opens a tab with a new terminal.
func (a *app) newTerminalTab() {
	if a.root == "" {
		return
	}
	a.term.open = true
	a.withTerminal(func() {
		tp := &a.term
		tp.nextID++
		p := &termPane{id: tp.nextID, title: shellName()}
		if err := a.newTerm(p); err != nil {
			a.note("Could not start a terminal: "+err.Error(), true)
			return
		}
		tab := &termTab{id: tp.nextID, panes: []*termPane{p}, active: p}
		tp.tabs = append(tp.tabs, tab)
		tp.active, tp.focus = tab, p
	})
}

// splitTerminal adds a terminal beside the active one of the tab.
func (a *app) splitTerminal() {
	tp := &a.term
	tab := tp.active
	if tab == nil {
		a.newTerminalTab()
		return
	}
	a.withTerminal(func() {
		tp.nextID++
		p := &termPane{id: tp.nextID, title: shellName()}
		if err := a.newTerm(p); err != nil {
			a.note("Could not start a terminal: "+err.Error(), true)
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
func (a *app) closePane(p *termPane) {
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
		if len(tp.tabs) == 0 {
			tp.open, tp.maximized = false, false
		}
		return
	}
}

// closeTerminals ends every terminal, as the workspace closes.
func (a *app) closeTerminals() {
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
func (a *app) openTermLink(p *termPane, link string) bool {
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
func (a *app) editorsAndPanel(c *ui.Context, editors func()) {
	tp := &a.term
	if tp.pct <= 0 {
		tp.pct = float32(a.cfg.panelPct())
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

func (a *app) terminalPanel(c *ui.Context) {
	tp := &a.term
	panel := ui.Row(c).Fill().AlignItems(ui.Stretch).Background(colEditor).BorderWidth(1, 0, 0, 0).BorderColor(colHairline)
	panel.Children(func() {
		ui.Column(c).Grow(1).FillHeight().Children(func() {
			switch {
			case tp.loadErr != nil:
				a.message(c, "The terminal is not available", tp.loadErr.Error(), "", nil)
			case tp.loading:
				ui.Row(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
					ui.Spinner(c)
					ui.Text(c, "Loading the terminal…").TextColor(colMuted)
				})
			case tp.active != nil:
				a.paneSplit(c, tp.active, tp.active.panes)
			}
		})
		a.termTabList(c)
	})
	tp.focused = panel.FocusWithin()
}

// paneSplit shows panes side by side, each taking its share of the room
// it splits with those after it.
func (a *app) paneSplit(c *ui.Context, tab *termTab, panes []*termPane) {
	if len(panes) == 1 {
		a.termPaneView(c, tab, panes[0])
		return
	}
	p := panes[0]
	ui.SplitPercent(c.Key(p), &p.pct, func() { a.termPaneView(c, tab, p) }, func() { a.paneSplit(c, tab, panes[1:]) }).Grow(1).FillHeight()
}

func (a *app) termPaneView(c *ui.Context, tab *termTab, p *termPane) {
	tp := &a.term
	box := ui.Column(c.Key(p)).Grow(1).Fill()
	if len(tab.panes) > 1 && tab.active == p {
		box.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 1}, colAccent, 0)
		})
	}
	box.Children(func() {
		v := terminal.View(c, p.term).Grow(1).Fill().Label("Terminal " + p.title)
		if tp.focus == p {
			v.Focus()
			tp.focus = nil
		}
		if v.FocusWithin() {
			tab.active = p
		}
	})
}

// termTabList is the panel's tabs, down its right side as VS Code lists
// its terminals, under a row of small buttons.
func (a *app) termTabList(c *ui.Context) {
	tp := &a.term
	ui.Column(c).Width(168).Shrink(0).FillHeight().Background(colTabBar).BorderWidth(0, 0, 0, 1).BorderColor(colHairline).Children(func() {
		ui.Row(c).FillWidth().Height(26).Padding(0, 4).Gap(0).AlignItems(ui.Center).Children(func() {
			small := func(icon *ui.SVG, label, tip string) bool {
				return iconButton(c, icon, label, tip, false).Padding(3).Clicked()
			}
			if small(icPlus, "New Terminal", "New Terminal ("+keyLabel("Ctrl+Shift+", "`")+")") {
				a.newTerminalTab()
			}
			if small(icSplitH, "Split Terminal", "Split Terminal ("+keyLabel("Cmd+", "D")+")") {
				a.splitTerminal()
			}
			if tab := tp.active; tab != nil && small(icTrash, "Kill Terminal", "Kill the Terminal") {
				a.closePane(tab.active)
			}
			ui.Box(c).Grow(1)
			maxIcon, maxTip := icMaximize, "Maximize the Panel"
			if tp.maximized {
				maxIcon, maxTip = icRestore, "Restore the Panel"
			}
			if small(maxIcon, maxTip, maxTip) {
				tp.maximized = !tp.maximized
			}
			if small(icClose, "Hide the Panel", "Hide the Panel ("+keyLabel("Cmd+Shift+", "Y")+")") {
				tp.open, tp.maximized = false, false
			}
		})
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(0, 4, 4, 4).Gap(1).Children(func() {
				for i, tab := range tp.tabs {
					a.termTabRow(c, i, tab)
				}
			})
		})
	})
}

// termTabRow is a tab of the list: the terminal's title, how many
// terminals it splits into, and a close button while hovered.
func (a *app) termTabRow(c *ui.Context, i int, tab *termTab) {
	tp := &a.term
	active := tp.active == tab
	row := ui.Row(c.Key(tab)).FillWidth().Height(22).Padding(0, 4, 0, 6).Gap(6).Radius(5).AlignItems(ui.Center)
	hovered := row.Hovered()
	switch {
	case active:
		row.Background(colSelectedRow)
	case hovered:
		row.Background(colPill)
	}
	row.OnClick(func() {
		tp.active = tab
		tp.focus = tab.active
	})
	row.Children(func() {
		ui.Icon(c, icPrompt).Size(12, 12).TextColor(colMuted)
		title := tab.active.title
		if title == "" {
			title = shellName()
		}
		t := ui.Text(c, title).FontSize(12).SingleLine().Grow(1).Shrink(1)
		if !active {
			t.TextColor(colMuted)
		}
		if len(tab.panes) > 1 {
			ui.Text(c, fmt.Sprint(len(tab.panes))).FontSize(10).TextColor(colMuted).Padding(0, 5).Radius(7).Background(colPill)
		}
		cl := ui.ButtonBase(c).Label(fmt.Sprintf("Close Terminal %d", i+1)).Size(14, 14).Radius(3).Center().KeepFocus()
		if hovered {
			cl.Children(func() { ui.Icon(c, icClose).Size(9, 9).TextColor(colMuted) })
		}
		cl.OnClick(func() {
			for len(tab.panes) > 0 {
				a.closePane(tab.panes[0])
			}
		})
	})
}

var (
	icPlus     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>`))
	icTrash    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M9 7V4.5h6V7M6.5 7l1 13h9l1-13M10 11v6M14 11v6"/></svg>`))
	icMaximize = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 15l6-6 6 6"/></svg>`))
	icRestore  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6"/></svg>`))
	icPrompt   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 7l5 5-5 5M12 18h7"/></svg>`))
)

// applyTerminalPrefs gives running terminals the Terminal pane's settings.
func (a *app) applyTerminalPrefs() {
	cos := terminal.CopyOnSelect(0)
	if a.cfg.TermCopyOnSelect {
		cos = terminal.CopyOnSelectClipboard
	}
	for _, tab := range a.term.tabs {
		for _, p := range tab.panes {
			if p.term != nil {
				p.term.SetFont(terminal.Font{Size: float32(a.cfg.termFontSize())})
				p.term.SetCopyOnSelect(cos)
			}
		}
	}
}
