package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/config"
	"see/internal/search"
	"see/internal/workspace"

	"see/internal/langserver"
	"see/internal/languages"
	"see/internal/theme"
	"see/locales"
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
	{"general", "General", icGear, ui.RGB(0x8e, 0x8e, 0x93), 0, "appearance light dark system theme reopen launch recent folders shell command install uninstall path cli"},
	{"editor", "Editor", icLines, ui.RGB(0x0a, 0x84, 0xff), 0, "font size line numbers relative soft wrap"},
	{"terminal", "Terminal", icPrompt, ui.RGB(0x2c, 0x2c, 0x2e), 0, "shell font size copy on select panel external ghostty rex seedmux"},
	{"notifications", "Notifications", icPrompt, ui.RGB(0xff, 0x95, 0x00), 1, "notify waiting done failed sound dock badge bounce"},
	{"intelligence", "Code Intelligence", icSparkle, ui.RGB(0xaf, 0x52, 0xde), 1, "gopls tsrs typescript javascript language server go to definition hover trusted folders trust"},
	{"advanced", "Advanced", icGear, ui.RGB(0x8e, 0x8e, 0x93), 2, "config reset data folder"},
	{"search", "Search", icSearch, ui.RGB(0xff, 0x95, 0x00), 1, "ripgrep rg find open quickly files"},
	{"shortcuts", "Shortcuts", icCommand, ui.RGB(0x63, 0x63, 0x66), 2, "keys keyboard"},
}

// settingsState is the settings window's own state.
type settingsState struct {
	shellCommandMessage string
	termFeaturesDraft   string
	editorFeaturesDraft string
	envDraft            string
	pane                string
	query               string
	back, forward       []string
	fontOpen            bool
	fontButton          ui.Handle
	fontQuery           string
	fontFilter          int
	fontFamilies        []ui.FontFamily
	fontLoading         chan []ui.FontFamily
	fontValuesSet       bool
	termWeight          float64
	termHeight          float64
	fontSize            float64
	termFontSize        float64
	lineNumbers         int
	serverInstall       string
	appearance          int
	languageChoice      int
	languageChoiceSet   bool
	terminalIcons       map[string]*ui.Bitmap
}

func (a *App) settingsGo(pane string) {
	s := &a.settings
	if pane == s.pane {
		return
	}
	s.fontOpen = false
	s.back = append(s.back, s.pane)
	s.forward = nil
	s.pane = pane
}

// changed tells the main window that a setting changed what it shows.
func (a *App) settingsChanged() {
	a.saveConfig()
	a.post.Post(func() {})
}

// applyEditorPrefs gives an editor the settings of the Editor pane.
func (a *App) applyEditorPrefs(st *editor.State) {
	st.Font = ui.Font{
		Family:   a.cfg.FontFamily,
		Size:     float32(a.cfg.EditorFontSize()),
		Weight:   a.cfg.EditorFontWeight,
		Thicken:  a.cfg.EditorThicken,
		Features: strings.Join(a.cfg.EditorFontFeatures, ", "),
	}
	if a.cfg.EditorLineHeight > 0 {
		st.Font.LineHeight = float32(a.cfg.EditorLineHeight)
	}
	st.SoftWrap = a.cfg.SoftWrap
	st.LineNumbers = editor.LineNumbers(min(max(a.cfg.LineNumbers, 0), 2))
	if a.cfg.EditorNoLigatures {
		if st.Font.Features == "" {
			st.Font.Features = "-liga, -calt"
		} else {
			st.Font.Features = "-liga, -calt, " + st.Font.Features
		}
	}
}

func (a *App) applyEditorPrefsAll() {
	for _, g := range a.groups() {
		for _, st := range g.eds {
			a.applyEditorPrefs(st)
		}
		for _, v := range g.diffs {
			for _, d := range v.diffStates() {
				a.applyDiffPrefs(d)
			}
		}
	}
}

func (a *App) applyDiffPrefs(diff *editor.DiffState) {
	diff.Font = ui.Font{
		Family:   a.cfg.FontFamily,
		Size:     float32(a.cfg.EditorFontSize()),
		Weight:   a.cfg.EditorFontWeight,
		Thicken:  a.cfg.EditorThicken,
		Features: strings.Join(a.cfg.EditorFontFeatures, ", "),
	}
	if a.cfg.EditorLineHeight > 0 {
		diff.Font.LineHeight = float32(a.cfg.EditorLineHeight)
	}
	if a.cfg.EditorNoLigatures {
		if diff.Font.Features == "" {
			diff.Font.Features = "-liga, -calt"
		} else {
			diff.Font.Features = "-liga, -calt, " + diff.Font.Features
		}
	}
	diff.Wrap = a.cfg.SoftWrap
	diff.NoLineNumbers = a.cfg.LineNumbers == int(editor.LineNumbersOff)
	diff.Layout = editor.DiffSplit
	if a.cfg.DiffUnified {
		diff.Layout = editor.DiffUnified
	}
	if a.cfg.DiffLayout == "unified" {
		diff.Layout = editor.DiffUnified
	}
	if a.cfg.DiffLayout == "split" {
		diff.Layout = editor.DiffSplit
	}
}

// toggleDiffLayout shows every diff inline, or side by side again, and
// keeps the choice for the next ones.
func (a *App) toggleDiffLayout() {
	a.cfg.DiffUnified = !a.cfg.DiffUnified
	for _, g := range a.groups() {
		for _, v := range g.diffs {
			for _, d := range v.diffStates() {
				a.applyDiffPrefs(d)
			}
		}
	}
	a.saveConfig()
}

// goToChange moves the active diff's caret to the next change, or the
// previous one with dir -1, as Xcode's and Peek's Next and Previous
// Change; it wraps around.
func (a *App) goToChange(dir int) {
	v := a.focus.diffs[a.focus.active]
	if v == nil || v.state == nil {
		return
	}
	n := len(v.state.Hunks())
	if n == 0 {
		return
	}
	// i is the hunk the caret is in, else the last one before it.
	i, inside := v.state.CurrentHunk()
	switch {
	case dir > 0:
		i++
	case inside:
		i--
	}
	v.state.GoToHunk((i%n + n) % n)
}

