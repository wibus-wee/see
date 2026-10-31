package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"see/internal/assets"
	"see/internal/workspace"
)

// The welcome window, after Xcode's: the app's icon, name and actions on
// the left, the folders opened lately on the right.

var (
	appIcon    = assets.Icon
	folderIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7.5A2.5 2.5 0 0 1 5.5 5h3.6l2 2.2h7.4A2.5 2.5 0 0 1 21 9.7v7.8a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 17.5z"/></svg>`))
	openIcon   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7.5A2.5 2.5 0 0 1 5.5 5h3.6l2 2.2h7.4A2.5 2.5 0 0 1 21 9.7V11"/><path d="M3 7.5v10A2.5 2.5 0 0 0 5.5 20h12.2a2 2 0 0 0 1.9-1.4l1.8-5.6a1.5 1.5 0 0 0-1.4-2H8.3a2 2 0 0 0-1.9 1.4L4 19.5"/></svg>`))
	reopenIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l3 2"/></svg>`))
)

// welcomeWindow fills the welcome window (welcomeWindowSize), its title
// bar hidden behind the left side, as Xcode's.
func (a *App) welcomeWindow(c *ui.Context) {
	ui.Row(c).Fill().DragWindow().Children(func() {
		a.welcomeIntro(c)
		a.welcomeRecent(c)
	})
}

// The welcome window's size, which does not change, as Xcode's.
const welcomeWidth, welcomeHeight = 740, 440

// welcomeIntro is the left side: the icon, the name, the version and what
// to do.
func (a *App) welcomeIntro(c *ui.Context) {
	ui.Column(c).Grow(1).Shrink(1).FillHeight().Background(colors(c).welcomeLeft).Padding(16, 28, 18, 28).Children(func() {
		ui.Box(c).Height(26) // under the window's buttons
		ui.Column(c).Grow(1).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Image(c, appIcon).Size(120, 120)
			ui.Text(c, "See").FontSize(28).Bold().TextColor(colors(c).text).TextAlign(ui.Center)
			if a.version != "" {
				ui.Text(c, "Version "+a.version).FontSize(13).TextColor(colors(c).muted)
			}
			ui.Box(c).Height(12)
			ui.Column(c).Gap(2).Width(300).Children(func() {
				a.welcomeAction(c, openIcon, "Open Folder…", "Read the source of a folder", shortcutLabel("O"), a.openFolderDialog)
				if last := a.lastFolder(); last != "" {
					a.welcomeAction(c, reopenIcon, "Reopen "+filepath.Base(last), tildePath(last), "", func() { a.openRecent(last) })
				}
			})
			if a.welcomeNote != "" {
				ui.Text(c, a.welcomeNote).FontSize(12).TextColor(colors(c).err).MaxWidth(380).TextAlign(ui.Center).Padding(8, 0, 0, 0)
			}
		})
		showWelcome := !a.cfg.ReopenLast
		ui.Checkbox(c, &showWelcome, "Show this window when See launches").FontSize(12).TextColor(colors(c).muted).OnClick(func() {
			a.cfg.ReopenLast = !showWelcome
			a.saveConfig()
		})
	})
}

// lastFolder is the folder opened last, unless it is the one open now.
func (a *App) lastFolder() string {
	if len(a.cfg.Recent) == 0 || a.cfg.Recent[0] == a.root {
		return ""
	}
	return a.cfg.Recent[0]
}

func (a *App) welcomeAction(c *ui.Context, icon *ui.SVG, title, detail, shortcut string, fn func()) {
	row := ui.Row(c).FillWidth().Padding(6, 8).Gap(10).AlignItems(ui.Center).Radius(8).Cursor(ui.CursorPointer).Label(title)
	if row.Hovered() {
		row.Background(colors(c).welcomeHover)
	}
	if shortcut != "" {
		row.Tooltip(shortcut)
	}
	row.OnClick(fn).Children(func() {
		ui.Box(c).Size(30, 30).Shrink(0).Radius(7).Background(colors(c).welcomeTile).Center().Children(func() {
			symbolIcon(c, icon, 16, colors(c).text)
		})
		ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
			ui.Text(c, title).FontSize(13).Bold().TextColor(colors(c).text).SingleLine()
			ui.Text(c, detail).FontSize(11).TextColor(colors(c).muted).SingleLine()
		})
	})
}

// welcomeRecent is the right side: the folders opened lately, newest
// first.
func (a *App) welcomeRecent(c *ui.Context) {
	ui.Column(c).Width(280).Shrink(0).FillHeight().Background(colors(c).welcomeRight).
		BorderWidth(0, 0, 0, 1).BorderColor(colors(c).separator).Padding(10, 8).Children(func() {
		recent := a.cfg.Recent
		if len(recent) == 0 {
			ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
				ui.Text(c, "No Recent Folders").FontSize(13).TextColor(colors(c).dim)
			})
			return
		}
		var forget string
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				for i, p := range recent {
					a.recentRow(c.Key(i), p, &forget)
				}
			})
		})
		if forget != "" {
			a.cfg.removeRecent(forget)
			a.saveConfig()
		}
	})
}

func (a *App) recentRow(c *ui.Context, p string, forget *string) {
	current := p == a.root
	row := ui.Row(c).FillWidth().Padding(7, 10).Gap(10).AlignItems(ui.Center).Radius(7).Cursor(ui.CursorPointer).Label(p).Tooltip(p)
	switch {
	case current:
		row.Background(a.theme.Selection)
	case row.Hovered():
		row.Background(colors(c).welcomeHover)
	}
	row.OnClick(func() { a.openRecent(p) })
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("Open").Chosen() {
			a.openRecent(p)
		}
		m.Separator()
		if m.Item("Remove from Recents").Chosen() {
			*forget = p
		}
		if m.Item("Clear Recents").Chosen() {
			a.cfg.clearRecent()
		}
	})
	row.Children(func() {
		symbolIcon(c, folderIcon, 22, colors(c).folder)
		ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
			ui.Text(c, filepath.Base(p)).FontSize(13).Bold().TextColor(colors(c).text).SingleLine()
			ui.Text(c, tildePath(filepath.Dir(p))).FontSize(11).TextColor(colors(c).muted).SingleLine()
		})
	})
}

// openRecent opens a recent folder, or goes back to it when it is open.
func (a *App) openRecent(p string) {
	if p == a.root {
		a.showWelcome = false
		return
	}
	a.openProject(p)
}

// tildePath shows a path in the home directory with ~.
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !workspace.Within(home, p) {
		return p
	}
	if p == home {
		return "~"
	}
	return "~" + string(filepath.Separator) + strings.TrimPrefix(p, home+string(filepath.Separator))
}
