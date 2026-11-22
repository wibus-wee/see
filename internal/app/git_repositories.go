package app

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"see/internal/git"
	"see/locales"
)

var (
	icGitTag     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><path d="M3 4h8l10 10-7 7L3 10z"/><circle cx="7.5" cy="8" r="0.8"/></svg>`))
	icGitRemote  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7"><circle cx="12" cy="12" r="9"/><ellipse cx="12" cy="12" rx="4" ry="9"/><path d="M3 12h18"/></svg>`))
	icGitCurrent = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12 4 4 10-10"/></svg>`))
)

type repositoryNavigator struct {
	page                                                  int
	rootClosed, branchesClosed, tagsClosed, remotesClosed bool
	closedRemotes                                         map[string]bool
	tag                                                   *gitTagDialog
}

type gitTagDialog struct {
	hash, target, name, message, err string
	saving                           bool
}

func (a *App) gitNavigatorTabs(c *ui.Context) {
	page := &a.git.repositoryView.page
	ui.Row(c).FillWidth().Shrink(0).Padding(0, sidebarInset, 4, sidebarInset).Children(func() {
		tabs := ui.TabsBase(c, page, 2)
		tabs.List.FillWidth().Height(gitSidebarTabsHeight).AlignItems(ui.Stretch).Children(func() {
			for i, label := range []string{locales.GitRepositoryChanges(), locales.GitRepositories()} {
				tab := tabs.Tab(i).Grow(1).Basis(0).MinWidth(0).Shrink(1).FillHeight().Padding(0, 6).Label(label)
				tab.TextColor(colors(c).Muted)
				if i == *page {
					tab.TextColor(c.Theme().Text).FontWeight(500).
						BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Separator)
				}
				tab.Children(func() {
					ui.Text(c, label).FontSize(gitSidebarGroupLabelSize).SingleLine().Ellipsis("…").MinWidth(0)
				})
			}
		})
	})
}

func (a *App) gitRepositories(c *ui.Context) {
	v, repo := &a.git.repositoryView, a.git.repository
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Padding(0, sidebarInset, 6, sidebarInset).Children(func() {
			repoDisclosure(c.Key("root"), 0, filepath.Base(a.git.root), icGit, v.rootClosed,
				func() { v.rootClosed = !v.rootClosed }, nil)
			if v.rootClosed {
				return
			}
			repoDisclosure(c.Key("branches"), 1, locales.GitRepositoryBranches(), icBranches, v.branchesClosed,
				func() { v.branchesClosed = !v.branchesClosed }, nil)
			if !v.branchesClosed {
				for _, ref := range repo.Branches {
					current := ref.Name == repo.Current
					a.repositoryRef(c, 2, ref, icBranches, current, func(m *ui.Menu) {
						if m.Item(locales.GitSwitchToBranch()).Disabled(current || a.git.busy).Chosen() {
							ctx, root := a.wsCtx, a.git.root
							a.runBranchJob(locales.GitSwitchedToBranch(ref.Name), func() error {
								return git.Switch(ctx, root, git.Branch{Name: ref.Name}, nil)
							})
						}
					})
				}
				if len(repo.Branches) == 0 {
					repoEmpty(c.Key("no-branches"), 2, locales.GitNoBranches())
				}
			}
			repoDisclosure(c.Key("tags"), 1, locales.GitRepositoryTags(), icGitTag, v.tagsClosed,
				func() { v.tagsClosed = !v.tagsClosed }, func() {
					label := locales.GitCreateTagAction()
					if glyphButton(c, icAdd, label, label, false, a.git.busy || a.git.head == "").Clicked() {
						a.openCreateTag(a.git.head, a.git.branch)
					}
				})
			if !v.tagsClosed {
				for _, ref := range repo.Tags {
					a.repositoryRef(c, 2, ref, icGitTag, false, nil)
				}
				if len(repo.Tags) == 0 {
					repoEmpty(c.Key("no-tags"), 2, locales.GitNoTags())
				}
			}
			repoDisclosure(c.Key("remotes"), 1, locales.GitRepositoryRemotes(), icGitRemote, v.remotesClosed,
				func() { v.remotesClosed = !v.remotesClosed }, nil)
			if !v.remotesClosed {
				for _, remote := range repo.Remotes {
					closed := v.closedRemotes[remote.Name]
					repoDisclosure(c.Key("remote:"+remote.Name), 2, remote.Name, icGitRemote, closed, func() {
						if v.closedRemotes == nil {
							v.closedRemotes = make(map[string]bool)
						}
						v.closedRemotes[remote.Name] = !closed
					}, nil)
					if !closed {
						for _, ref := range remote.Branches {
							a.repositoryRef(c, 3, ref, icBranches, false, nil)
						}
						if len(remote.Branches) == 0 {
							repoEmpty(c.Key("empty:"+remote.Name), 3, locales.GitNoRemoteBranches())
						}
					}
				}
				if len(repo.Remotes) == 0 {
					repoEmpty(c.Key("no-remotes"), 2, locales.GitNoRemotes())
				}
			}
		})
	})
}

