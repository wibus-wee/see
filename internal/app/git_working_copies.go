package app

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"see/internal/git"
	"see/internal/workspace"
	"see/locales"
)

type gitWorkingCopy struct {
	changes         []git.Change
	counts          [2]map[string]git.FileStat
	selected        map[string]bool
	selectionAnchor string
	message         string
	err             string
	loading         bool
	busy            bool
	merging         bool
	closed          bool
	refreshSeq      uint64
	diffSeq         uint64
}

func (a *App) showsRepositoryChanges() bool {
	return a.cfg.GitAutoDiscover && a.git.repositoriesLoaded && len(a.git.repositories) > 1 && a.git.branchDiffMode == uncommittedMode
}

func (a *App) gitChangeTabKey(root string, change git.Change, staged bool) string {
	if change.Conflicted() {
		return a.gitConflictTabKey(root, change.Path)
	}
	if a.cfg.GitAutoDiscover && a.git.workingCopies[root] != nil && root != a.git.root {
		return diffTab(root+"\x00"+change.Path, staged)
	}
	return diffTab(change.Path, staged)
}

func (a *App) gitWorkingCopiesNavigator(c *ui.Context) {
	changes := func() {
		count := 0
		for _, workingCopy := range a.git.workingCopies {
			count += len(workingCopy.changes)
		}
		ui.Column(c).Grow(1).FillWidth().Children(func() {
			a.gitSectionHeader(c, locales.MsgChanges(), count, &a.git.changesClosed, func() {
				if a.git.changesClosed {
					return
				}
				if a.git.root != "" {
					a.branchDiffModePicker(c)
					if a.git.syncing != "" {
						ui.Spinner(c).FontSize(10).Tooltip(a.git.syncing)
					}
					workingCopy := a.git.workingCopies[a.git.root]
					if workingCopy == nil || !workingCopy.busy {
						a.syncButtons(c)
					}
				}
				tip := locales.MsgRefreshStatus()
				if a.hasRemote() {
					tip = locales.MsgFetchAndRefreshStatus()
				}
				busy := a.git.busy
				if workingCopy := a.git.workingCopies[a.git.root]; workingCopy != nil {
					busy = busy || workingCopy.busy
				}
				if glyphButton(c, icRefresh, locales.MsgRefresh(), tip, false, busy).Clicked() {
					if a.git.root != "" && a.hasRemote() {
						a.fetchAndRefresh()
					} else if a.git.root != "" {
						a.refreshGit()
					}
					a.refreshGitWorkingCopies()
				}
			})
			if a.git.changesClosed {
				return
			}
			ui.Scroll(c.Key("git-changes-tree")).Grow(1).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Gap(gitSidebarRepositoryGap).Children(func() {
					for _, root := range a.git.repositories {
						workingCopy := a.git.workingCopies[root]
						if workingCopy != nil {
							a.gitWorkingCopyGroup(c, root, workingCopy)
						}
					}
				})
			})
		})
	}
	history := func() {
		a.gitHistoryHeader(c)
		if !a.git.historyClosed {
			if a.git.root == "" {
				ui.Text(c, locales.GitSelectRepositoryToSeeChanges()).FontSize(gitSidebarRowLabelSize).
					TextColor(colors(c).Muted).Padding(8, sidebarInset)
			} else {
				a.historyNavigator(c)
			}
		}
	}
	if !a.git.historyClosed {
		if a.git.split <= 0 {
			a.git.split = 40
		}
		ui.SplitVerticalPercent(c, &a.git.split,
			func() { ui.Column(c).Fill().Children(changes) },
			func() { ui.Column(c).Fill().Children(history) }).Grow(1).FillWidth()
	} else {
		ui.Column(c).Grow(1).FillWidth().Children(func() {
			changes()
			history()
		})
	}
}