// settingsView is the settings window's view.
func (a *App) settingsView(c *ui.Context) {
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
	if c.Shortcut(ui.Cmd, ui.KeyW) {
		a.host.CloseSettings()
	}
	bar := c.TitleBar()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		side := ui.Column(c).Width(240).Shrink(0).Padding(max(bar.Height, 40)+4, 10, 12, 10).Gap(1).DragWindow()
		if !vibrant {
			side.Background(theme.Of(c).Sidebar)
		}
		side.Children(func() { a.settingsSidebar(c) })
		content := ui.Column(c).Grow(1).Background(theme.Of(c).Settings).BorderWidth(0, 0, 0, 1).BorderColor(theme.Of(c).Hairline)
		content.Children(func() { a.settingsContent(c, bar) })
	})
}

func (a *App) settingsSidebar(c *ui.Context) {
	s := &a.settings
	ui.SearchField(c, &s.query).Label(locales.MsgSearchSettings()).Placeholder(locales.Search()).FillWidth().Height(28).Radius(14).Margin(0, 0, 10, 0)
	q := strings.ToLower(strings.TrimSpace(s.query))
	// The app's own row, as System Settings shows the account's.
	if q == "" {
		chosen := s.pane == "about"
		sidebarRow(c, chosen, "See", func() { a.settingsGo("about") }).Padding(6, 6).Gap(8).Children(func() {
			ui.Image(c, appIcon).Size(34, 34).Shrink(0)
			ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
				ui.Text(c, "See").FontSize(13).Bold().SingleLine().TextColor(rowText(chosen, theme.Of(c).Text))
				ui.Text(c, a.versionLabel()).FontSize(11).SingleLine().TextColor(rowText(chosen, theme.Of(c).Muted))
			})
		})
	}
	group := -1
	shown := 0
	for _, p := range settingsPanes {
		p.title = settingsPaneTitle(p.id)
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
			ui.Text(c, p.title).FontSize(13).SingleLine().Shrink(1).TextColor(rowText(chosen, theme.Of(c).Text))
		})
	}
	if shown == 0 {
		ui.Column(c).FillWidth().Padding(24, 8).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.MsgNoResults()).Bold().TextColor(theme.Of(c).Muted)
			ui.Text(c, locales.MsgFor()+strings.TrimSpace(s.query)+"”").FontSize(11).TextColor(theme.Of(c).Dim).SingleLine()
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
func (a *App) settingsStep(q string, up bool) {
	ids := []string{}
	if q == "" {
		ids = append(ids, "about")
	}
	for _, p := range settingsPanes {
		p.title = settingsPaneTitle(p.id)
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
		row.Background(c.Theme().Accent).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.12))
	case row.Pressed():
		row.Background(theme.Of(c).Pill.Mix(theme.Of(c).Text, 0.08))
	case row.Hovered():
		row.Background(theme.Of(c).Pill)
	}
	row.OnClick(fn)
	return row
}

// rowText is the color of a sidebar row's text: white on the accent.
func rowText(chosen bool, col ui.Color) ui.Color {
	if !chosen {
		return col
	}
	if col == theme.Dark.Muted || col == theme.Light.Muted {
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
		Children(func() { symbolIcon(c, icon, size*0.62, ui.RGB(0xff, 0xff, 0xff)) })
}

func (a *App) versionLabel() string {
	if a.version == "" {
		return "Settings"
	}
	return "Version " + a.version
}

func (a *App) settingsContent(c *ui.Context, bar ui.TitleBar) {
	s := &a.settings
	title := "See"
	for _, p := range settingsPanes {
		if p.id == s.pane {
			title = settingsPaneTitle(p.id)
		}
	}
	// The title bar: back and forward on a pill of glass, then the title.
	ui.Row(c).FillWidth().Height(max(bar.Height, settingsBarHeight)).Padding(0, 20, 0, 16).Gap(12).AlignItems(ui.Center).DragWindow().Children(func() {
		ui.Row(c).Height(32).Radius(16).Padding(0, 4).Gap(0).AlignItems(ui.Center).Material(glass.Glass{}).Children(func() {
			back := iconButtonWithState(c, icBack, "Back", "Back", false, len(s.back) == 0, 13)
			if back.Clicked() && len(s.back) > 0 {
				s.forward = append(s.forward, s.pane)
				s.pane, s.back = s.back[len(s.back)-1], s.back[:len(s.back)-1]
			}
			ui.Box(c).Size(1, 16).Background(theme.Of(c).Separator)
			fwd := iconButtonWithState(c, icForward, "Forward", "Forward", false, len(s.forward) == 0, 13)
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
			case "notifications":
				a.notificationsPane(c)
			case "intelligence":
				a.intelligencePane(c)
			case "search":
				a.searchPane(c)
			case "shortcuts":
				a.shortcutsPane(c)
			case "advanced":
				a.advancedPane(c)
			default:
				a.aboutPane(c)
			}
		})
	})
}

// ----- Groups and rows -----

// card is a rounded group of settings; its rows are divided by lines.
func card(c *ui.Context, rows ...func()) {
	ui.Column(c).FillWidth().Radius(12).Background(theme.Of(c).Card).Padding(0, 14).Children(func() {
		for i, row := range rows {
			if i > 0 {
				ui.Box(c).Height(1).FillWidth().Background(theme.Of(c).Separator)
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
			ui.Text(c, detail).FontSize(12).TextColor(theme.Of(c).Muted)
		}
	})
}

// statusCard is a card with a tile, a name, a status, and an action, as
// the server's at the top of the picture this follows.
func statusCard(c *ui.Context, icon *ui.SVG, tile ui.Color, name, status string, action func()) {
	ui.Row(c).FillWidth().Radius(12).Background(theme.Of(c).Card).Padding(12, 14).Gap(12).AlignItems(ui.Center).Children(func() {
		iconTile(c, icon, tile, 38)
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, name).Bold()
			ui.Text(c, status).FontSize(12).TextColor(theme.Of(c).Muted)
		})
		if action != nil {
			action()
		}
	})
}

