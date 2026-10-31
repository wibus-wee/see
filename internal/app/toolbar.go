package app

import (
	"path/filepath"
	"see/internal/languages"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"see/internal/langserver"
)

// toolbarHeight leaves room for the glass controls and window buttons.
const toolbarHeight = 48

// toolbar aligns the navigator controls with the sidebar and groups the
// document navigation and editor actions over the workspace.
func (a *App) toolbar(c *ui.Context, sidebarProgress float32, vibrant bool) {
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
	// The sidebar runs up to the window's top edge, as macOS 27's: the
	// toolbar's hairline is under the content alone, and the sidebar's
	// header joins the navigator bar below it without a line.
	row := ui.Row(c).FillWidth().Height(max(bar.Height, toolbarHeight)).
		AlignItems(ui.Stretch).DragWindow()
	row.Children(func() {
		sidebarHeader := ui.Row(c).Width(width).MinWidth(bar.Left+44).Shrink(1).FillHeight().Padding(0, 0, 0, max(bar.Left, 8)+12).
			AlignItems(ui.Center)
		if !a.sidebarOff {
			sidebarHeader.Background(sidebarColor(c, vibrant)).BorderWidth(0, 1, 0, 0).BorderColor(colors(c).Separator)
		} else {
			sidebarHeader.Background(colors(c).Toolbar).BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Hairline)
		}
		sidebarHeader.Children(func() {
			if glassToolbarButton(c, icSidebar, "Show Navigator", "Hide or Show the Navigator", !a.sidebarOff).Clicked() {
				a.sidebarOff = !a.sidebarOff
			}
		})
		ui.Row(c).Grow(1).Shrink(1).FillHeight().Padding(0, bar.Right+8, 0, 6).Gap(6).AlignItems(ui.Center).Background(colors(c).Toolbar).
			BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Hairline).Children(func() {
			ui.Row(c).Height(32).Radius(16).Padding(0, 3).AlignItems(ui.Center).Material(glass.Glass{}).Children(func() {
				back := iconButtonWithState(c, icBack, "Go Back", "Go Back", false, len(a.hist.Back) == 0, 13)
				if back.Clicked() {
					a.goBack()
				}
				ui.Box(c).Size(1, 16).Background(colors(c).Separator)
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

func (a *App) quickButton(c *ui.Context, centered bool, x, y float32) {
	quick := ui.ButtonBase(c).Height(32).Padding(0, 12).Radius(16).Shrink(1).KeepFocus().
		Label("Open Quickly").Tooltip("Open Quickly (" + keyLabel("Cmd+Shift+", "O") + ")").Material(glass.Glass{Interactive: true})
	if centered {
		quick.Absolute().Left(x).Top(y).Width(132).Center()
	}
	quick.Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			symbolIcon(c, icSearch, 15, colors(c).Text)
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
func (a *App) projectTitle(c *ui.Context) {
	b := ui.ButtonBase(c).Label("Switch Project").Tooltip(tildePath(a.root)).
		Padding(0, 5, 0, 0).Radius(6).Shrink(1).Bind(&a.titleButton)
	if b.Hovered() || b.Pressed() {
		b.Background(colors(c).Pill)
	}
	b.OnClick(func() {
		r := a.titleButton.Bounds(c)
		a.projectMenu(int(r.X), int(r.Y+r.H+4))
	})
	b.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, filepath.Base(a.root)).Bold().SingleLine().Shrink(1)
			symbolIcon(c, icChevron, 9, colors(c).Dim).Shrink(0)
		})
	})
}

// projectMenu opens the recent folders at x, y in the window, as Xcode's
// Open Recent: the folder open is checked, and folders with the same name
// show their parent's too.
func (a *App) projectMenu(x, y int) {
	var items []*mygo.MenuItem
	for _, p := range a.cfg.Recent {
		p := p
		items = append(items, &mygo.MenuItem{
			Label:   projectLabel(a.cfg.Recent, p),
			ToolTip: p,
			Type:    mygo.MenuItemCheckbox,
			Checked: p == a.root,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.post.Post(func() { a.openRecent(p) })
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
			a.post.Post(a.openFolderDialog)
		},
	})
	a.host.PopupMenu(x, y, items)
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
func (a *App) modeSwitch(c *ui.Context) {
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
func (a *App) activityView(c *ui.Context) {
	state, serverErr, name := a.lspState, a.lspErr, "gopls"
	retry := a.retryLSP
	if a.focus != nil && languages.TypeScriptLanguage(a.activePath()) != "" {
		state, serverErr, name = a.tsState, a.tsErr, "tsrs"
		retry = a.retryTypeScriptLSP
	}
	pill := ui.Row(c).Height(16).MaxWidth(260).Shrink(1).Gap(5).AlignItems(ui.Center)
	pill.Children(func() {
		// The dot tells the state at a glance, as Peek's toolbar does: the
		// accent while work goes on, red when it went wrong.
		dot := colors(c).Muted
		switch {
		case a.finding, a.find.running, state == langserver.Starting:
			dot = c.Theme().Accent
		case a.noticeError, state == langserver.Failed, a.watchErr != nil:
			dot = colors(c).Err
		}
		if dot != colors(c).Muted {
			ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0)
		}
		switch {
		case a.finding:
			ui.Spinner(c).FontSize(11)
			ui.Text(c, a.findingLabel).FontSize(12).TextColor(colors(c).Muted).SingleLine()
		case a.notice != "":
			t := ui.Text(c, a.notice).FontSize(12).SingleLine().Shrink(1).Tooltip(a.notice)
			if a.noticeError {
				t.TextColor(colors(c).Err)
			}
			if state == langserver.Failed && ui.Button(c, "Retry").Label("Retry "+name).Padding(0, 6).FontSize(11).Clicked() {
				retry()
			}
		case a.find.running:
			ui.Spinner(c).FontSize(11)
			ui.Text(c, "Searching…").FontSize(12).TextColor(colors(c).Muted)
		case state == langserver.Starting:
			ui.Spinner(c).FontSize(11)
			ui.Text(c, "Starting "+name+"…").FontSize(12).TextColor(colors(c).Muted)
		case state == langserver.Failed:
			msg := name + " unavailable"
			if serverErr != nil {
				msg = name + ": " + shortErr(serverErr)
			}
			ui.Text(c, msg).FontSize(12).TextColor(colors(c).Err).SingleLine().Shrink(1)
			if ui.Button(c, "Retry").Label("Retry "+name).Padding(0, 6).FontSize(11).Clicked() {
				retry()
			}
		case a.watchErr != nil:
			ui.Text(c, "Not watching for changes").FontSize(12).TextColor(colors(c).Muted).Tooltip(a.watchErr.Error())
		case state == langserver.Ready:
			ui.Text(c, "Ready · "+name).FontSize(12).TextColor(colors(c).Muted)
		default:
			if a.git.branch != "" {
				symbolIcon(c, icGit, 12, colors(c).Muted)
				ui.Text(c, a.git.branch).FontSize(12).TextColor(colors(c).Muted).SingleLine().Shrink(1).Tooltip("Git branch: " + a.git.branch)
			} else {
				ui.Text(c, "Ready").FontSize(12).TextColor(colors(c).Muted)
			}
		}
	})
}