func (a *App) gitWorkingCopyGroup(c *ui.Context, root string, workingCopy *gitWorkingCopy) {
	staged, unstaged := 0, 0
	for _, change := range workingCopy.changes {
		if gitChangeInSection(change, true) {
			staged++
		}
		if gitChangeInSection(change, false) {
			unstaged++
		}
	}
	label := filepath.Base(root)
	if rel, err := filepath.Rel(a.realRoot, root); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		label = filepath.ToSlash(rel)
	}
	ui.Column(c.Key("changes-repository:" + root)).FillWidth().Children(func() {
		repoDisclosure(c.Key("repository-header"), 0, label, icGit, workingCopy.closed, func() {
			workingCopy.closed = !workingCopy.closed
		}, func() {
			if workingCopy.closed {
				return
			}
			busy := workingCopy.busy || root == a.git.root && a.git.busy
			if staged > 0 && glyphButton(c, icMinus, locales.MsgUnstageAllChanges(), locales.MsgUnstageAllChanges(), false, busy).Clicked() {
				a.stageAllGitWorkingCopyChanges(root, true)
			}
			if unstaged > 0 && glyphButton(c, icPlus, locales.MsgStageAllChanges(), locales.MsgStageAllChanges(), false, busy).Clicked() {
				a.stageAllGitWorkingCopyChanges(root, false)
			}
			if root == a.git.root && len(workingCopy.changes) > 0 && glyphButton(c, icStash, locales.MsgStash(), locales.MsgStash(), false, busy || a.git.stashing).Clicked() {
				a.beginStash()
			}
		})
		if workingCopy.closed {
			return
		}
		conflicts := 0
		for _, change := range workingCopy.changes {
			if change.Conflicted() {
				conflicts++
			}
		}
		a.gitWorkingCopyCommitBar(c, root, workingCopy, staged, conflicts > 0)
		if workingCopy.err != "" {
			ui.Text(c, workingCopy.err).FontSize(11).TextColor(colors(c).Muted).Padding(4, sidebarInset)
		}
		if root == a.git.root && a.git.stashing {
			a.stashBar(c)
		}
		if workingCopy.loading && len(workingCopy.changes) == 0 {
			ui.Row(c).Height(gitSidebarRowHeight).Padding(0, sidebarInset+gitSidebarTreeIndent).Gap(5).Children(func() {
				ui.Spinner(c).FontSize(10)
				ui.Text(c, locales.GitScanningRepositories()).FontSize(gitSidebarRowLabelSize).TextColor(colors(c).Muted)
			})
		}
		if len(workingCopy.changes) == 0 && !workingCopy.loading && workingCopy.err == "" {
			ui.Text(c, locales.MsgNoChanges()).FontSize(gitSidebarRowLabelSize).TextColor(colors(c).Muted).
				Padding(gitSidebarEmptyInset, sidebarInset+gitSidebarTreeIndent).SingleLine().Ellipsis("…").MinWidth(0)
		}
		for _, change := range workingCopy.changes {
			if change.Conflicted() {
				a.gitConflictRowIn(c, root, workingCopy, change)
			}
		}
		if staged > 0 {
			a.gitSection(c, locales.MsgStagedChanges(), staged)
			a.gitWorkingCopyRows(c, root, workingCopy, true)
		}
		if unstaged > 0 {
			a.gitSection(c, locales.MsgChanges(), unstaged)
			a.gitWorkingCopyRows(c, root, workingCopy, false)
		}
		if root == a.git.root {
			a.stashRows(c)
		}
	})
}

func (a *App) gitWorkingCopyRows(c *ui.Context, root string, workingCopy *gitWorkingCopy, staged bool) {
	for _, change := range workingCopy.changes {
		if gitChangeInSection(change, staged) {
			a.gitRowIn(c, root, workingCopy, change, staged)
		}
	}
}

