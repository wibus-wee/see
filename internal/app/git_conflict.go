package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
	"see/internal/workspace"
	"see/locales"
)

type gitConflictView struct {
	file                    *git.ConflictFile
	choices                 []git.ConflictChoice
	at, resolved            int
	current, incoming       *editor.State
	preview                 *editor.State
	err                     error
	loading, saving, staged bool
}

func gitConflictText(key string) string {
	return locales.Catalog.Format(i18n.Locale(), key, nil)
}

func conflictTab(path string) string { return diffTabPrefix + "conflict:" + path }

func (a *App) gitConflictTabKey(root, path string) string {
	if a.cfg.GitAutoDiscover && a.git.workingCopies[root] != nil && root != a.git.root {
		return conflictTab(root + "\x00" + path)
	}
	return conflictTab(path)
}

func (a *App) gitConflictRow(c *ui.Context, ch git.Change) {
	a.gitConflictRowIn(c, a.git.root, nil, ch)
}

func (a *App) gitConflictRowIn(c *ui.Context, root string, workingCopy *gitWorkingCopy, ch git.Change) {
	label := fmt.Sprintf(gitConflictText("gitResolveFile"), ch.Path)
	tabKey := a.gitConflictTabKey(root, ch.Path)
	row := ui.ButtonBase(c.Key(tabKey)).Label(label).Tooltip(label).
		FillWidth().Height(gitSidebarRowHeight).Padding(0, sidebarInset+gitSidebarTreeIndent).Radius(4)
	if a.focus.active == tabKey {
		row.Background(colors(c).SelectedRow)
	} else if row.Hovered() {
		row.Background(colors(c).Pill)
	}
	row.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			fileIcon(c, filepath.Base(ch.Path), 15)
			ui.Text(c, filepath.Base(ch.Path)).FontSize(gitSidebarRowLabelSize).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
			if dir := filepath.Dir(ch.Path); dir != "." {
				ui.Text(c, filepath.ToSlash(dir)).FontSize(10).TextColor(colors(c).Muted).
					SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
			}
			ui.Box(c).Grow(1)
			ui.Text(c, "!").FontSize(11).FontWeight(600).TextColor(a.gitStatusColor('!')).Padding(0, 3).Shrink(0)
		})
	})
	row.OnClick(func() {
		if workingCopy != nil {
			workingCopy.selected = nil
			workingCopy.selectionAnchor = ""
			a.showGitWorkingCopyDiff(root, ch, false, a.focus, sideNone, len(a.focus.order), workingCopy)
		} else {
			a.git.selected = nil
			a.git.selectionAnchor = ""
			a.showGitDiff(ch, false)
		}
	})
}

func (a *App) openGitConflictInRepository(root string, ch git.Change, g *group, placement side, at int) {
	if placement != sideNone {
		g = a.splitBeside(g, placement)
		at = 0
	}
	key := a.gitConflictTabKey(root, ch.Path)
	if v := g.diffs[key]; v != nil && v.conflict != nil && !v.conflict.staged {
		a.activateIn(g, key)
		return
	}
	v := &gitDiffTab{name: ch.Path, root: root, conflict: &gitConflictView{}}
	if g.diffs == nil {
		g.diffs = map[string]*gitDiffTab{}
	}
	g.diffs[key] = v
	g.addTab(key, at)
	a.activateIn(g, key)
	a.loadGitConflict(v)
}

func (a *App) loadGitConflict(v *gitDiffTab) {
	m := v.conflict
	root := v.root
	if root == "" {
		root = a.git.root
	}
	if m.loading || m.saving || root == "" {
		return
	}
	*m = gitConflictView{loading: true}
	workingCopy := a.git.workingCopies[root]
	ctx, gen, scopeSeq, name := a.wsCtx, a.gen, a.git.scopeSeq, v.name
	go func() {
		file, err := git.LoadConflict(ctx, root, name)
		a.post.Post(func() {
			if a.gen != gen || root == a.git.root && a.git.scopeSeq != scopeSeq ||
				root != a.git.root && (workingCopy == nil || a.git.workingCopies[root] != workingCopy) {
				return
			}
			m.loading, m.file, m.err = false, file, err
			if file != nil {
				m.choices = make([]git.ConflictChoice, len(file.Hunks))
				if len(file.Hunks) > 0 {
					a.selectGitConflict(v, 0)
				}
			}
		})
	}()
}

