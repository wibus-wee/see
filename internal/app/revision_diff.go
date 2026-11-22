package app

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/git"
)

const revisionPrefix = diffTabPrefix + "revision:"

type revisionView struct {
	path, root, current string
	commits             []git.Commit
	selected            int
	seq                 uint64
	loading, pickerOpen bool
	err                 error
}

func (a *App) showActiveRevision() {
	if a.focus != nil {
		a.showRevision(a.focus, a.docs[a.activePath()], "")
	}
}

func (a *App) showRevision(g *group, d *doc, hash string) {
	if g == nil || d == nil || d.buf == nil || d.untitled {
		return
	}
	key := revisionPrefix + d.path
	if g.diffs == nil {
		g.diffs = map[string]*gitDiffTab{}
	}
	v := g.diffs[key]
	if v == nil {
		v = &gitDiffTab{name: d.name, revision: &revisionView{path: d.path, root: a.git.root, selected: -1}}
		g.diffs[key] = v
		g.addTab(key, len(g.order))
	}
	v.revision.current = d.buf.Snapshot().String()
	a.activateIn(g, key)
	a.loadRevision(v, hash)
}

func (a *App) hasRevision(v *gitDiffTab) bool {
	for _, g := range a.groups() {
		if g.diffs[revisionPrefix+v.revision.path] == v {
			return true
		}
	}
	return false
}

func (a *App) loadRevision(v *gitDiffTab, hash string) {
	r := v.revision
	r.seq++
	seq := r.seq
	r.loading, r.err = true, nil
	if r.root == "" {
		r.loading = false
		return
	}
	root, path, post := r.root, r.path, a.post.Post
	go func() {
		rel, err := filepath.Rel(root, path)
		var commits []git.Commit
		if err == nil {
			commits, err = git.LogFile(context.Background(), root, false, 0, 50, filepath.ToSlash(rel))
		}
		post(func() {
			if r.seq != seq || !a.hasRevision(v) {
				return
			}
			r.loading, r.err, r.commits = false, err, commits
			if err != nil || len(commits) == 0 {
				v.state = nil
				return
			}
			index := 0
			for i, cm := range commits {
				if cm.Hash == hash {
					index = i
					break
				}
			}
			a.selectRevision(v, index)
		})
	}()
}

func (a *App) selectRevision(v *gitDiffTab, index int) {
	r := v.revision
	if index < 0 || index >= len(r.commits) {
		return
	}
	r.selected = index
	r.seq++
	seq := r.seq
	r.loading, r.err = true, nil
	root, path, hash, current, post := r.root, r.path, r.commits[index].Hash, r.current, a.post.Post
	go func() {
		rel, err := filepath.Rel(root, path)
		var old []byte
		if err == nil {
			old, err = git.FileAtCommit(context.Background(), root, rel, hash)
		}
		post(func() {
			if r.seq != seq || !a.hasRevision(v) {
				return
			}
			r.loading, r.err = false, err
			if err != nil {
				v.state = nil
				return
			}
			before, after := editor.NewBuffer(string(old)), editor.NewBuffer(current)
			lang := languageFor(filepath.Base(path), string(old))
			before.SetLanguage(lang)
			after.SetLanguage(lang)
			v.state = &editor.DiffState{Old: before, New: after, Theme: a.edTheme}
			a.applyDiffPrefs(v.state)
		})
	}()
}

func (a *App) refreshRevision(v *gitDiffTab) {
	r := v.revision
	if d := a.docs[r.path]; d != nil && d.buf != nil {
		r.current = d.buf.Snapshot().String()
	}
	hash := ""
	if r.selected >= 0 && r.selected < len(r.commits) {
		hash = r.commits[r.selected].Hash
	}
	a.loadRevision(v, hash)
}

func revisionLabel(cm git.Commit) string {
	return cm.Short + " · " + cm.Subject + " · " + cm.Date.Format("2006-01-02")
}