// ----- Panes -----

func (a *App) generalPane(c *ui.Context) {
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
			if !s.languageChoiceSet {
				switch a.cfg.Language {
				case "en":
					s.languageChoice = 1
				case "zh-Hans":
					s.languageChoice = 2
				}
				s.languageChoiceSet = true
			}
			labeled(c, locales.Language(), locales.LanguageDescription())
			if ui.Segmented(c, &s.languageChoice, locales.System(), "English", "简体中文").Label(locales.Language()).Changed() {
				a.cfg.Language = []string{"", "en", "zh-Hans"}[s.languageChoice]
				mygo.App.SetLocale(a.cfg.Language)
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.Appearance(), locales.MsgFollowsTheSystemOrStaysLightOrDark())
			if ui.Segmented(c, &s.appearance, locales.System(), locales.Light(), locales.Dark()).Label(locales.Appearance()).Changed() {
				a.chooseAppearance([]string{"", "light", "dark"}[s.appearance])
				a.post.Post(func() {})
			}
		},
		func() {
			labeled(c, locales.MsgReopenTheLastFolderAtLaunch(), locales.MsgOtherwiseTheWelcomeWindowShowsWithTheFolders())
			if ui.Switch(c, &a.cfg.ReopenLast).Label(locales.MsgReopenTheLastFolderAtLaunch()).Changed() {
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgAskBeforeClosingUnsavedFiles(), locales.MsgSeeAsksBeforeClosingATabOrWindow())
			if ui.Switch(c, &a.cfg.ConfirmClose).Label(locales.MsgAskBeforeClosing()).Changed() {
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgGitStatusInTheNavigator(), locales.MsgShowEachFileSStatusAndLineCounts())
			if ui.Switch(c, &a.cfg.GitStatus).Label(locales.MsgGitStatus()).Changed() {
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgHiddenNames(), locales.MsgHideMatchingFilesAndFoldersInTheProject())
			if ui.TextInput(c, &a.cfg.HiddenNames).Label(locales.MsgHiddenNames()).Placeholder(".DS_Store").Width(200).Changed() {
				a.filter.forQuery = ""
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgDiffLayout(), locales.MsgShowDiffsInlineOrSideBySide())
			layout := 0
			if a.cfg.DiffLayout == "unified" || (a.cfg.DiffLayout == "" && a.cfg.DiffUnified) {
				layout = 1
			} else if a.cfg.DiffLayout == "split" {
				layout = 2
			}
			if ui.Segmented(c, &layout, locales.MsgAuto(), locales.MsgUnified(), locales.MsgSplit()).Label(locales.MsgDiffLayout()).Changed() {
				a.cfg.DiffLayout = []string{"auto", "unified", "split"}[layout]
				a.cfg.DiffUnified = a.cfg.DiffLayout == "unified"
				a.applyEditorPrefsAll()
				a.settingsChanged()
			}
		},
	)
	heading(c, locales.MsgShellCommand())
	card(c, func() {
		detail := "Install see in ~/.local/bin to open files and folders from a terminal. Add ~/.local/bin to your PATH if needed."
		if s.shellCommandMessage != "" {
			detail = s.shellCommandMessage
		}
		labeled(c, locales.MsgSeeCommand(), detail)
		ui.Button(c.Key("install-see"), locales.Install()).Label(locales.MsgInstallSeeCommand()).OnClick(func() {
			a.installSee()
			s.shellCommandMessage = a.notice
		})
		ui.Button(c.Key("uninstall-see"), locales.MsgUninstall()).Label(locales.MsgUninstallSeeCommand()).OnClick(func() {
			a.uninstallSee()
			s.shellCommandMessage = a.notice
		})
	})
	heading(c, locales.MsgRecentFolders())
	card(c, func() {
		n := len(a.cfg.Recent)
		labeled(c, locales.MsgDRecentFolderS(n), locales.MsgShownOnTheWelcomeWindow())
		if ui.Button(c, locales.MsgClearRecents()).Disabled(n == 0).Clicked() {
			a.cfg.ClearRecent()
			a.settingsChanged()
		}
	})
	heading(c, locales.MsgStartup())
	card(c,
		func() {
			labeled(c, locales.MsgOpenAtLogin(), locales.MsgStartSeeWhenYouLogIn())
			open := mygo.App.OpenAtLogin()
			if ui.Switch(c, &open).Label(locales.MsgOpenAtLogin()).Changed() {
				if err := mygo.App.SetOpenAtLogin(open); err != nil {
					_ = err
				}
				a.settingsChanged()
			}
		},
	)
	heading(c, locales.MsgUpdates())
	card(c,
		func() {
			labeled(c, locales.MsgAutomaticChecks(), locales.MsgCheckForNewVersionsInTheBackground())
			auto := updater.AutomaticChecks()
			if ui.Switch(c, &auto).Label(locales.MsgAutomaticChecks()).Changed() {
				updater.SetAutomaticChecks(auto)
				a.settingsChanged()
			}
		},
	)
}

func (a *App) initFontValues() {
	s := &a.settings
	if s.fontValuesSet {
		return
	}
	s.fontValuesSet = true
	s.fontSize = a.cfg.EditorFontSize()
	s.termFontSize = a.cfg.TerminalFontSize()
	s.termWeight = float64(a.cfg.TermFontWeight)
	if s.termWeight < 100 || s.termWeight > 900 {
		s.termWeight = 400
	}
	s.termHeight = a.cfg.TermLineHeight
}

