package app

import (
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"see/internal/git"
	"see/locales"
)

var icStash = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 13.5h4.5l1.5 2.5h4l1.5-2.5H20"/><path d="M6 5.5h12l2 8v5H4v-5z"/><path d="M12 7.5v4.5m-2-2 2 2 2-2"/></svg>`))

// beginStash shows the field that names a stash under the commit field,
// as Xcode's Stash Changes sheet, in the navigator.
func (a *App) beginStash() {
	if a.git.root == "" || len(a.git.changes) == 0 {
		return
	}
	a.sidebarOff, a.navigator, a.git.changesClosed = false, navGit, false
	a.git.branchDiffMode, a.git.stashing = uncommittedMode, true
}

func (a *App) endStash() {
	a.git.stashing, a.git.stashMessage = false, ""
}

// stashBar is the message field directly under Changes while stashing,
// with Stash and Cancel at its end.
func (a *App) stashBar(c *ui.Context) {
	can := !a.git.busy && len(a.git.changes) > 0
	stash := func() {
		if !can {
			return
		}
		ctx, root, message := a.wsCtx, a.git.root, a.git.stashMessage
		a.stashJob("Stashed changes.", func() error { return git.StashPush(ctx, root, message) }, func() {
			a.git.stashesClosed = false
			a.endStash()
		})
	}
	ui.Row(c).Shrink(0).FillWidth().Padding(gitSidebarEmptyInset, sidebarInset).Children(func() {
		ui.Row(c).FillWidth().Height(gitSidebarComposerHeight).Radius(4).Background(colors(c).Pill).Padding(0, 3, 0, gitSidebarTextOffset).Gap(2).AlignItems(ui.Center).Children(func() {
			in := ui.TextInput(c.Key("git-stash-message"), &a.git.stashMessage).Placeholder(locales.MsgStashMessageOptional()).Label(locales.MsgStashMessage()).
				FontSize(gitSidebarRowLabelSize).Grow(1).Background(ui.Transparent).BorderWidth(0).Padding(0).AutoFocus()
			switch {
			case in.Submitted():
				stash()
			case in.Shortcut(0, ui.KeyEscape):
				a.endStash()
			}
			if glyphButton(c, icClose, locales.MsgCancelStash(), locales.Cancel(), false, false).Clicked() {
				a.endStash()
			}
			tip := locales.Catalog.Format(i18n.Locale(), "msgStashDChanges", map[string]any{"count": len(a.git.changes)})
			b := ui.ButtonBase(c).Label(locales.MsgStash()).Tooltip(tip).Size(22, 22).Radius(11).Center().Disabled(!can).KeepFocus()
			color := colors(c).Dim
			if can {
				b.Background(c.Theme().Accent)
				color = ui.RGB(0xff, 0xff, 0xff)
			}
			b.Children(func() { symbolIcon(c, icStash, 12, color) })
			b.OnClick(stash)
		})
	})
}

// stashRows are the stashes under the changes, as Xcode's Stashed
// Changes: newest first, each opening its changes as a commit does.
func (a *App) stashRows(c *ui.Context) {
	if len(a.git.stashes) == 0 {
		return
	}
	a.gitCompactSectionHeader(c, locales.MsgStashedChanges(), len(a.git.stashes), &a.git.stashesClosed, func() {})
	if a.git.stashesClosed {
		return
	}
	now := time.Now()
	for _, s := range a.git.stashes {
		a.stashRow(c, s, now)
	}
}