func repoDisclosure(c *ui.Context, depth float32, title string, icon *ui.SVG, closed bool, toggle func(), actions func()) {
	ui.Row(c).FillWidth().Height(24).Shrink(0).AlignItems(ui.Center).Children(func() {
		b := ui.ButtonBase(c).Grow(1).MinWidth(0).Height(24).Label(title).Tooltip(title).AlignItems(ui.Center).Justify(ui.Start).
			Padding(0, 4, 0, 4+depth*12).Gap(5).Radius(5).KeepFocus().OnClick(toggle)
		if b.Hovered() {
			b.Background(colors(c).Pill)
		}
		turn := float32(90)
		if closed {
			turn = 0
		}
		b.Children(func() {
			symbolIcon(c, icChevron, 8, colors(c).Dim).Rotate(turn)
			symbolIcon(c, icon, 13, colors(c).Muted)
			ui.Text(c, title).FontSize(12).Bold().SingleLine().Ellipsis("…").MinWidth(0)
		})
		if actions != nil {
			actions()
		}
	})
}

func repoEmpty(c *ui.Context, depth float32, label string) {
	ui.Text(c, label).Height(22).FontSize(11).TextColor(colors(c).Muted).Padding(2, 4, 0, 17+depth*12).SingleLine()
}

func (a *App) repositoryRef(c *ui.Context, depth float32, ref git.Ref, icon *ui.SVG, current bool, menu func(*ui.Menu)) {
	color := c.Theme().Accent
	label := ref.Name
	if current {
		label = locales.GitCurrentBranch(ref.Name)
	}
	row := ui.ButtonBase(c.Key(ref.FullName)).FillWidth().Height(22).Shrink(0).AlignItems(ui.Center).
		Label(label).Tooltip(label).Gap(5).Radius(5).Padding(0, 6, 0, 17+depth*12).KeepFocus().
		OnClick(func() {
			if h := &a.git.history; !current && !h.all {
				h.all, h.stale = true, true
			}
			a.revealLogRef(ref.FullName + "^{commit}")
		})
	if current || row.Hovered() {
		row.Background(color.Alpha(0.12))
	}
	if menu != nil {
		row.ContextMenu(menu)
	}
	row.Children(func() {
		symbolIcon(c, icon, 13, color)
		ui.Text(c, ref.Name).Grow(1).MinWidth(0).FontSize(12).SingleLine().Ellipsis("…")
		if current {
			symbolIcon(c, icGitCurrent, 13, color)
		}
	})
}

func (a *App) openCreateTag(hash, target string) {
	if !a.git.busy && hash != "" {
		a.git.repositoryView.tag = &gitTagDialog{hash: hash, target: target}
	}
}