func (a *App) editorPane(c *ui.Context) {
	s := &a.settings
	a.initFontValues()
	s.lineNumbers = a.cfg.LineNumbers
	heading(c, locales.MsgTypography())
	a.fontPreview(c, false)
	card(c,
		func() { a.fontPicker(c, false) },
		func() {
			labeled(c, locales.MsgFontSize(), locales.MsgTheEditorSFontSize())
			ui.Text(c, fmt.Sprintf("%g pt", s.fontSize)).TextColor(theme.Of(c).Muted)
			if ui.Stepper(c, &s.fontSize, 9, 28, 1).Label(locales.MsgFontSize()).Changed() {
				a.cfg.FontSize = s.fontSize
				a.applyEditorPrefsAll()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgFontWeight(), locales.MsgTheWeightOfRegularEditorText())
			weight := float64(a.cfg.EditorFontWeight)
			if weight == 0 {
				weight = 400
			}
			ui.Text(c, fmt.Sprintf("%g", weight)).TextColor(theme.Of(c).Muted)
			if ui.Stepper(c, &weight, 100, 900, 100).Label(locales.MsgEditorFontWeight()).Changed() {
				a.cfg.EditorFontWeight = int(weight)
				a.applyEditorPrefsAll()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgLineHeight(), locales.MsgExtraSpaceBetweenEditorRows())
			height := a.cfg.EditorLineHeight
			if height == 0 {
				height = 1.5
			}
			ui.Text(c, fmt.Sprintf("%g", height)).TextColor(theme.Of(c).Muted)
			if ui.Stepper(c, &height, 1, 3, 0.1).Label(locales.MsgEditorLineHeight()).Changed() {
				a.cfg.EditorLineHeight = height
				a.applyEditorPrefsAll()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgLigatures(), locales.MsgCombineSupportedCharacterSequences())
			enabled := !a.cfg.EditorNoLigatures
			ui.Switch(c, &enabled).Label(locales.MsgLigatures()).OnClick(func() { a.cfg.EditorNoLigatures = !enabled; a.applyEditorPrefsAll(); a.settingsChanged() })
		},
		func() {
			labeled(c, locales.MsgFontSmoothing(), locales.MsgDrawEditorTextWithAThickerStrokeOn())
			if ui.Switch(c, &a.cfg.EditorThicken).Label(locales.MsgFontSmoothing()).Changed() {
				a.applyEditorPrefsAll()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgFontFeatures(), locales.MsgCommaSeparatedOpenTypeFeatures())
			if a.settings.editorFeaturesDraft == "" && len(a.cfg.EditorFontFeatures) > 0 {
				a.settings.editorFeaturesDraft = strings.Join(a.cfg.EditorFontFeatures, ", ")
			}
			if ui.TextInput(c, &a.settings.editorFeaturesDraft).Label(locales.MsgEditorFontFeatures()).Width(170).Changed() {
				var feats []string
				for _, f := range strings.Split(a.settings.editorFeaturesDraft, ",") {
					f = strings.TrimSpace(f)
					if f != "" {
						feats = append(feats, f)
					}
				}
				a.cfg.EditorFontFeatures = feats
				a.applyEditorPrefsAll()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgLineNumbers(), locales.MsgRelativeNumbersCountFromTheCaretSLine())
			if ui.Segmented(c, &s.lineNumbers, "On", locales.MsgRelative(), locales.MsgOff()).Label(locales.MsgLineNumbers()).Changed() {
				a.cfg.LineNumbers = s.lineNumbers
				a.applyEditorPrefsAll()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgWrapLines(), locales.MsgLongLinesWrapAtTheEditorSWidth())
			if ui.Switch(c, &a.cfg.SoftWrap).Label(locales.MsgWrapLines()).Changed() {
				a.applyEditorPrefsAll()
				a.settingsChanged()
			}
		},
	)
}

func (a *App) notificationsPane(c *ui.Context) {
	card(c,
		func() {
			labeled(c, locales.MsgNotifyWhenWaiting(), locales.MsgNotifyWhenATerminalOrAgentNeedsYou())
			if ui.Switch(c, &a.cfg.NotifyWaiting).Label(locales.MsgNotifyWaiting()).Changed() {
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgNotifyWhenDone(), locales.MsgNotifyWhenATaskFinishes())
			if ui.Switch(c, &a.cfg.NotifyDone).Label(locales.MsgNotifyDone()).Changed() {
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgNotifyOnFailure(), locales.MsgNotifyWhenSomethingFails())
			if ui.Switch(c, &a.cfg.NotifyFailed).Label(locales.MsgNotifyFailed()).Changed() {
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgPlaySound(), locales.MsgPlayASoundWithNotifications())
			if ui.Switch(c, &a.cfg.NotifySound).Label(locales.MsgPlaySound()).Changed() {
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgDockBadge(), locales.MsgShowABadgeOnTheAppIcon())
			if ui.Switch(c, &a.cfg.DockBadge).Label(locales.MsgDockBadge()).Changed() {
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgDockBounce(), locales.MsgBounceTheAppIconWhenNotifying())
			if ui.Switch(c, &a.cfg.BounceDock).Label(locales.MsgDockBounce()).Changed() {
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgTestNotification(), locales.MsgSendATestNotification())
			if ui.Button(c, locales.MsgTest()).Clicked() {
				n := mygo.NewNotification(mygo.NotificationOptions{Title: "See", Body: "Test notification", Silent: !a.cfg.NotifySound})
				go func() { _ = n.Show() }()
			}
		},
	)
}

