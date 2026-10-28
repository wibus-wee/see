package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"nsr/internal/search"
	"nsr/internal/workspace"
)

// The settings window, after macOS 27's System Settings: the panes in a
// sidebar over the window's material, with a field that finds settings,
// and each pane's settings in rounded groups under a title with back and
// forward on glass.

var (
	icGear    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="3.2"/><path d="M12 2.5v3M12 18.5v3M2.5 12h3M18.5 12h3M5.3 5.3l2.1 2.1M16.6 16.6l2.1 2.1M5.3 18.7l2.1-2.1M16.6 7.4l2.1-2.1"/></svg>`))
	icLines   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M5 6h14M5 10h9M5 14h14M5 18h7"/></svg>`))
	icSparkle = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M12 2.5c.6 4.6 2.9 6.9 7.5 7.5-4.6.6-6.9 2.9-7.5 7.5-.6-4.6-2.9-6.9-7.5-7.5 4.6-.6 6.9-2.9 7.5-7.5z"/><path d="M18.5 15c.3 2 1.2 2.9 3.2 3.2-2 .3-2.9 1.2-3.2 3.2-.3-2-1.2-2.9-3.2-3.2 2-.3 2.9-1.2 3.2-3.2z"/></svg>`))
	icCommand = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M9 9V6.5A2.5 2.5 0 1 0 6.5 9H9zm0 0h6M9 9v6m6-6V6.5A2.5 2.5 0 1 1 17.5 9H15zm0 0v6m0 0v2.5a2.5 2.5 0 1 0 2.5-2.5H15zm0 0H9m0 0v2.5A2.5 2.5 0 1 1 6.5 15H9z"/></svg>`))
)

// settingsPane is a pane of the settings window.
type settingsPane struct {
	id, title string
	icon      *ui.SVG
	tile      ui.Color // the icon's tile
	group     int      // panes of a group stand together in the sidebar
	keywords  string   // what finding settings matches, besides the title
}

var settingsPanes = []settingsPane{
	{"general", "General", icGear, ui.RGB(0x8e, 0x8e, 0x93), 0, "appearance light dark system theme reopen launch recent folders"},
	{"editor", "Editor", icLines, ui.RGB(0x0a, 0x84, 0xff), 0, "font size line numbers relative soft wrap"},
	{"terminal", "Terminal", icPrompt, ui.RGB(0x2c, 0x2c, 0x2e), 0, "shell font size copy on select panel"},
	{"intelligence", "Code Intelligence", icSparkle, ui.RGB(0xaf, 0x52, 0xde), 1, "gopls language server go to definition hover trusted folders trust"},
	{"search", "Search", icSearch, ui.RGB(0xff, 0x95, 0x00), 1, "ripgrep rg find open quickly files"},
	{"shortcuts", "Shortcuts", icCommand, ui.RGB(0x63, 0x63, 0x66), 2, "keys keyboard"},
}

// settingsState is the settings window's own state.
type settingsState struct {
	pane          string
	query         string
	back, forward []string
	fontSize      float64
	termFontSize  float64
	lineNumbers   int
	appearance    int
}

func (a *app) settingsGo(pane string) {
	s := &a.settings
	if pane == s.pane {
		return
	}
	s.back = append(s.back, s.pane)
	s.forward = nil
	s.pane = pane
}

// changed tells the main window that a setting changed what it shows.
func (a *app) settingsChanged() {
	a.saveConfig()
	a.post(func() {})
}

// applyEditorPrefs gives an editor the settings of the Editor pane.
func (a *app) applyEditorPrefs(st *editor.State) {
	st.Font = ui.Font{Size: float32(a.cfg.fontSize())}
	st.SoftWrap = a.cfg.SoftWrap
	st.LineNumbers = editor.LineNumbers(min(max(a.cfg.LineNumbers, 0), 2))
}

func (a *app) applyEditorPrefsAll() {
	for _, g := range a.groups() {
		for _, st := range g.eds {
			a.applyEditorPrefs(st)
		}
	}
}

