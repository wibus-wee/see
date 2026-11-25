package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"see/locales"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"see/internal/assets"
	"see/internal/proc"
	"see/internal/workspace"
)

// The welcome window, after Xcode's: the app's icon, name and actions on
// the left, the folders opened lately on the right.

var (
	appIcon      = assets.Icon
	folderIcon   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7.5A2.5 2.5 0 0 1 5.5 5h3.6l2 2.2h7.4A2.5 2.5 0 0 1 21 9.7v7.8a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 17.5z"/></svg>`))
	documentIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3H6.5A2.5 2.5 0 0 0 4 5.5v13A2.5 2.5 0 0 0 6.5 21h11a2.5 2.5 0 0 0 2.5-2.5V9z"/><path d="M14 3v6h6M12 12v6M9 15h6"/></svg>`))
	openIcon     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7.5A2.5 2.5 0 0 1 5.5 5h3.6l2 2.2h7.4A2.5 2.5 0 0 1 21 9.7V11"/><path d="M3 7.5v10A2.5 2.5 0 0 0 5.5 20h12.2a2 2 0 0 0 1.9-1.4l1.8-5.6a1.5 1.5 0 0 0-1.4-2H8.3a2 2 0 0 0-1.9 1.4L4 19.5"/></svg>`))
	reopenIcon   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l3 2"/></svg>`))
	cloneIcon    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="6" cy="5" r="2.4"/><circle cx="6" cy="19" r="2.4"/><circle cx="18" cy="7" r="2.4"/><path d="M6 7.4v9.2M18 9.4c0 4.8-6 3.6-8.4 7.2"/></svg>`))
)

// welcomeClone is the Clone Repository dialog of the welcome window: the
// address and the folder to clone into, and while git runs its error.
type welcomeClone struct {
	open          bool
	url, dir, err string
	cloning       bool
}

// welcomeWindow fills the welcome window (welcomeWindowSize), its title
// bar hidden behind the left side, as Xcode's.
func (a *App) welcomeWindow(c *ui.Context) {
	ui.Row(c).Fill().DragWindow().Children(func() {
		a.welcomeIntro(c)
		a.welcomeRecent(c)
	})
	if a.clone.open {
		a.clonePanel(c)
	}
}

// welcomeText gets welcome-window copy from the catalog without requiring the
// generated convenience methods, which may lag newly added catalog entries.
func welcomeText(key string) string { return locales.Catalog.Format(i18n.Locale(), key, nil) }

// showInFolderLabel names the file manager's reveal command, as the
// platform calls it.
func showInFolderLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return welcomeText("welcomeShowInFinder")
	case "windows":
		return welcomeText("welcomeShowInExplorer")
	default:
		return welcomeText("welcomeShowInFileManager")
	}
}

// The welcome window's size, which does not change, as Xcode's.
const welcomeWidth, welcomeHeight = 740, 480

// welcomeIntro is the left side: the icon, the name, the version and what
// to do.
func (a *App) welcomeIntro(c *ui.Context) {
	ui.Column(c).Grow(1).Shrink(1).FillHeight().Background(colors(c).WelcomeLeft).Padding(16, 28, 18, 28).Children(func() {
		ui.Box(c).Height(26) // under the window's buttons
		// The center scrolls rather than run into the checkbox below when the
		// actions and the note outgrow the window.
		ui.Scroll(c).Grow(1).Shrink(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(5).Children(func() {
				ui.Image(c, appIcon).Size(96, 96)
				ui.Text(c, "See").FontSize(28).Bold().TextColor(colors(c).Text).TextAlign(ui.Center)
				if a.version != "" {
					ui.Text(c, locales.MsgVersion()+a.version).FontSize(13).TextColor(colors(c).Muted)
				}
				ui.Box(c).Height(6)
				ui.Column(c).Gap(2).Width(300).Children(func() {
					a.welcomeAction(c, documentIcon, locales.MsgNewFile2(), welcomeText("welcomeNewFileDetail"), shortcutLabel("N"), true, a.newFile)
					a.welcomeAction(c, openIcon, locales.MsgOpenFile2(), welcomeText("welcomeOpenFileDetail"), "", false, a.openFileDialog)
					a.welcomeAction(c, openIcon, locales.MsgOpenFolder2(), welcomeText("welcomeOpenFolderDetail"), shortcutLabel("O"), false, a.openFolderDialog)
					a.welcomeAction(c, cloneIcon, welcomeText("welcomeCloneRepository"), welcomeText("welcomeCloneDetail"), "", false, func() {
						a.clone = welcomeClone{open: true}
					})
					if last := a.lastFolder(); last != "" {
						a.welcomeAction(c, reopenIcon, welcomeText("welcomeReopenRecent"), filepath.Base(last)+" — "+tildePath(last), "", false, func() { a.openRecent(last) })
					}
				})
				if a.welcomeNote != "" {
					ui.Text(c, a.welcomeNote).FontSize(12).TextColor(colors(c).Err).MaxWidth(380).TextAlign(ui.Center).Padding(8, 0, 0, 0)
				}
			})
		})
		showWelcome := !a.cfg.ReopenLast
		ui.Checkbox(c, &showWelcome, welcomeText("welcomeShowOnLaunch")).FontSize(12).TextColor(colors(c).Muted).OnClick(func() {
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

func (a *App) welcomeAction(c *ui.Context, icon *ui.SVG, title, detail, shortcut string, autofocus bool, fn func()) {
	row := ui.ButtonBase(c).FillWidth().Padding(6, 8).Gap(10).AlignItems(ui.Center).Radius(8).Label(title)
	if autofocus {
		row.AutoFocus()
	}
	if row.Hovered() {
		row.Background(colors(c).WelcomeHover)
	}
	if shortcut != "" {
		row.Tooltip(shortcut)
	}
	row.OnClick(fn).Children(func() {
		ui.Box(c).Size(30, 30).Shrink(0).Radius(7).Background(colors(c).WelcomeTile).Center().Children(func() {
			symbolIcon(c, icon, 16, colors(c).Text)
		})
		ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
			ui.Text(c, title).FontSize(13).Bold().TextColor(colors(c).Text).SingleLine()
			ui.Text(c, detail).FontSize(11).TextColor(colors(c).Muted).SingleLine()
		})
		if shortcut != "" {
			ui.Text(c, shortcut).FontSize(11).TextColor(colors(c).Dim).Shrink(0)
		}
	})
}

// welcomeRecent is the right side: the folders opened lately, newest
// first.
func (a *App) welcomeRecent(c *ui.Context) {
	ui.Column(c).Width(280).Shrink(0).FillHeight().Background(colors(c).WelcomeRight).
		BorderWidth(0, 0, 0, 1).BorderColor(colors(c).Separator).Padding(10, 8).Children(func() {
		// The window's buttons sit over the top-right on Linux and
		// Windows; keep the first row clear of them.
		if bar := c.TitleBar(); bar.Right > 0 {
			ui.Box(c).Height(max(bar.Bottom-10, 0))
		}
		recent := a.cfg.Recent
		if len(recent) == 0 {
			ui.Column(c).Grow(1).FillWidth().Center().Gap(4).Children(func() {
				ui.Text(c, locales.MsgNoRecentFolders()).FontSize(13).TextColor(colors(c).Dim)
				ui.Text(c, welcomeText("welcomeNoRecentFoldersDetail")).FontSize(11).TextColor(colors(c).Muted)
			})
			return
		}
		var forget string
		clear := false
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				for _, p := range recent {
					a.recentRow(c.Key(p), p, &forget, &clear)
				}
			})
		})
		switch {
		case clear:
			a.cfg.ClearRecent()
		case forget != "":
			a.cfg.RemoveRecent(forget)
			a.saveConfig()
		}
	})
}

func (a *App) recentRow(c *ui.Context, p string, forget *string, clear *bool) {
	current := p == a.root
	row := ui.Row(c).FillWidth().Padding(7, 10).Gap(10).AlignItems(ui.Center).Radius(7).Cursor(ui.CursorPointer).Label(p).Tooltip(p)
	switch {
	case current:
		row.Background(a.theme.Selection)
	case row.Hovered():
		row.Background(colors(c).WelcomeHover)
	}
	row.OnClick(func() { a.openRecent(p) })
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.MsgOpen()).Chosen() {
			a.openRecent(p)
		}
		if m.Item(showInFolderLabel()).Chosen() {
			mygo.Shell.ShowItemInFolder(p)
		}
		m.Separator()
		if m.Item(welcomeText("welcomeRemoveFromRecents")).Chosen() {
			*forget = p
		}
		if m.Item(locales.MsgClearRecents()).Chosen() {
			*clear = true
		}
	})
	row.Children(func() {
		symbolIcon(c, folderIcon, 22, colors(c).Folder)
		ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
			ui.Text(c, filepath.Base(p)).FontSize(13).Bold().TextColor(colors(c).Text).SingleLine().Ellipsis("…").TruncateMiddle()
			ui.Text(c, tildePath(filepath.Dir(p))).FontSize(11).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").TruncateMiddle()
		})
	})
}

// clonePanel is the Clone Repository dialog: an address to clone and the
// folder to put it in, over the welcome window as the quick-open panel is
// over the editor.
func (a *App) clonePanel(c *ui.Context) {
	q := &a.clone
	if c.Shortcut(0, ui.KeyEscape) && !q.cloning {
		q.open = false
		return
	}
	ui.Overlay(c, func() {
		backdrop := ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Background(ui.RGBA(0, 0, 0, 0.18))
		backdrop.OnClick(func() {
			if !q.cloning {
				q.open = false
			}
		})
		backdrop.Children(func() {
			panel := ui.Column(c).Width(420).Radius(12).Padding(14).Gap(8).
				Background(colors(c).Popover).Border(1, colors(c).PopoverEdge).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.5))
			panel.OnClick(func() {}) // clicks on the panel stay on it
			panel.Children(func() {
				ui.Text(c, welcomeText("welcomeCloneRepository")).FontSize(14).Bold().TextColor(colors(c).Text)
				ui.Text(c, welcomeText("welcomeCloneURL")).FontSize(11).TextColor(colors(c).Muted)
				urlField := ui.TextInput(c, &q.url).Label(welcomeText("welcomeCloneURL")).Placeholder("https://github.com/owner/repo.git").
					FontSize(13).FillWidth().Height(28).Padding(0, 8).AutoFocus()
				if urlField.Submitted() {
					a.cloneRepository()
				}
				ui.Text(c, welcomeText("welcomeCloneLocation")).FontSize(11).TextColor(colors(c).Muted).Padding(4, 0, 0, 0)
				ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					dirField := ui.TextInput(c, &q.dir).Label(welcomeText("welcomeCloneLocation")).Placeholder(tildePath(cloneDirSuggestion(q.url))).
						FontSize(13).Grow(1).Height(28).Padding(0, 8)
					if dirField.Submitted() {
						a.cloneRepository()
					}
					if ui.Button(c, welcomeText("welcomeCloneChoose")).Label(welcomeText("welcomeCloneChoose")).Height(28).Disabled(q.cloning).Clicked() {
						a.pickCloneDir()
					}
				})
				switch {
				case q.cloning:
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Spinner(c)
						ui.Text(c, fmt.Sprintf(welcomeText("welcomeCloning"), q.url)).FontSize(12).TextColor(colors(c).Muted)
					})
				case q.err != "":
					ui.Text(c, q.err).FontSize(12).TextColor(colors(c).Err).Wrap()
				}
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Grow(1)
					if ui.Button(c, locales.Cancel()).Label(locales.Cancel()).Height(26).Disabled(q.cloning).Clicked() {
						q.open = false
					}
					if ui.Button(c, welcomeText("welcomeCloneButton")).Label(welcomeText("welcomeCloneButton")).Height(26).Disabled(q.cloning).Clicked() {
						a.cloneRepository()
					}
				})
			})
		})
	})
}

// pickCloneDir asks for the folder to clone into, off the UI thread.
func (a *App) pickCloneDir() {
	pick := a.host.PickFolder
	go func() {
		p, err := pick()
		a.post.Post(func() {
			switch {
			case err != nil:
				a.clone.err = locales.MsgCouldNotShowTheFolderDialog() + err.Error()
			case p != "":
				a.clone.dir = p
			}
		})
	}()
}

// cloneDirSuggestion is where a clone of url would go: the repository's
// name under ~/Projects, or the home folder when it cannot be told.
func cloneDirSuggestion(url string) string {
	name := cloneRepoName(url)
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return name
	}
	return filepath.Join(home, "Projects", name)
}

// cloneRepoName is the folder name a clone of url takes, as git chooses
// it: the path's last part, without .git.
func cloneRepoName(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, "/")
	if i := strings.LastIndexAny(url, "/:"); i >= 0 {
		url = url[i+1:]
	}
	return strings.TrimSuffix(url, ".git")
}

// cloneRepository runs git clone and opens the cloned folder, as the
// dialog says it does. While it runs, Escape and the dialog's buttons
// stay quiet; a failure leaves the dialog open with git's message.
func (a *App) cloneRepository() {
	q := &a.clone
	if q.cloning {
		return
	}
	url := strings.TrimSpace(q.url)
	dir := strings.TrimSpace(q.dir)
	switch {
	case url == "":
		q.err = welcomeText("welcomeCloneEnterURL")
		return
	case dir == "":
		dir = cloneDirSuggestion(url)
	case strings.HasPrefix(dir, "~"):
		dir = expandTilde(dir)
	}
	q.cloning, q.err = true, ""
	post := a.post.Post
	go func() {
		target := filepath.Join(dir, cloneRepoName(url))
		err := gitClone(url, target)
		post(func() {
			q.cloning = false
			if err != nil {
				q.err = welcomeText("welcomeCloneFailed") + gitErrLine(err)
				return
			}
			q.open = false
			a.openProject(target)
		})
	}()
}

// gitClone clones url into target. Unlike git.Command a clone may run for
// longer than its 30-second timeout, so it is run here with the same
// prompt-suppressing environment rather than through it. It is a variable
// so tests can see the call without a network.
var gitClone = func(url, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := proc.CommandContext(ctx, "git", "clone", "--progress", "--", url, target)
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// expandTilde makes a path under the user's home of a leading ~.
func expandTilde(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~"+string(filepath.Separator)) && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimLeft(p[2:], "/\\"))
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