func (a *App) intelligencePane(c *ui.Context) {
	heading(c, locales.LanguageServers())
	ui.Text(c, locales.IntelligenceDescription()).FontSize(12).TextColor(theme.Of(c).Muted).Padding(0, 4, 4, 4)
	goPath, goErr := findGopls(a.root)
	tsPath, tsErr := languages.FindTypeScript(a.root)
	clangPath, clangErr := languages.FindClangd(a.root)
	cmakePath, cmakeErr := languages.FindCMakeServer(a.root)
	ui.Column(c).FillWidth().Radius(12).Background(theme.Of(c).Card).Padding(0, 14).Children(func() {
		a.toolRow(c.Key("go"), toolRow{
			id: "gopls", language: "Go", server: "gopls",
			path: goPath, findErr: goErr, state: a.lspState, serverErr: a.lspErr, retry: a.retryLSP,
			managed: managedGoplsExists(), install: a.installManagedGopls, remove: a.removeManagedGopls,
			installLabel: locales.MsgInstallGopls(), removeLabel: locales.MsgRemoveManagedGopls(), hint: goplsInstallCommand(),
		})
		ui.Box(c).Height(1).FillWidth().Background(theme.Of(c).Separator)
		a.toolRow(c.Key("typescript"), toolRow{
			id: "typescript", language: "TypeScript & JavaScript", server: "tsrs",
			path: tsPath, findErr: tsErr, state: a.tsState, serverErr: a.tsErr, retry: a.retryTypeScriptLSP,
			managed: languages.ManagedTypeScriptExists(), install: a.installManagedTypeScript, remove: a.removeManagedTypeScript,
			installLabel: locales.MsgInstallTypeScriptJavaScript(), removeLabel: locales.MsgRemoveManagedTypeScriptServer(),
			hint: "npm install -g tsrs@0.20.1-ts7.1.0-beta",
		})
		ui.Box(c).Height(1).FillWidth().Background(theme.Of(c).Separator)
		a.toolRow(c.Key("clangd"), toolRow{id: "clangd", language: "C / C++ / Objective-C", server: "clangd", path: clangPath, findErr: clangErr, state: a.clangState, serverErr: a.clangErr, retry: a.retryClangd, hint: "https://clangd.llvm.org/installation"})
		ui.Box(c).Height(1).FillWidth().Background(theme.Of(c).Separator)
		a.toolRow(c.Key("cmake"), toolRow{id: "cmake", language: "CMake", server: "cmake-language-server", path: cmakePath, findErr: cmakeErr, state: a.cmakeState, serverErr: a.cmakeErr, retry: a.retryCMake, hint: "pip install cmake-language-server"})
	})
	heading(c, locales.TrustedFolders())
	ui.Text(c, locales.TrustedDescription()).FontSize(12).TextColor(theme.Of(c).Muted).Padding(0, 4, 4, 4)
	if len(a.cfg.Trusted) == 0 {
		card(c, func() {
			labeled(c, locales.NoTrustedFolders(), locales.TrustedHint())
		})
		return
	}
	var rows []func()
	for _, p := range a.cfg.Trusted {
		rows = append(rows, func() {
			folderGlyph(c, false, 18)
			labeled(c, filepath.Base(p), tildePath(p))
			if ui.Button(c, locales.Remove()).Label(locales.MsgRemoveTrustedFolder() + p).Clicked() {
				a.cfg.Trusted = slices.DeleteFunc(a.cfg.Trusted, func(t string) bool { return t == p })
				a.settingsChanged()
			}
		})
	}
	card(c, rows...)
}

// toolRow is a tool in Settings: a language server, or ripgrep, with See's
// own installation of it.
type toolRow struct {
	id, language, server string // "gopls", "Go", "gopls"
	path                 string
	findErr              error
	state                langserver.State
	serverErr            error
	retry                func()
	managed              bool              // See's copy is there, even if it cannot run
	install              func(ui.Services) // installs See's copy
	remove               func(ui.Services) // removes See's copy
	installLabel         string            // what its Install button says, and is found by
	removeLabel          string            // what its Remove button is found by
	hint                 string            // how to install it without See
}

func (a *App) toolRow(c *ui.Context, r toolRow) {
	status, color := locales.Available(), theme.Of(c).Muted
	switch {
	case r.state == langserver.Failed:
		status, color = locales.Unavailable(), theme.Of(c).Err
	case r.state == langserver.Starting:
		status = locales.Starting()
	case r.state == langserver.Ready:
		status = locales.Running()
	case r.findErr != nil:
		status = locales.NotInstalled()
	}
	detail := r.server
	if r.path != "" {
		detail += " · " + tildePath(r.path)
	}
	if r.state == langserver.Failed && r.serverErr != nil {
		detail = r.server + " · " + shortErr(r.serverErr)
	}
	ui.Column(c).FillWidth().Padding(12, 0).Gap(10).Children(func() {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
				ui.Text(c, r.language).Bold().SingleLine().Shrink(1)
				ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					dot := theme.Of(c).Muted
					if r.state == langserver.Ready {
						dot = c.Theme().Accent
					} else if r.state == langserver.Failed {
						dot = theme.Of(c).Err
					}
					ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0)
					ui.Text(c, status).FontSize(12).TextColor(color).SingleLine().Shrink(0)
					ui.Text(c, "· "+detail).FontSize(12).TextColor(theme.Of(c).Muted).SingleLine().Shrink(1).Tooltip(detail)
				})
			})
			a.toolButtons(c, r)
		})
		a.toolNote(c, r)
	})
}

// toolButtons are the buttons at the end of a tool's row: that a job is
// running, or Install when the tool was not found, or Remove when See
// installed it.
func (a *App) toolButtons(c *ui.Context, r toolRow) {
	if a.serverJob && a.serverJobName == r.id {
		ui.Text(c, locales.Working()).FontSize(12).TextColor(theme.Of(c).Muted)
		return
	}
	services := c.Services()
	retry := func() {
		if r.state == langserver.Failed && ui.Button(c.Key("retry"), locales.Retry()).Label(locales.MsgRetry()+r.server).Clicked() {
			r.retry()
		}
	}
	switch {
	case r.managed:
		retry()
		ui.Button(c.Key("remove"), locales.Remove()).Label(r.removeLabel).OnClick(func() { r.remove(services) })
	case r.findErr != nil:
		if r.install != nil {
			ui.Button(c.Key("install"), locales.Install()).Label(r.installLabel).OnClick(func() { r.install(services) })
		}
		if r.hint != "" {
			label := locales.OtherWays()
			if a.settings.serverInstall == r.id {
				label = locales.Done()
			}
			if ui.Button(c.Key("ways"), label).Label(locales.MsgOtherWaysToInstall() + r.server).Clicked() {
				if a.settings.serverInstall == r.id {
					a.settings.serverInstall = ""
				} else {
					a.settings.serverInstall = r.id
				}
			}
		}
	default:
		retry()
	}
}