// settingsView is the settings window's view.
func (a *app) settingsView(c *ui.Context) {
	a.applyAppearance(c)
	c.SetTheme(a.theme)
	s := &a.settings
	if s.pane == "" {
		s.pane = "general"
	}
	vibrant := c.Vibrancy()
	if vibrant {
		c.Root().Background(ui.Transparent)
	}
	if c.Shortcut(ui.Cmd, ui.KeyW) && a.closeSettings != nil {
		a.closeSettings()
	}
	bar := c.TitleBar()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		side := ui.Column(c).Width(244).Shrink(0).Padding(max(bar.Height, 40)+4, 10, 12, 10).Gap(1).DragWindow()
		if !vibrant {
			side.Background(colSidebar)
		}
		side.Children(func() { a.settingsSidebar(c) })
		content := ui.Column(c).Grow(1).Background(colSettings).BorderWidth(0, 0, 0, 1).BorderColor(colHairline)
		content.Children(func() { a.settingsContent(c, bar) })
	})
}

func (a *app) settingsSidebar(c *ui.Context) {
	s := &a.settings
	ui.SearchField(c, &s.query).Label("Search Settings").Placeholder("Search").FillWidth().Height(28).Radius(14).Margin(0, 0, 10, 0)
	q := strings.ToLower(strings.TrimSpace(s.query))
	// The app's own row, as System Settings shows the account's.
	if q == "" {
		chosen := s.pane == "about"
		sidebarRow(c, chosen, "Native Source Reader", func() { a.settingsGo("about") }).Padding(6, 6).Gap(8).Children(func() {
			ui.Image(c, appIcon).Size(34, 34).Shrink(0)
			ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
				ui.Text(c, "Native Source Reader").FontSize(13).Bold().SingleLine().TextColor(rowText(chosen, colText))
				ui.Text(c, a.versionLabel()).FontSize(11).SingleLine().TextColor(rowText(chosen, colMuted))
			})
		})
	}
	group := -1
	shown := 0
	for _, p := range settingsPanes {
		if q != "" && !strings.Contains(strings.ToLower(p.title+" "+p.keywords), q) {
			continue
		}
		shown++
		if p.group != group {
			if group >= 0 || q == "" {
				ui.Box(c).Height(10)
			}
			group = p.group
		}
		chosen := s.pane == p.id
		sidebarRow(c.Key(p.id), chosen, p.title, func() { a.settingsGo(p.id) }).Children(func() {
			iconTile(c, p.icon, p.tile, 20)
			ui.Text(c, p.title).FontSize(13).SingleLine().Shrink(1).TextColor(rowText(chosen, colText))
		})
	}
	if shown == 0 {
		ui.Column(c).FillWidth().Padding(24, 8).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "No Results").Bold().TextColor(colMuted)
			ui.Text(c, "for “"+strings.TrimSpace(s.query)+"”").FontSize(11).TextColor(colDim).SingleLine()
		})
	}
	// Up and Down go from pane to pane, as in System Settings, unless
	// something else takes them, as a field being typed in.
	if up, down := c.Shortcut(0, ui.KeyUp), c.Shortcut(0, ui.KeyDown); up || down {
		a.settingsStep(q, up)
	}
}

// settingsStep goes to the pane above or below the one shown, among those
// the search leaves.
func (a *app) settingsStep(q string, up bool) {
	ids := []string{}
	if q == "" {
		ids = append(ids, "about")
	}
	for _, p := range settingsPanes {
		if q == "" || strings.Contains(strings.ToLower(p.title+" "+p.keywords), q) {
			ids = append(ids, p.id)
		}
	}
	i := slices.Index(ids, a.settings.pane)
	switch {
	case len(ids) == 0:
		return
	case i < 0:
		i = 0
	case up:
		i = max(i-1, 0)
	default:
		i = min(i+1, len(ids)-1)
	}
	a.settingsGo(ids[i])
}

// sidebarRow is a row of the settings sidebar, as System Settings': the
// pane shown in the accent color, its text white; one under the pointer
// faintly, and darker while pressed.
func sidebarRow(c *ui.Context, chosen bool, label string, fn func()) ui.Element {
	row := ui.Row(c).FillWidth().Padding(5, 8).Gap(9).Radius(8).AlignItems(ui.Center).Label(label).
		Transition(ui.ElementTransition{Colors: true, Duration: 90 * time.Millisecond})
	switch {
	case chosen:
		row.Background(colAccent).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.12))
	case row.Pressed():
		row.Background(colPill.Mix(colText, 0.08))
	case row.Hovered():
		row.Background(colPill)
	}
	row.OnClick(fn)
	return row
}