func (a *App) selectGitConflict(v *gitDiffTab, i int) {
	m := v.conflict
	if m.saving || i < 0 || i >= len(m.choices) {
		return
	}
	m.at = i
	h := m.file.Hunks[i]
	m.current = a.gitConflictEditor(v.name, gitConflictText("gitConflictCurrent"), h.Current)
	m.incoming = a.gitConflictEditor(v.name, gitConflictText("gitConflictIncoming"), h.Incoming)
	a.updateGitConflictPreview(v)
}

func (a *App) chooseGitConflict(v *gitDiffTab, choice git.ConflictChoice) {
	m := v.conflict
	if m.saving {
		return
	}
	if m.choices[m.at] == 0 {
		m.resolved++
	}
	m.choices[m.at] = choice
	a.updateGitConflictPreview(v)
}

func (a *App) updateGitConflictPreview(v *gitDiffTab) {
	m := v.conflict
	m.preview = a.gitConflictEditor(v.name, gitConflictText("gitConflictPreview"), m.file.Hunks[m.at].Result(m.choices[m.at]))
}

func (a *App) gitConflictEditor(path, label, text string) *editor.State {
	if text == "" {
		return nil
	}
	buf := editor.NewBuffer(strings.TrimSuffix(strings.TrimSuffix(text, "\n"), "\r"))
	if len(text) < workspace.PlainLimit {
		buf.SetLanguage(languageFor(path, buf.Line(0)))
	}
	return &editor.State{Buffer: buf, ReadOnly: true, Label: label, NoFolding: true}
}

func (a *App) saveGitConflict(v *gitDiffTab) {
	m := v.conflict
	root := v.root
	if root == "" {
		root = a.git.root
	}
	workingCopy := a.git.workingCopies[root]
	groupScoped := workingCopy != nil
	busy := a.git.busy
	if groupScoped {
		busy = workingCopy.busy || root == a.git.root && a.git.busy
	}
	if busy || m.file == nil || m.err != nil || len(m.choices) == 0 || m.resolved != len(m.choices) || root == "" {
		return
	}
	ctx, gen, scopeSeq := a.wsCtx, a.gen, a.git.scopeSeq
	path := workspace.DocPath(a.root, a.realRoot, filepath.Join(root, filepath.FromSlash(v.name)))
	if d := a.docs[path]; d != nil && (d.saving || d.buf != nil && d.buf.Modified()) {
		a.note(locales.MsgSaveOrCloseTheFileSUnsavedEdits(), true)
		return
	}
	if groupScoped {
		workingCopy.busy = true
		if root == a.git.root {
			a.git.busy, a.git.resolvingPath = true, path
		}
	} else {
		a.git.busy, a.git.resolvingPath = true, path
	}
	if workingCopy != nil {
		workingCopy.refreshSeq++
		workingCopy.loading = false
		workingCopy.diffSeq++
	}
	m.saving = true
	file, choices := m.file, slices.Clone(m.choices)
	go func() {
		err := file.Resolve(ctx, choices)
		a.post.Post(func() {
			if a.gen != gen || groupScoped && a.git.workingCopies[root] != workingCopy ||
				!groupScoped && (a.git.scopeSeq != scopeSeq || a.git.root != root) {
				return
			}
			if groupScoped {
				workingCopy.busy = false
				if root == a.git.root {
					a.git.busy, a.git.resolvingPath = false, ""
				}
			} else {
				a.git.busy, a.git.resolvingPath = false, ""
			}
			m.saving = false
			m.staged, m.err = err == nil, err
			if err == nil {
				m.file, m.choices, m.current, m.incoming, m.preview = nil, nil, nil, nil, nil
				if groupScoped {
					workingCopy.diffSeq++
				} else {
					a.git.diffSeq++
				}
				a.note(gitConflictText("gitConflictStaged"), false)
			}
			if d := a.docs[path]; d != nil {
				a.reloadDoc(d)
			}
			if root == a.git.root {
				a.refreshGit()
			} else {
				workingCopy.refreshSeq++
				workingCopy.loading = false
				a.refreshGitWorkingCopy(root)
			}
		})
	}()
}