// toolNote shows why an installation failed, and how to install the tool
// without See.
func (a *App) toolNote(c *ui.Context, r toolRow) {
	if a.serverJobName == r.id && a.serverJobErr != "" {
		ui.Text(c, a.serverJobErr).FontSize(12).TextColor(theme.Of(c).Err)
	}
	if a.settings.serverInstall != r.id || r.findErr == nil || r.managed || r.hint == "" {
		return
	}
	ui.Column(c).FillWidth().Gap(6).Padding(2, 0, 2, 0).Children(func() {
		ui.Text(c, locales.InstallHint()).FontSize(12).TextColor(theme.Of(c).Muted)
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, r.hint).FontSize(12).TextColor(theme.Of(c).Text).Grow(1).Shrink(1)
			services := c.Services()
			ui.Button(c.Key("copy"), locales.Copy()).Label(locales.MsgCopy() + r.server + locales.MsgInstallCommand()).OnClick(func() { services.WriteClipboard(r.hint) })
		})
	})
}

func (a *App) searchPane(c *ui.Context) {
	rg, err := findRipgrep(a.root)
	status := "Finds text in the workspace · " + tildePath(rg)
	if err != nil {
		status = "Not installed: Open Quickly lists files without it"
	}
	r := toolRow{
		id: "ripgrep", language: "ripgrep", server: "ripgrep",
		path: rg, findErr: err, state: langserver.Off,
		managed: managedRipgrepExists(), install: a.installManagedRipgrep, remove: a.removeManagedRipgrep,
		installLabel: locales.MsgInstallRipgrep(), removeLabel: locales.MsgRemoveManagedRipgrep(),
		hint: search.InstallHint(),
	}
	statusCard(c, icSearch, ui.RGB(0xff, 0x95, 0x00), "ripgrep", status, func() { a.toolButtons(c, r) })
	a.toolNote(c, r)
	heading(c, locales.MsgWhatIsSearched())
	card(c,
		func() {
			labeled(c, locales.MsgIgnoredFolders(), strings.Join(ignoredList(), ", ")+locales.MsgAndWhatGitignoreLeavesOut())
		},
		func() {
			labeled(c, locales.MsgOpenQuickly(), fmt.Sprintf(locales.MsgListsUpToDFilesByName(), maxQuickFiles))
		},
	)
}

func (a *App) shortcutsPane(c *ui.Context) {
	rows := []struct{ what, mods, key string }{}
	for _, cmd := range commands() {
		rows = append(rows, struct{ what, mods, key string }{cmd.name, cmd.mods, cmd.key})
	}
	var fns []func()
	for _, r := range rows {
		fns = append(fns, func() {
			labeled(c, r.what, "")
			if r.key != "" {
				ui.Text(c, keyLabel(r.mods, r.key)).TextColor(theme.Of(c).Muted).Padding(2, 8).Radius(6).Background(theme.Of(c).Pill)
			}
		})
	}
	card(c, fns...)
}

func (a *App) advancedPane(c *ui.Context) {
	card(c,
		func() {
			labeled(c, locales.MsgConfigFile(), locales.MsgOpenSeeSConfigurationFile())
			if ui.Button(c, locales.MsgOpen()).Clicked() {
				// reveal config dir
			}
			ui.Text(c, a.cfg.Path()).TextColor(theme.Of(c).Muted).FontSize(12)
		},
		func() {
			labeled(c, locales.MsgDataFolder(), locales.MsgOpenSeeSApplicationDataFolder())
			ui.Text(c, "~/.config/See").TextColor(theme.Of(c).Muted).FontSize(12)
		},
		func() {
			labeled(c, locales.MsgResetSettings(), locales.MsgResetAllSettingsToTheirDefaults())
			if ui.Button(c, locales.MsgReset()).Clicked() {
				// reset fields
				def := config.Load("")
				def.SetPath(a.cfg.Path())
				*a.cfg = *def
				a.settingsChanged()
			}
		},
	)
}

func (a *App) aboutPane(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(24, 0).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Image(c, appIcon).Size(96, 96)
		ui.Text(c, "See").FontSize(20).Bold()
		ui.Text(c, a.versionLabel()).TextColor(theme.Of(c).Muted)
		ui.Text(c, locales.MsgFastFocusedReadingOfSourceCodeDrawnNatively()).FontSize(12).TextColor(theme.Of(c).Muted)
	})
	if a.root != "" {
		card(c, func() { labeled(c, locales.MsgFolderOpen(), tildePath(a.root)) })
	}
}