func (a *App) revisionContent(c *ui.Context, g *group, v *gitDiffTab) {
	r, p := v.revision, colors(c)
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Height(32).Padding(0, 8).Gap(6).AlignItems(ui.Center).Children(func() {
			symbolIcon(c, icClock, 14, c.Theme().Accent)
			ui.Text(c, localHistoryText("revisionTitle")).FontSize(12).Bold()
			ui.Text(c, "· "+filepath.Base(r.path)).FontSize(12).TextColor(p.Muted).SingleLine().Ellipsis("…").Shrink(1)
			ui.Box(c).Grow(1)
			if r.selected >= 0 && r.selected < len(r.commits) {
				cm := r.commits[r.selected]
				ui.Text(c, cm.Short+" · "+cm.Author+" · "+cm.Date.Format("2006-01-02")).FontSize(11).TextColor(p.Muted).SingleLine().Ellipsis("…").Shrink(1)
			}
		})
		switch {
		case r.loading:
			ui.Row(c).Grow(1).FillWidth().Center().Children(func() { ui.Spinner(c) })
		case r.err != nil:
			ui.Column(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
				ui.Text(c, localHistoryText("localHistoryLoadError")+r.err.Error()).TextColor(p.Err)
				if ui.Button(c, localHistoryText("localHistoryRetry")).Clicked() {
					a.refreshRevision(v)
				}
			})
		case r.root == "":
			ui.Row(c).Grow(1).FillWidth().Center().Children(func() { ui.Text(c, inspectorText("inspectorNotRepo")) })
		case len(r.commits) == 0:
			ui.Row(c).Grow(1).FillWidth().Center().Children(func() { ui.Text(c, inspectorText("inspectorNoHistory")) })
		default:
			if v.state != nil {
				editor.DiffView(c, v.state).Grow(1).FillWidth()
			} else {
				ui.Box(c).Grow(1)
			}
		}
		ui.Row(c).FillWidth().Height(30).Padding(0, 8).Gap(2).AlignItems(ui.Center).Children(func() {
			for i := len(r.commits) - 1; i >= 0; i-- {
				cm := r.commits[i]
				b := ui.ButtonBase(c.Key(cm.Hash)).Label(revisionLabel(cm)).Tooltip(revisionLabel(cm)).Grow(1).Basis(0).MinWidth(3).Height(22)
				b.Children(func() {
					color := p.Dim
					if i == r.selected {
						color = c.Theme().Accent
					}
					ui.Box(c).Width(3).Height(14).Background(color)
				})
				if b.Clicked() {
					a.selectRevision(v, i)
				}
			}
			ui.Text(c, "● "+localHistoryText("localHistoryWorkingTree")).FontSize(10).TextColor(p.Muted).SingleLine()
		})
		ui.Row(c).FillWidth().Height(30).Padding(0, 8).Gap(6).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(p.Separator).Children(func() {
			label := localHistoryText("revisionChoose")
			if r.selected >= 0 && r.selected < len(r.commits) {
				label = r.commits[r.selected].Short
			}
			picker := ui.ButtonBase(c).Label(label).Height(22).Padding(0, 5)
			picker.Children(func() { ui.Text(c, label).FontSize(11) })
			picker.PopoverMenuButton(func(m *ui.Menu) {
				for i, cm := range r.commits {
					if m.Item(revisionLabel(cm)).Chosen() {
						a.selectRevision(v, i)
					}
				}
			}, ui.PopoverMenuButtonOptions{Open: &r.pickerOpen})
			target := localHistoryText("localHistoryWorkingTree")
			if d := a.docs[r.path]; d != nil && d.buf != nil && d.buf.Modified() {
				target = localHistoryText("localHistoryCurrentUnsaved")
			}
			ui.Text(c, "→ "+target).FontSize(11).SingleLine()
			if ui.Button(c, "‹").Tooltip(localHistoryText("revisionOlder")).Disabled(r.selected+1 >= len(r.commits)).Clicked() {
				a.selectRevision(v, r.selected+1)
			}
			if ui.Button(c, "›").Tooltip(localHistoryText("revisionNewer")).Disabled(r.selected <= 0).Clicked() {
				a.selectRevision(v, r.selected-1)
			}
			ui.Box(c).Grow(1)
			if v.state != nil {
				add, del := v.state.Stats()
				ui.Text(c, fmt.Sprintf("+%d −%d", add, del)).FontSize(11).TextColor(p.Muted)
			}
			if ui.Button(c, localHistoryText("localHistoryRefresh")).Disabled(r.loading).Clicked() {
				a.refreshRevision(v)
			}
		})
	})
}