func gitConflictError(err error) string {
	switch {
	case errors.Is(err, git.ErrConflictResolved):
		return gitConflictText("gitConflictAlreadyResolved")
	case errors.Is(err, git.ErrConflictUnsupported):
		return gitConflictText("gitConflictUnsupported")
	case errors.Is(err, git.ErrConflictMarkers):
		return gitConflictText("gitConflictMalformed")
	case errors.Is(err, git.ErrConflictStale):
		return gitConflictText("gitConflictStale")
	default:
		return describeErr(err)
	}
}

func (a *App) gitConflictContent(c *ui.Context, g *group, v *gitDiffTab) {
	m := v.conflict
	panel := ui.Column(c.Key(v)).Grow(1).FillWidth().MinHeight(0).Background(colors(c).Editor)
	panel.Children(func() {
		openFile := func() {
			a.focus = g
			a.openGitFileIn(a.gitDiffRoot(v), git.Change{Path: v.name})
		}
		switch {
		case m.loading:
			ui.Row(c).Grow(1).FillWidth().Center().Gap(6).Children(func() {
				ui.Spinner(c).FontSize(12)
				ui.Text(c, gitConflictText("gitConflictReading")).FontSize(12).TextColor(colors(c).Muted)
			})
		case m.staged:
			a.message(c, gitConflictText("gitConflictStaged"), gitConflictText("gitConflictStagedDetail"), gitConflictText("gitConflictOpenFile"), openFile)
		case m.err != nil:
			ui.Column(c).Fill().Center().Padding(12).Gap(8).Children(func() {
				ui.Text(c, gitConflictText("gitConflictUnavailable")).FontSize(15).Bold()
				ui.Text(c, gitConflictError(m.err)).FontSize(12).TextColor(colors(c).Muted).MaxWidth(440).TextAlign(ui.Center)
				ui.Row(c).Gap(6).Children(func() {
					if m.file != nil || errors.Is(m.err, git.ErrConflictMarkers) {
						ui.Button(c, gitConflictText("gitConflictOpenFile")).OnClick(openFile)
					}
					ui.Button(c, locales.Reload()).OnClick(func() { a.loadGitConflict(v) })
				})
			})
		default:
			a.gitConflictToolbar(c, v, openFile)
			if len(m.choices) == 0 {
				a.message(c, gitConflictText("gitConflictNoMarkers"), gitConflictText("gitConflictNoMarkersDetail"), "", nil)
				return
			}
			h := m.file.Hunks[m.at]
			ui.Row(c).Grow(1).Basis(0).FillWidth().MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				a.gitConflictPane(c, gitConflictText("gitConflictCurrent"), h.CurrentLabel, m.current, gitConflictText("gitConflictNoLines"), a.gitStatusColor('!'))
				ui.Box(c).Width(1).FillHeight().Shrink(0).Background(colors(c).Separator)
				a.gitConflictPane(c, gitConflictText("gitConflictIncoming"), h.IncomingLabel, m.incoming, gitConflictText("gitConflictNoLines"), c.Theme().Accent)
			})
			a.gitConflictChoices(c, v)
			empty := gitConflictText("gitConflictNoLines")
			if m.choices[m.at] == 0 {
				empty = gitConflictText("gitConflictChoose")
			}
			a.gitConflictPane(c, gitConflictText("gitConflictPreview"), "", m.preview, empty, a.gitStatusColor('A'))
		}
	})
	if a.focusGroupTarget == nil && panel.FocusWithin() {
		a.focus = g
	}
}