func ignoredList() []string { return workspace.IgnoredNames() }

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (a *App) terminalPane(c *ui.Context) {
	s := &a.settings
	a.initFontValues()
	statusCard(c, icPrompt, ui.RGB(0x2c, 0x2c, 0x2e), shellName(),
		"Terminals run your login shell in the open folder, with Ghostty's emulator.", nil)
	a.cfg.ExternalTerminal = externalTerminalName(a.cfg.ExternalTerminal)
	heading(c, locales.MsgExternalTerminal())
	rows := []func(){func() {
		labeled(c, locales.MsgTerminalApp(), locales.MsgOpenTheCurrentFolderWith()+keyLabel("Cmd+Shift+", "C")+".")
		a.externalTerminalPicker(c)
	}}
	if a.cfg.ExternalTerminal == "Custom App" {
		rows = append(rows, func() {
			labeled(c, locales.MsgAppPath(), locales.MsgPathToATerminalApplicationThatCanOpen())
			ui.TextInput(c, &a.cfg.ExternalTerminalPath).Label(locales.MsgExternalTerminalAppPath()).Placeholder("/Applications/Terminal.app").Width(200).OnChange(func() {
				delete(a.settings.terminalIcons, "Custom App")
				a.settingsChanged()
				if strings.HasSuffix(strings.ToLower(a.cfg.ExternalTerminalPath), ".app") {
					a.host.LoadTerminalIcon(a.cfg.ExternalTerminalPath)
				}
			}).OnSubmit(func() {
				a.host.LoadTerminalIcon(a.cfg.ExternalTerminalPath)
			})
			if ui.Button(c, locales.MsgChoose()).Clicked() {
				a.host.PickTerminalApp()
			}
		})
	}
	card(c, rows...)
	heading(c, locales.MsgTypography())
	a.fontPreview(c, true)
	card(c,
		func() { a.fontPicker(c, true) },
		func() {
			labeled(c, locales.MsgFontSize(), locales.MsgTheTerminalFontSize())
			ui.Text(c, fmt.Sprintf("%g pt", s.termFontSize)).TextColor(theme.Of(c).Muted)
			if ui.Stepper(c, &s.termFontSize, 9, 28, 1).Label(locales.MsgTerminalFontSize()).Changed() {
				a.cfg.TermFontSize = s.termFontSize
				a.applyTerminalPrefs()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgFallbackFonts(), locales.MsgCommaSeparatedFamiliesUsedForMissingCharacters())
			if ui.TextInput(c, &a.cfg.TermFontFallback).Label(locales.MsgFallbackFonts()).Placeholder(locales.MsgSystemFallback()).Width(170).Changed() {
				a.applyTerminalPrefs()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgFontWeight(), locales.MsgTheWeightOfRegularTerminalText())
			weight := &s.termWeight
			ui.Text(c, fmt.Sprintf("%g", *weight)).TextColor(theme.Of(c).Muted)
			if ui.Stepper(c, weight, 100, 900, 100).Label(locales.MsgTerminalFontWeight()).Changed() {
				a.cfg.TermFontWeight = int(*weight)
				a.applyTerminalPrefs()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgFontSmoothing(), locales.MsgDrawTerminalTextWithAThickerStrokeOn())
			if ui.Switch(c, &a.cfg.TermThicken).Label(locales.MsgFontSmoothing()).Changed() {
				a.applyTerminalPrefs()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgLigatures(), locales.MsgCombineSupportedCharacterSequences())
			enabled := !a.cfg.TermNoLigatures
			ui.Switch(c, &enabled).Label(locales.MsgLigatures()).OnClick(func() { a.cfg.TermNoLigatures = !enabled; a.applyTerminalPrefs(); a.settingsChanged() })
		},
		func() {
			labeled(c, locales.MsgFontFeatures(), locales.MsgCommaSeparatedOpenTypeFeatures())
			if a.settings.termFeaturesDraft == "" && len(a.cfg.TermFontFeatures) > 0 {
				a.settings.termFeaturesDraft = strings.Join(a.cfg.TermFontFeatures, ", ")
			}
			if ui.TextInput(c, &a.settings.termFeaturesDraft).Label(locales.MsgTerminalFontFeatures()).Width(170).Changed() {
				// parse comma-separated
				var feats []string
				for _, f := range strings.Split(a.settings.termFeaturesDraft, ",") {
					f = strings.TrimSpace(f)
					if f != "" {
						feats = append(feats, f)
					}
				}
				a.cfg.TermFontFeatures = feats
				a.applyTerminalPrefs()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgLineHeight(), locales.MsgExtraSpaceBetweenTerminalRows())
			height := &s.termHeight
			ui.Text(c, fmt.Sprintf("%g %%", *height)).TextColor(theme.Of(c).Muted)
			if ui.Stepper(c, height, -20, 100, 5).Label(locales.MsgTerminalLineHeight()).Changed() {
				a.cfg.TermLineHeight = *height
				a.applyTerminalPrefs()
				a.settingsChanged()
			}
		},
	)
	heading(c, locales.MsgBehavior())
	card(c,
		func() {
			labeled(c, locales.MsgCopyOnSelect(), locales.MsgSelectingTextCopiesItToTheClipboardAs())
			if ui.Switch(c, &a.cfg.TermCopyOnSelect).Label(locales.MsgCopyOnSelect()).Changed() {
				a.applyTerminalPrefs()
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgCursor(), locales.MsgTheShapeOfTheTerminalCursor())
			cursor := 0
			switch a.cfg.TermCursor {
			case "bar":
				cursor = 1
			case "underline":
				cursor = 2
			}
			if ui.Segmented(c, &cursor, locales.MsgBlock(), locales.MsgBar(), locales.MsgUnderline()).Label(locales.MsgCursor()).Changed() {
				a.cfg.TermCursor = []string{"block", "bar", "underline"}[cursor]
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgCursorBlink(), locales.MsgLetTheCursorBlink())
			enabled := !a.cfg.TermCursorNoBlink
			if ui.Switch(c, &enabled).Label(locales.MsgCursorBlink()).Changed() {
				a.cfg.TermCursorNoBlink = !enabled
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgOptionAsAlt(), locales.MsgMakeOptionTheAltMetaKeyOnMacOS())
			if ui.Switch(c, &a.cfg.TermOptionAsAlt).Label(locales.MsgOptionAsAlt()).Changed() {
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgRightClick(), locales.MsgWhatASecondaryClickDoes())
			rc := 0
			switch a.cfg.TermRightClick {
			case "paste":
				rc = 1
			case "copyOrPaste":
				rc = 2
			}
			if ui.Segmented(c, &rc, locales.MsgMenu(), locales.MsgPaste(), locales.MsgCopyPaste()).Label(locales.MsgRightClick()).Changed() {
				a.cfg.TermRightClick = []string{"menu", "paste", "copyOrPaste"}[rc]
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgClipboardRead(), locales.MsgWhetherProgramsMayReadTheClipboard())
			cb := 0
			switch a.cfg.TermClipboard {
			case "allow":
				cb = 1
			case "deny":
				cb = 2
			}
			if ui.Segmented(c, &cb, locales.MsgAsk(), locales.MsgAllow(), locales.MsgDeny()).Label(locales.MsgClipboard()).Changed() {
				a.cfg.TermClipboard = []string{"ask", "allow", "deny"}[cb]
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgDimUnfocused(), locales.MsgFadeTerminalsThatDoNotHaveTheFocus())
			if ui.Switch(c, &a.cfg.TermDimUnfocused).Label(locales.MsgDimUnfocused()).Changed() {
				a.settingsChanged()
			}
		},
	)
	heading(c, locales.MsgScrolling())
	card(c,
		func() {
			labeled(c, locales.MsgWheelScroll(), locales.MsgRowsAMouseWheelNotchScrolls())
			v := a.cfg.TermScrollDiscrete
			if v == 0 {
				v = 3
			}
			ui.Text(c, fmt.Sprintf("%g", v)).TextColor(theme.Of(c).Muted)
			if ui.Stepper(c, &v, 1, 10, 1).Label(locales.MsgWheelScroll()).Changed() {
				a.cfg.TermScrollDiscrete = v
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgTrackpadScroll(), locales.MsgMultiplierForTrackpadScrolling())
			v := a.cfg.TermScrollPrecision
			if v == 0 {
				v = 1
			}
			ui.Text(c, fmt.Sprintf("%g", v)).TextColor(theme.Of(c).Muted)
			if ui.Stepper(c, &v, 0.25, 3, 0.25).Label(locales.MsgTrackpadScroll()).Changed() {
				a.cfg.TermScrollPrecision = v
				a.settingsChanged()
			}
		},
	)
	heading(c, locales.MsgShell())
	card(c,
		func() {
			labeled(c, locales.MsgShell(), locales.MsgTheShellTerminalsRun())
			if ui.TextInput(c, &a.cfg.TermShell).Label(locales.MsgTerminalShell()).Placeholder(locales.MsgLoginShell()).Width(200).Changed() {
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgScrollback(), locales.MsgMegabytesOfOutputKeptAboveTheScreen())
			v := float64(a.cfg.TermScrollback)
			if v == 0 {
				v = 4
			}
			ui.Text(c, fmt.Sprintf("%d MB", int(v))).TextColor(theme.Of(c).Muted)
			if ui.Stepper(c, &v, 0, 64, 1).Label(locales.MsgScrollback()).Changed() {
				a.cfg.TermScrollback = int(v)
				a.settingsChanged()
			}
		},
		func() {
			labeled(c, locales.MsgEnvironment(), locales.MsgExtraEnvironmentVariablesKEYValue())
			for i, kv := range a.cfg.TermEnv {
				idx := i
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, kv).FontSize(12).Grow(1).SingleLine().Ellipsis("…").MinWidth(0)
					if ui.Button(c, locales.Remove()).Label(locales.MsgRemove() + kv).Clicked() {
						a.cfg.TermEnv = slices.Delete(slices.Clone(a.cfg.TermEnv), idx, idx+1)
						a.settingsChanged()
					}
				})
			}
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				if ui.TextInput(c, &a.settings.envDraft).Label(locales.MsgEnvironmentVariable()).Placeholder("KEY=value").Width(200).Changed() {
				}
				if ui.Button(c, locales.MsgAdd()).Clicked() {
					if kv := strings.TrimSpace(a.settings.envDraft); strings.Contains(kv, "=") {
						a.cfg.TermEnv = append(slices.Clone(a.cfg.TermEnv), kv)
						a.settings.envDraft = ""
						a.settingsChanged()
					}
				}
			})
		},
	)
}