func (a *App) stashRow(c *ui.Context, s git.Stash, now time.Time) {
	row := ui.Row(c.Key("stash:"+s.EntryKey())).FillWidth().Height(gitSidebarRowHeight).
		Padding(0, sidebarInset+gitSidebarTreeIndent).Radius(4).AlignItems(ui.Center)
	hovered := row.Hovered()
	if a.focus.active == commitTab(s.Hash) {
		row.Background(colors(c).SelectedRow)
	} else if hovered {
		row.Background(colors(c).Pill)
	}
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.MsgShowStashedChanges()).Chosen() {
			a.openStash(s)
		}
		m.Separator()
		if m.Item(locales.MsgApplyStashedChanges()).Disabled(a.git.busy).Chosen() {
			a.applyStash(s, false)
		}
		if m.Item(locales.MsgApplyAndDropStash()).Disabled(a.git.busy).Chosen() {
			a.applyStash(s, true)
		}
		m.Separator()
		if m.Item(locales.MsgCopyStashMessage()).Chosen() {
			c.Services().WriteClipboard(s.Message)
		}
		m.Separator()
		if m.Item(locales.MsgDropStash()).Disabled(a.git.busy).Chosen() {
			a.dropStash(s)
		}
	})
	row.Children(func() {
		detail := s.Ref()
		if s.Branch != "" {
			detail += " · " + s.Branch
		}
		detail += " · " + relativeTime(s.Date, now)
		b := ui.ButtonBase(c).Label(locales.MsgStash() + ": " + s.Message).Tooltip(s.Message + " · " + detail).Description(detail).
			Grow(1).Shrink(1).MinWidth(0).Padding(0).Radius(4)
		b.Children(func() {
			ui.Row(c).FillWidth().Gap(gitSidebarColumnGap).AlignItems(ui.Center).Children(func() {
				symbolIcon(c, icStash, gitSidebarIconColumn, colors(c).Muted)
				ui.Text(c, s.Message).FontSize(gitSidebarRowLabelSize).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
			})
		})
		b.OnClick(func() { a.openStash(s) })
		ui.Box(c).Width(gitSidebarStashActionWidth).Height(gitSidebarRowHeight).Shrink(0).Children(func() {
			if hovered {
				ui.Row(c).FillWidth().Height(gitSidebarRowHeight).Gap(2).AlignItems(ui.Center).Children(func() {
					if glyphButton(c, icStash, locales.Catalog.Format(i18n.Locale(), "msgApplyStashS", map[string]any{"message": s.Message}), locales.MsgApplyStashedChanges(), false, a.git.busy).Clicked() {
						a.applyStash(s, false)
					}
					if glyphButton(c, icTrash, locales.Catalog.Format(i18n.Locale(), "msgDropStashS", map[string]any{"message": s.Message}), locales.MsgDropStash(), false, a.git.busy).Clicked() {
						a.dropStash(s)
					}
				})
			}
		})
	})
}

// openStash shows a stash's changes in a commit tab: a stash is a commit
// whose first parent is the commit it was taken on.
func (a *App) openStash(s git.Stash) {
	gen, scopeSeq, ctx, root := a.gen, a.git.scopeSeq, a.wsCtx, a.git.root
	go func() {
		cm, err := git.ReadCommit(ctx, root, s.Hash)
		a.post.Post(func() {
			if a.gen != gen || a.git.scopeSeq != scopeSeq {
				return
			}
			if err != nil {
				a.note(locales.MsgGit()+" "+gitErrLine(err), true)
				return
			}
			cm.Subject = s.Message
			a.openCommit(cm)
		})
	}()
}

func (a *App) applyStash(s git.Stash, drop bool) {
	ctx, root := a.wsCtx, a.git.root
	done := locales.MsgAppliedStashedChanges()
	if drop {
		done = locales.MsgAppliedAndDroppedTheStash()
	}
	a.stashJob(done, func() error { return git.StashApply(ctx, root, s, drop) }, nil)
}

// dropStash asks before a stash is dropped, as git keeps no way back.
func (a *App) dropStash(s git.Stash) {
	if a.git.busy || a.deleteConfirm != nil {
		return
	}
	q := &confirmation{message: locales.Catalog.Format(i18n.Locale(), "msgDropTheStashSItsChangesCannotBeRecovered", map[string]any{"message": s.Message})}
	q.actions = []action{
		{locales.MsgDropStashAction(), func() {
			if a.deleteConfirm != q {
				return
			}
			a.deleteConfirm = nil
			ctx, root := a.wsCtx, a.git.root
			a.stashJob(locales.MsgDroppedTheStash(), func() error { return git.StashDrop(ctx, root, s) }, nil)
		}},
		{locales.Cancel(), func() {
			if a.deleteConfirm == q {
				a.deleteConfirm = nil
			}
		}},
	}
	a.deleteConfirm = q
}

// stashJob runs a stash command as a branch switch is run, once no file
// has edits it would write over; ok runs when it succeeds.
func (a *App) stashJob(done string, job func() error, ok func()) {
	if a.git.busy || a.git.root == "" {
		return
	}
	if len(a.dirtyDocs()) > 0 {
		a.note(locales.MsgSaveOrRevertUnsavedEditsFirst(), true)
		return
	}
	a.runBranchJob(done, func() error {
		err := job()
		if err == nil && ok != nil {
			a.post.Post(ok)
		}
		return err
	})
}