func (a *App) gitWorkingCopyCommitBar(c *ui.Context, root string, workingCopy *gitWorkingCopy, staged int, conflicted bool) {
	if conflicted {
		return
	}
	busy := workingCopy.busy || root == a.git.root && a.git.busy
	can := !busy && (staged > 0 || workingCopy.merging) && strings.TrimSpace(workingCopy.message) != ""
	commit := func() {
		if can {
			a.runGitWorkingCopyAction(root, "commit", "-m", workingCopy.message)
		}
	}
	placeholder := locales.MsgCommitMessage()
	if staged == 0 && !workingCopy.merging {
		placeholder = locales.MsgStageChangesToCommit()
	}
	tip := locales.Catalog.Format(i18n.Locale(), "msgCommitDStagedChanges", map[string]any{"count": staged})
	if workingCopy.merging {
		tip = gitConflictText("gitCommitMerge")
	}
	ui.Column(c).Shrink(0).FillWidth().Children(func() {
		ui.Row(c).FillWidth().Height(gitSidebarRowHeight).Padding(0, sidebarInset).AlignItems(ui.Center).Children(func() {
			ui.Row(c).FillWidth().Height(gitSidebarRowHeight).Radius(gitSidebarRowHeight/2).Background(c.Theme().Surface).
				Padding(0, 3, 0, gitSidebarTreeIndent+gitSidebarTextOffset).Gap(4).AlignItems(ui.Center).Children(func() {
				in := ui.TextInput(c.Key("git-commit-message:"+root), &workingCopy.message).Placeholder(placeholder).Label(locales.MsgCommitMessage()).
					FontSize(12).Grow(1).Background(ui.Transparent).BorderWidth(0).Padding(0)
				if in.Submitted() {
					commit()
				}
				b := ui.ButtonBase(c).Label(locales.MsgCommit()).Tooltip(tip).Size(22, 22).Radius(11).Center().Disabled(!can).KeepFocus()
				color := colors(c).Dim
				if can {
					b.Background(c.Theme().Accent)
					color = ui.RGB(0xff, 0xff, 0xff)
				}
				b.Children(func() { symbolIcon(c, icCommitArrow, 12, color) })
				b.OnClick(commit)
			})
		})
	})
}

func (a *App) reconcileGitWorkingCopies() {
	if !a.cfg.GitAutoDiscover || !a.git.repositoriesLoaded {
		a.git.workingCopies = nil
		return
	}
	workingCopies := make(map[string]*gitWorkingCopy, len(a.git.repositories))
	for _, root := range a.git.repositories {
		workingCopy := a.git.workingCopies[root]
		if workingCopy == nil {
			workingCopy = &gitWorkingCopy{}
		}
		workingCopies[root] = workingCopy
	}
	a.git.workingCopies = workingCopies
}

func (a *App) updateGitWorkingCopy(root string, state git.State, err error, counting bool) {
	if !a.cfg.GitAutoDiscover || !a.git.repositoriesLoaded || !slices.Contains(a.git.repositories, root) {
		return
	}
	workingCopy := a.git.workingCopies[root]
	if workingCopy == nil {
		return
	}
	workingCopy.refreshSeq++
	workingCopy.loading = false
	workingCopy.changes, workingCopy.merging = state.Changes, state.Merging
	workingCopy.err = ""
	if err != nil {
		workingCopy.err = err.Error()
	} else {
		a.refreshGitWorkingDiffs(root, state.Changes)
	}
	if counting || len(state.Changes) == 0 {
		workingCopy.counts = [2]map[string]git.FileStat{state.Staged, state.Unstaged}
	}
	workingCopy.pruneSelection()
}

func (a *App) refreshGitWorkingCopies() {
	if !a.showsRepositoryChanges() {
		return
	}
	for _, root := range a.git.repositories {
		if root != a.git.root {
			a.refreshGitWorkingCopy(root)
		}
	}
}

func (a *App) refreshGitWorkingCopy(root string) {
	workingCopy := a.git.workingCopies[root]
	if workingCopy == nil || workingCopy.loading || a.wsCtx == nil {
		return
	}
	workingCopy.loading = true
	workingCopy.refreshSeq++
	seq, gen, ctx := workingCopy.refreshSeq, a.gen, a.wsCtx
	counting := a.navigator == navGit && !a.sidebarOff
	go func() {
		state, err := git.Sync(ctx, root, counting)
		a.post.Post(func() {
			if a.gen != gen || !a.cfg.GitAutoDiscover || !a.git.repositoriesLoaded ||
				a.git.workingCopies[root] != workingCopy || workingCopy.refreshSeq != seq ||
				!slices.Contains(a.git.repositories, root) {
				return
			}
			workingCopy.loading = false
			workingCopy.changes, workingCopy.merging = state.Changes, state.Merging
			workingCopy.err = ""
			if err != nil {
				workingCopy.err = err.Error()
			} else {
				a.refreshGitWorkingDiffs(root, state.Changes)
			}
			if counting || len(state.Changes) == 0 {
				workingCopy.counts = [2]map[string]git.FileStat{state.Staged, state.Unstaged}
			}
			workingCopy.pruneSelection()
		})
	}()
}