// rowText is the color of a sidebar row's text: white on the accent.
func rowText(chosen bool, c ui.Color) ui.Color {
	if !chosen {
		return c
	}
	if c == colMuted {
		return ui.RGBA(0xff, 0xff, 0xff, 0.78)
	}
	return ui.RGB(0xff, 0xff, 0xff)
}

// iconTile draws an icon white on a rounded tile of color, as System
// Settings' panes have them: lit from above, with a fine rim and shadow.
func iconTile(c *ui.Context, icon *ui.SVG, color ui.Color, size float32) {
	ui.Box(c).Size(size, size).Shrink(0).Radius(size*0.26).Center().
		LinearGradient(ui.LinearGradient{From: color.Mix(ui.RGB(0xff, 0xff, 0xff), 0.22), To: color.Mix(ui.RGB(0, 0, 0), 0.06), Angle: 180}).
		InsetShadow(0, 0.5, 0, 0, ui.RGBA(0xff, 0xff, 0xff, 0.28)).
		Shadow(0, 0.5, 1.5, 0, ui.RGBA(0, 0, 0, 0.22)).
		Children(func() { ui.Icon(c, icon).Size(size*0.62, size*0.62).TextColor(ui.RGB(0xff, 0xff, 0xff)) })
}

func (a *app) versionLabel() string {
	if a.version == "" {
		return "Settings"
	}
	return "Version " + a.version
}

func (a *app) settingsContent(c *ui.Context, bar ui.TitleBar) {
	s := &a.settings
	title := "Native Source Reader"
	for _, p := range settingsPanes {
		if p.id == s.pane {
			title = p.title
		}
	}
	// The title bar: back and forward on a pill of glass, then the title.
	ui.Row(c).FillWidth().Height(max(bar.Height, settingsBarHeight)).Padding(0, 20, 0, 16).Gap(12).AlignItems(ui.Center).DragWindow().Children(func() {
		ui.Row(c).Height(32).Radius(16).Padding(0, 4).Gap(0).AlignItems(ui.Center).Material(glass.Glass{}).Children(func() {
			back := iconButton(c, icBack, "Back", "Back", false).Disabled(len(s.back) == 0)
			if back.Clicked() && len(s.back) > 0 {
				s.forward = append(s.forward, s.pane)
				s.pane, s.back = s.back[len(s.back)-1], s.back[:len(s.back)-1]
			}
			ui.Box(c).Size(1, 16).Background(colSeparator)
			fwd := iconButton(c, icForward, "Forward", "Forward", false).Disabled(len(s.forward) == 0)
			if fwd.Clicked() && len(s.forward) > 0 {
				s.back = append(s.back, s.pane)
				s.pane, s.forward = s.forward[len(s.forward)-1], s.forward[:len(s.forward)-1]
			}
		})
		ui.Text(c, title).FontSize(17).Bold().SingleLine()
	})
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Padding(4, 20, 24, 20).Gap(8).Children(func() {
			switch s.pane {
			case "general":
				a.generalPane(c)
			case "editor":
				a.editorPane(c)
			case "terminal":
				a.terminalPane(c)
			case "intelligence":
				a.intelligencePane(c)
			case "search":
				a.searchPane(c)
			case "shortcuts":
				a.shortcutsPane(c)
			default:
				a.aboutPane(c)
			}
		})
	})
}

// ----- Groups and rows -----

// card is a rounded group of settings; its rows are divided by lines.
func card(c *ui.Context, rows ...func()) {
	ui.Column(c).FillWidth().Radius(12).Background(colCard).Padding(0, 14).Children(func() {
		for i, row := range rows {
			if i > 0 {
				ui.Box(c).Height(1).FillWidth().Background(colSeparator)
			}
			ui.Row(c.Key(i)).FillWidth().MinHeight(44).Padding(8, 0).Gap(12).AlignItems(ui.Center).Children(row)
		}
	})
}

// heading heads a group, as System Settings' section titles.
func heading(c *ui.Context, s string) {
	ui.Text(c, s).Bold().Padding(14, 4, 2, 4)
}