func (a *App) externalTerminalPicker(c *ui.Context) {
	t := c.Theme()
	sel := ui.SelectBase(c, &a.cfg.ExternalTerminal)
	label := func(name string) {
		if icon := a.settings.terminalIcons[name]; icon != nil {
			ui.Image(c, icon).Size(18, 18).Shrink(0)
		} else {
			symbolIcon(c, icPrompt, 18, theme.Of(c).Muted).Shrink(0)
		}
		display := name
		if name == "Custom App" && a.cfg.ExternalTerminalPath != "" {
			display = strings.TrimSuffix(filepath.Base(a.cfg.ExternalTerminalPath), ".app")
		}
		ui.Text(c, display).SingleLine()
	}
	sel.Trigger.Label(locales.MsgExternalTerminalApp()).Gap(8).Padding(6, 10).Radius(8).Background(t.Surface).Border(1, t.Border).AlignItems(ui.Center).Children(func() {
		label(a.cfg.ExternalTerminal)
		symbolIcon(c, icChevron, 10, theme.Of(c).Muted)
	})
	sel.Trigger.OnChange(a.settingsChanged)
	sel.Popup(func(panel ui.Element) {
		panel.Padding(4).Radius(10).Background(t.Background).Border(1, t.Border).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.15))
		for _, name := range externalTerminalNames {
			item := sel.Item(name).Gap(8).Padding(6, 10).Radius(6).AlignItems(ui.Center)
			if item.Highlighted() {
				item.Background(t.Accent).TextColor(t.AccentText)
			}
			item.Children(func() { label(name) })
		}
	})
}

func settingsPaneTitle(id string) string {
	switch id {
	case "general":
		return locales.General()
	case "editor":
		return locales.Editor()
	case "terminal":
		return locales.Terminal()
	case "notifications":
		return locales.Notifications()
	case "intelligence":
		return locales.CodeIntelligence()
	case "advanced":
		return locales.Advanced()
	case "search":
		return locales.Search()
	case "shortcuts":
		return locales.Shortcuts()
	case "about":
		return locales.About()
	}
	return id
}