func (a *App) runGitWorkingCopyAction(root string, args ...string) {
	workingCopy := a.git.workingCopies[root]
	if root == "" || workingCopy == nil || workingCopy.busy || len(args) == 0 || a.wsCtx == nil {
		return
	}
	active := root == a.git.root
	if active && a.git.busy {
		return
	}
	workingCopy.busy = true
	workingCopy.refreshSeq++
	workingCopy.loading = false
	workingCopy.diffSeq++
	if active {
		a.git.busy = true
	}
	ctx, gen := a.wsCtx, a.gen
	go func() {
		_, err := git.Command(ctx, root, args...)
		a.post.Post(func() {
			if a.gen != gen || a.git.workingCopies[root] != workingCopy {
				return
			}
			workingCopy.busy = false
			workingCopy.refreshSeq++
			workingCopy.loading = false
			workingCopy.diffSeq++
			if root == a.git.root {
				a.git.busy = false
				a.git.diffSeq++
			}
			if err != nil {
				a.note(locales.MsgGit()+err.Error(), true)
			} else if args[0] == "commit" {
				workingCopy.message = ""
				a.note(locales.MsgCommittedStagedChanges(), false)
			}
			if root == a.git.root {
				a.refreshGit()
			} else {
				a.refreshGitWorkingCopy(root)
			}
		})
	}()
}

func (a *App) stageGitWorkingCopyChanges(root string, staged bool, changes []git.Change) {
	var paths []string
	for _, change := range changes {
		if change.Conflicted() {
			continue
		}
		paths = append(paths, change.Path)
		if change.OldPath != "" {
			paths = append(paths, change.OldPath)
		}
	}
	if len(paths) == 0 {
		return
	}
	if staged {
		a.runGitWorkingCopyAction(root, append([]string{"reset", "-q", "--"}, paths...)...)
	} else {
		a.runGitWorkingCopyAction(root, append([]string{"add", "--"}, paths...)...)
	}
}

func (a *App) stageAllGitWorkingCopyChanges(root string, staged bool) {
	workingCopy := a.git.workingCopies[root]
	if workingCopy == nil {
		return
	}
	var changes []git.Change
	for _, change := range workingCopy.changes {
		if gitChangeInSection(change, staged) {
			changes = append(changes, change)
		}
	}
	a.stageGitWorkingCopyChanges(root, staged, changes)
}

func (a *App) discardGitWorkingCopyChange(root string, change git.Change) {
	a.discardGitWorkingCopyChanges(root, []git.Change{change})
}