func (a *App) createTagDialog(c *ui.Context) {
	q := a.git.repositoryView.tag
	closeDialog := func() {
		if !q.saving {
			a.git.repositoryView.tag = nil
		}
	}
	if c.Shortcut(0, ui.KeyEscape) && !q.saving {
		closeDialog()
		return
	}
	ui.Overlay(c, func() {
		ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Padding(12).
			Background(ui.RGBA(0, 0, 0, 0.18)).OnClick(closeDialog).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(380).Radius(12).Padding(14).Gap(6).
				Background(colors(c).Popover).Border(1, colors(c).PopoverEdge).
				Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.5)).OnClick(func() {}).Children(func() {
				ui.Text(c, locales.GitCreateTag()).FontSize(15).Bold()
				ui.Text(c, q.target).FontSize(12).TextColor(colors(c).Muted).SingleLine().Tooltip(q.target)
				name := locales.GitTagName()
				ui.Text(c, name).FontSize(12).Padding(4, 0, 0, 0)
				in := ui.TextInput(c, &q.name).Label(name).Placeholder("v1.0.0").FillWidth().
					Height(28).FontSize(13).Padding(0, 8).ReadOnly(q.saving).AutoFocus()
				if in.Submitted() {
					a.submitTag(q)
				}
				message := locales.GitTagMessage()
				ui.Text(c, message).FontSize(12).Padding(4, 0, 0, 0)
				ui.TextArea(c, &q.message).Label(message).FillWidth().Height(68).
					FontSize(13).Padding(5, 8).ReadOnly(q.saving)
				if q.err != "" {
					ui.Text(c, q.err).FontSize(12).TextColor(colors(c).Err).Wrap()
				}
				ui.Row(c).FillWidth().Gap(6).Padding(4, 0, 0, 0).AlignItems(ui.Center).Children(func() {
					if q.saving {
						ui.Spinner(c).FontSize(12)
					}
					ui.Box(c).Grow(1)
					if ui.Button(c, locales.Cancel()).Height(26).Disabled(q.saving).Clicked() {
						closeDialog()
					}
					if ui.Button(c, locales.GitCreateTag()).Height(26).Disabled(a.git.busy || strings.TrimSpace(q.name) == "").Clicked() {
						a.submitTag(q)
					}
				})
			})
		})
	})
}

func (a *App) submitTag(q *gitTagDialog) {
	name := strings.TrimSpace(q.name)
	if name == "" || q.saving || a.git.busy {
		return
	}
	q.saving, q.err, a.git.busy = true, "", true
	ctx, root, gen, hash, message := a.wsCtx, a.git.root, a.gen, q.hash, q.message
	go func() {
		err := git.CreateTag(ctx, root, name, message, hash)
		a.post.Post(func() {
			if a.gen != gen {
				return
			}
			q.saving, a.git.busy = false, false
			if err != nil {
				q.err = gitErrLine(err)
			} else {
				a.git.repositoryView.tag = nil
				a.note(locales.GitTagCreated(name), false)
			}
			a.refreshGit()
		})
	}()
}

func (a *App) cherryPick(cm git.Commit) {
	if a.git.busy || a.git.root == "" {
		return
	}
	a.git.busy = true
	ctx, root, gen := a.wsCtx, a.git.root, a.gen
	go func() {
		err := git.CherryPick(ctx, root, cm.Hash)
		head, paused := "", false
		if err == nil {
			head = git.Resolve(ctx, root, "HEAD")
		} else {
			paused = git.Resolve(ctx, root, "CHERRY_PICK_HEAD") != ""
		}
		a.post.Post(func() {
			if a.gen != gen {
				return
			}
			a.git.busy = false
			if err != nil {
				msg := gitErrLine(err)
				if paused {
					msg += " " + locales.GitCherryPickPaused()
				}
				a.note(locales.MsgGit()+msg, true)
			} else {
				a.git.diffSeq++
				a.note(locales.GitCherryPicked(cm.Short), false)
				a.revealLogCommit(head)
			}
			if !a.watching {
				a.refreshAll()
			} else {
				a.refreshGit()
			}
		})
	}()
}