func (a *App) gitConflictToolbar(c *ui.Context, v *gitDiffTab, openFile func()) {
	m := v.conflict
	root := a.gitDiffRoot(v)
	busy := a.git.busy
	if workingCopy := a.git.workingCopies[root]; workingCopy != nil {
		busy = workingCopy.busy || root == a.git.root && a.git.busy
	}
	ui.Row(c).FillWidth().Height(34).Shrink(0).Padding(0, 8).Gap(5).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Separator).Children(func() {
		if len(m.choices) > 0 {
			prev, next := gitConflictText("gitConflictPrevious"), gitConflictText("gitConflictNext")
			glyphButton(c, icChangeUp, prev, prev, false, m.saving || m.at == 0).
				OnClick(func() { a.selectGitConflict(v, m.at-1) })
			glyphButton(c, icChangeDown, next, next, false, m.saving || m.at+1 == len(m.choices)).
				OnClick(func() { a.selectGitConflict(v, m.at+1) })
			ui.Text(c, fmt.Sprintf(gitConflictText("gitConflictPosition"), m.at+1, len(m.choices))).FontSize(12).Bold().SingleLine()
			ui.Text(c, fmt.Sprintf(gitConflictText("gitConflictLine"), m.file.Hunks[m.at].Line)).FontSize(11).TextColor(colors(c).Muted).
				SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		}
		ui.Box(c).Grow(1)
		if len(m.choices) > 0 {
			ui.Text(c, fmt.Sprintf(gitConflictText("gitConflictProgress"), m.resolved, len(m.choices))).FontSize(11).TextColor(colors(c).Muted).
				SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		}
		open := gitConflictText("gitConflictOpenFile")
		glyphButton(c, icGitOpen, open, open, false, m.saving).OnClick(openFile)
		reload := gitConflictText("gitConflictReload")
		glyphButton(c, icRefresh, reload, reload, false, m.saving).OnClick(func() { a.loadGitConflict(v) })
		save := gitConflictText("gitConflictSave")
		if m.saving {
			save = gitConflictText("gitConflictSaving")
		}
		ui.PrimaryButton(c.Key("save-resolution"), save).FontSize(11).Height(24).Padding(0, 7).
			Disabled(busy || len(m.choices) == 0 || m.resolved != len(m.choices)).OnClick(func() { a.saveGitConflict(v) })
	})
}

func (a *App) gitConflictChoices(c *ui.Context, v *gitDiffTab) {
	m := v.conflict
	ui.Row(c).FillWidth().Height(37).Shrink(0).Center().Padding(0, 6).Gap(5).
		Background(colors(c).Pill).BorderWidth(1, 0, 1, 0).BorderColor(colors(c).Separator).Children(func() {
		for _, option := range []struct {
			choice git.ConflictChoice
			label  string
		}{
			{git.KeepCurrent, gitConflictText("gitConflictKeepCurrent")},
			{git.KeepBoth, gitConflictText("gitConflictKeepBoth")},
			{git.KeepIncoming, gitConflictText("gitConflictKeepIncoming")},
		} {
			var b ui.Element
			if m.choices[m.at] == option.choice {
				b = ui.PrimaryButton(c.Key(option.choice), option.label)
			} else {
				b = ui.Button(c.Key(option.choice), option.label)
			}
			b.FontSize(12).Height(25).Padding(0, 8).Disabled(m.saving).OnClick(func() {
				a.chooseGitConflict(v, option.choice)
			})
			if option.choice == git.KeepBoth {
				b.Tooltip(gitConflictText("gitConflictBothOrder"))
			}
		}
	})
}

func (a *App) gitConflictPane(c *ui.Context, title, label string, st *editor.State, empty string, tint ui.Color) {
	ui.Column(c.Key(title)).Grow(1).Basis(0).MinWidth(0).MinHeight(0).Children(func() {
		ui.Row(c).FillWidth().Shrink(0).Height(26).Padding(0, 8).Gap(6).AlignItems(ui.Center).
			Background(colors(c).Pill).BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Separator).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Shrink(0).Background(tint)
			ui.Text(c, title).FontSize(11).Bold().SingleLine()
			if label != "" {
				ui.Text(c, label).FontSize(11).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1).Tooltip(label)
			}
		})
		if st != nil {
			st.Theme = a.edTheme
			a.applyEditorPrefs(st)
			st.LineNumbers = editor.LineNumbersOff
			editor.View(c, st).Grow(1).FillWidth().MinHeight(0).MinWidth(0)
		} else {
			ui.Text(c, empty).FontSize(12).TextColor(colors(c).Muted).Padding(8)
		}
	})
}