func (a *App) discardGitWorkingCopyChanges(root string, changes []git.Change) {
	workingCopy := a.git.workingCopies[root]
	if root == "" || workingCopy == nil || workingCopy.busy || a.deleteConfirm != nil {
		return
	}
	paths := make([]string, len(changes))
	for i, change := range changes {
		path := workspace.DocPath(a.root, a.realRoot, filepath.Join(root, filepath.FromSlash(change.Path)))
		paths[i] = path
		if doc := a.docs[path]; doc != nil && doc.buf != nil && doc.buf.Modified() {
			a.note(locales.MsgSaveOrCloseTheFileSUnsavedEdits(), true)
			return
		}
	}
	message := locales.DiscardFilesQuestion(len(changes))
	if len(changes) == 1 {
		message = locales.DiscardFileQuestion(changes[0].Path)
		if changes[0].Index == '?' {
			message = locales.TrashUntrackedQuestion(changes[0].Path)
		}
	}
	q := &confirmation{message: message}
	q.actions = []action{
		{locales.DiscardChanges(), func() {
			if a.deleteConfirm != q || workingCopy.busy || a.git.workingCopies[root] != workingCopy {
				return
			}
			a.deleteConfirm = nil
			for i := range changes {
				if doc := a.docs[paths[i]]; doc != nil && doc.buf != nil && doc.buf.Modified() {
					a.note(locales.MsgSaveOrCloseTheFileSUnsavedEdits(), true)
					return
				}
			}
			active := root == a.git.root
			if active && a.git.busy {
				return
			}
			workingCopy.busy = true
			workingCopy.refreshSeq++
			workingCopy.loading = false
			workingCopy.diffSeq++
			if active {
				a.git.busy = true
			}
			ctx, gen, trash := a.wsCtx, a.gen, a.host.TrashItem
			go func() {
				var firstErr error
				for i, change := range changes {
					var err error
					if change.Index == '?' {
						err = trash(paths[i])
					} else {
						_, err = git.Command(ctx, root, "restore", "--worktree", "--", change.Path)
					}
					if err != nil && firstErr == nil {
						firstErr = err
					}
					path := paths[i]
					a.post.Post(func() {
						if a.gen != gen || a.git.workingCopies[root] != workingCopy || err != nil {
							return
						}
						if change.Index == '?' {
							a.removed(path)
						} else if doc := a.docs[path]; doc != nil {
							a.reloadDoc(doc)
						}
						a.refreshDir(filepath.Dir(path))
					})
				}
				a.post.Post(func() {
					if a.gen != gen || a.git.workingCopies[root] != workingCopy {
						return
					}
					workingCopy.busy = false
					workingCopy.refreshSeq++
					workingCopy.loading = false
					workingCopy.diffSeq++
					if root == a.git.root {
						a.git.busy = false
						a.git.diffSeq++
					}
					if firstErr != nil {
						a.note(locales.MsgCouldNotDiscardGitChanges()+firstErr.Error(), true)
					} else {
						a.quick.files = nil
						a.filter.forQuery = ""
					}
					if root == a.git.root {
						a.refreshGit()
					} else {
						a.refreshGitWorkingCopy(root)
					}
				})
			}()
		}},
		{locales.Cancel(), func() {
			if a.deleteConfirm == q {
				a.deleteConfirm = nil
			}
		}},
	}
	a.deleteConfirm = q
}

func (g *gitWorkingCopy) changeKeys() []string {
	var keys []string
	for _, staged := range []bool{true, false} {
		for _, change := range g.changes {
			if gitChangeInSection(change, staged) {
				keys = append(keys, gitChangeKey(staged, change.Path))
			}
		}
	}
	return keys
}

func (g *gitWorkingCopy) pruneSelection() {
	valid := make(map[string]bool)
	for _, key := range g.changeKeys() {
		valid[key] = true
	}
	for key := range g.selected {
		if !valid[key] {
			delete(g.selected, key)
		}
	}
	if !valid[g.selectionAnchor] {
		g.selectionAnchor = ""
	}
}

func (g *gitWorkingCopy) selectChange(key string, mods ui.Modifiers) {
	if mods&ui.Shift != 0 && g.selectionAnchor != "" {
		keys := g.changeKeys()
		start, end := -1, -1
		for i, item := range keys {
			if item == g.selectionAnchor {
				start = i
			}
			if item == key {
				end = i
			}
		}
		if start >= 0 && end >= 0 {
			if mods&(ui.Cmd|ui.Ctrl) == 0 {
				g.selected = make(map[string]bool)
			}
			if g.selected == nil {
				g.selected = make(map[string]bool)
			}
			if start > end {
				start, end = end, start
			}
			for _, item := range keys[start : end+1] {
				g.selected[item] = true
			}
			return
		}
	}
	if mods&(ui.Cmd|ui.Ctrl) == 0 {
		g.selected = make(map[string]bool)
	}
	if g.selected == nil {
		g.selected = make(map[string]bool)
	}
	if mods&(ui.Cmd|ui.Ctrl) != 0 && g.selected[key] {
		delete(g.selected, key)
	} else {
		g.selected[key] = true
	}
	g.selectionAnchor = key
}