// labeled is a row's label and its description, which take the room
// before its control.
func labeled(c *ui.Context, label, detail string) {
	ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
		ui.Text(c, label)
		if detail != "" {
			ui.Text(c, detail).FontSize(12).TextColor(colMuted)
		}
	})
}

// statusCard is a card with a tile, a name, a status, and an action, as
// the server's at the top of the picture this follows.
func statusCard(c *ui.Context, icon *ui.SVG, tile ui.Color, name, status string, action func()) {
	ui.Row(c).FillWidth().Radius(12).Background(colCard).Padding(12, 14).Gap(12).AlignItems(ui.Center).Children(func() {
		iconTile(c, icon, tile, 38)
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, name).Bold()
			ui.Text(c, status).FontSize(12).TextColor(colMuted)
		})
		if action != nil {
			action()
		}
	})
}

// ----- Panes -----

func (a *app) generalPane(c *ui.Context) {
	s := &a.settings
	switch a.cfg.Appearance {
	case "light":
		s.appearance = 1
	case "dark":
		s.appearance = 2
	default:
		s.appearance = 0
	}
	card(c,
		func() {
			labeled(c, "Appearance", "Follows the system, or stays light or dark.")
			if ui.Segmented(c, &s.appearance, "System", "Light", "Dark").Label("Appearance").Changed() {
				a.chooseAppearance([]string{"", "light", "dark"}[s.appearance])
				a.post(func() {})
			}
		},
		func() {
			labeled(c, "Reopen the last folder at launch", "Otherwise the welcome window shows, with the folders opened lately.")
			if ui.Switch(c, &a.cfg.ReopenLast).Label("Reopen the last folder at launch").Changed() {
				a.settingsChanged()
			}
		},
	)
	heading(c, "Recent Folders")
	card(c, func() {
		n := len(a.cfg.Recent)
		labeled(c, fmt.Sprintf("%d recent folder%s", n, plural(n)), "Shown on the welcome window.")
		if ui.Button(c, "Clear Recents").Disabled(n == 0).Clicked() {
			a.cfg.clearRecent()
			a.settingsChanged()
		}
	})
}

func (a *app) editorPane(c *ui.Context) {
	s := &a.settings
	s.fontSize = a.cfg.fontSize()
	s.lineNumbers = a.cfg.LineNumbers
	card(c,
		func() {
			labeled(c, "Font Size", "The editor's monospaced font.")
			ui.Text(c, fmt.Sprintf("%g pt", s.fontSize)).TextColor(colMuted)
			if ui.Stepper(c, &s.fontSize, 9, 28, 1).Label("Font Size").Changed() {
				a.cfg.FontSize = s.fontSize
				a.applyEditorPrefsAll()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, "Line Numbers", "Relative numbers count from the caret's line.")
			if ui.Segmented(c, &s.lineNumbers, "On", "Relative", "Off").Label("Line Numbers").Changed() {
				a.cfg.LineNumbers = s.lineNumbers
				a.applyEditorPrefsAll()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, "Wrap Lines", "Long lines wrap at the editor's width rather than scrolling sideways.")
			if ui.Switch(c, &a.cfg.SoftWrap).Label("Wrap Lines").Changed() {
				a.applyEditorPrefsAll()
				a.settingsChanged()
			}
		},
	)
}

func (a *app) intelligencePane(c *ui.Context) {
	status := "Starts the first time Go to Definition or a hover needs it."
	path, err := findGopls(a.root)
	switch {
	case err != nil:
		status = "Not installed: go install golang.org/x/tools/gopls@latest"
	case a.lspState == lspReady:
		status = "Running · " + tildePath(path)
	case a.lspState == lspStarting:
		status = "Starting…"
	case a.lspState == lspFailed && a.lspErr != nil:
		status = shortErr(a.lspErr)
	default:
		status += " " + tildePath(path)
	}
	statusCard(c, icSparkle, ui.RGB(0xaf, 0x52, 0xde), "gopls", status, func() {
		if a.lspState == lspFailed && ui.Button(c, "Retry").Clicked() {
			a.retryLSP()
			a.post(func() {})
		}
	})
	heading(c, "Trusted Folders")
	ui.Text(c, "gopls runs the go command in these folders, which can download modules and toolchains.").FontSize(12).TextColor(colMuted).Padding(0, 4, 4, 4)
	if len(a.cfg.Trusted) == 0 {
		card(c, func() { labeled(c, "No trusted folders", "A folder is trusted the first time you allow gopls in it.") })
		return
	}
	var rows []func()
	for _, p := range a.cfg.Trusted {
		rows = append(rows, func() {
			folderGlyph(c, false, 18)
			labeled(c, filepath.Base(p), tildePath(p))
			if ui.Button(c, "Remove").Clicked() {
				a.cfg.Trusted = slices.DeleteFunc(a.cfg.Trusted, func(t string) bool { return t == p })
				a.settingsChanged()
			}
		})
	}
	card(c, rows...)
}

func (a *app) searchPane(c *ui.Context) {
	rg, err := search.FindRipgrep(a.root)
	status := "Finds text in the workspace · " + tildePath(rg)
	if err != nil {
		status = "Not installed: " + search.InstallHint()
	}
	statusCard(c, icSearch, ui.RGB(0xff, 0x95, 0x00), "ripgrep", status, nil)
	heading(c, "What is searched")
	card(c,
		func() {
			labeled(c, "Ignored folders", strings.Join(ignoredList(), ", ")+", and what .gitignore leaves out.")
		},
		func() {
			labeled(c, "Open Quickly", fmt.Sprintf("Lists up to %d files by name.", maxQuickFiles))
		},
	)
}

func (a *app) shortcutsPane(c *ui.Context) {
	rows := []struct{ what, mods, key string }{
		{"Open Folder", "Cmd+", "O"},
		{"Open Quickly", "Cmd+Shift+", "O"},
		{"Find in Workspace", "Cmd+Shift+", "F"},
		{"Find in File", "Cmd+", "F"},
		{"Go to Definition", "", "F12"},
		{"Go Back", "Ctrl+", "-"},
		{"Go Forward", "Ctrl+Shift+", "-"},
		{"Add Editor on Right", "Cmd+", "D"},
		{"Add Editor Below", "Cmd+Shift+", "D"},
		{"Close Tab", "Cmd+", "W"},
		{"Show or Hide the Terminal", "Cmd+Shift+", "Y"},
		{"New Terminal", "Ctrl+Shift+", "`"},
		{"Split Terminal (in a terminal)", "Cmd+", "D"},
		{"Show or Hide the Navigator", "Cmd+", "0"},
		{"Settings", "Cmd+", ","},
	}
	var fns []func()
	for _, r := range rows {
		fns = append(fns, func() {
			labeled(c, r.what, "")
			ui.Text(c, keyLabel(r.mods, r.key)).TextColor(colMuted).Padding(2, 8).Radius(6).Background(colPill)
		})
	}
	card(c, fns...)
}

func (a *app) aboutPane(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(24, 0).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Image(c, appIcon).Size(96, 96)
		ui.Text(c, "Native Source Reader").FontSize(20).Bold()
		ui.Text(c, a.versionLabel()).TextColor(colMuted)
		ui.Text(c, "Fast, focused reading of source code, drawn natively.").FontSize(12).TextColor(colMuted)
	})
	if a.root != "" {
		card(c, func() { labeled(c, "Folder open", tildePath(a.root)) })
	}
}

func ignoredList() []string { return workspace.IgnoredNames() }

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (a *app) terminalPane(c *ui.Context) {
	s := &a.settings
	s.termFontSize = a.cfg.termFontSize()
	statusCard(c, icPrompt, ui.RGB(0x2c, 0x2c, 0x2e), shellName(),
		"Terminals run your login shell in the open folder, with Ghostty's emulator.", nil)
	heading(c, "Appearance")
	card(c,
		func() {
			labeled(c, "Font Size", "The terminals' monospaced font.")
			ui.Text(c, fmt.Sprintf("%g pt", s.termFontSize)).TextColor(colMuted)
			if ui.Stepper(c, &s.termFontSize, 9, 28, 1).Label("Terminal Font Size").Changed() {
				a.cfg.TermFontSize = s.termFontSize
				a.applyTerminalPrefs()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, "Copy on Select", "Selecting text copies it to the clipboard, as in Ghostty.")
			if ui.Switch(c, &a.cfg.TermCopyOnSelect).Label("Copy on Select").Changed() {
				a.applyTerminalPrefs()
				a.settingsChanged()
			}
		},
	)
}
