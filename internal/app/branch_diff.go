package app

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
	"see/locales"
)

type branchDiffMode uint8

const (
	uncommittedMode branchDiffMode = iota
	compareBaseMode
)

const branchDiffFilesAtOnce = 20

type branchDiffFile struct {
	git.FileStat
	untracked bool
	binary    bool
	diff      *editor.DiffState
}

type branchDiffView struct {
	baseRef, baseName string
	result            *git.BranchDiffResult
	files             []branchDiffFile
	loading           bool
	err               string
	seq               uint64
	fingerprint       string
	patch             []byte
	shown             int
	collapsed         map[string]bool
	jumpPath          string
	scroll            ui.ScrollState
}

func branchDiffTab(baseRef string) string { return diffTabPrefix + "branch:" + baseRef }

func branchDiffText(key string) string {
	return locales.Catalog.Format(i18n.Locale(), key, nil)
}

func branchDiffMoreFiles(n int) string {
	return strings.Replace(branchDiffText("gitShowMoreFiles"), "%d", fmt.Sprint(n), 1)
}

func (a *App) branchDiffModePicker(c *ui.Context) {
	mode := &a.git.branchDiffMode
	selected := branchDiffText("gitUncommitted")
	if *mode == compareBaseMode {
		selected = branchDiffText("gitCompareAgainstBase")
	}
	selectMode := ui.SelectBase(c, mode)
	selectMode.Trigger.Label(selected).Padding(3, sidebarInset).Radius(6).Gap(6).AlignItems(ui.Center).KeepFocus()
	selectMode.Trigger.Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, selected).FontSize(12).Bold().SingleLine().Shrink(1)
			symbolIcon(c, icChevron, 9, colors(c).Muted)
		})
	})
	selectMode.Popup(func(panel ui.Element) {
		panel.Padding(4).Radius(8).Background(colors(c).Popover).Border(1, colors(c).PopoverEdge)
		for _, option := range []struct {
			value branchDiffMode
			label string
		}{{uncommittedMode, branchDiffText("gitUncommitted")}, {compareBaseMode, branchDiffText("gitCompareAgainstBase")}} {
			item := selectMode.Item(option.value).Padding(6, 8).Radius(5)
			if item.Highlighted() {
				item.Background(colors(c).Pill)
			}
			item.Children(func() { ui.Text(c, option.label).FontSize(12) })
		}
	})
	if selectMode.Trigger.Changed() && *mode == compareBaseMode && a.git.branchPanel == nil {
		a.openBaseBranchPicker(selectMode.Trigger.Bounds(), nil)
	}
}

func (a *App) branchDiffNavigator(c *ui.Context) {
	v := a.git.branchPanel
	if v == nil {
		ui.Row(c).Padding(8, sidebarInset).Children(func() {
			button := ui.Button(c, branchDiffText("gitChooseBaseBranch")).FontSize(12)
			if button.Clicked() {
				a.openBaseBranchPicker(button.Bounds(), nil)
			}
		})
		return
	}
	ui.Row(c).FillWidth().Height(30).Padding(0, sidebarInset).Gap(5).AlignItems(ui.Center).Children(func() {
		ui.Text(c, branchDiffText("gitSince")+" "+v.baseName).FontSize(12).Bold().SingleLine().Shrink(1)
		ui.Text(c, branchDiffText("gitReadOnly")).FontSize(10).TextColor(colors(c).Muted).SingleLine()
		ui.Box(c).Grow(1)
		if v.loading {
			ui.Spinner(c).FontSize(10)
		} else if v.err == "" && v.result != nil {
			added, deleted := branchDiffTotals(v.files)
			ui.Text(c, fmt.Sprintf("%d", len(v.files))).FontSize(11).TextColor(colors(c).Muted).SingleLine()
			if added > 0 {
				ui.Text(c, fmt.Sprintf("+%d", added)).FontSize(10).TextColor(a.gitStatusColor('A')).SingleLine()
			}
			if deleted > 0 {
				ui.Text(c, fmt.Sprintf("−%d", deleted)).FontSize(10).TextColor(a.gitStatusColor('D')).SingleLine()
			}
		}
	})
	ui.Row(c).FillWidth().Padding(2, sidebarInset, 6).Gap(6).Children(func() {
		if ui.Button(c, branchDiffText("gitViewDiff")).FontSize(11).Clicked() {
			a.openBranchDiff(a.focus, v.baseRef, v.baseName, "")
		}
		button := ui.Button(c, branchDiffText("gitChangeBase")).FontSize(11)
		if button.Clicked() {
			a.openBaseBranchPicker(button.Bounds(), nil)
		}
	})
	switch {
	case v.loading && v.result == nil:
		ui.Row(c).Padding(10, sidebarInset).Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, branchDiffText("gitReadingBranchDiff")).FontSize(12).TextColor(colors(c).Muted)
		})
	case v.err != "":
		ui.Text(c, v.err).FontSize(12).TextColor(colors(c).Err).Padding(8, sidebarInset)
	case v.result == nil:
		ui.Text(c, branchDiffText("gitChooseBaseBranch")).FontSize(12).TextColor(colors(c).Muted).Padding(8, sidebarInset)
	case len(v.files) == 0:
		ui.Text(c, branchDiffText("gitNoBranchChanges")).FontSize(12).TextColor(colors(c).Muted).Padding(8, sidebarInset)
	default:
		ui.Scroll(c.Key("branch-diff-files")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(0, 0, 8).Children(func() {
				for _, f := range v.files {
					row := ui.Row(c.Key(f.Path)).FillWidth().Height(28).Padding(0, sidebarInset).Gap(7).AlignItems(ui.Center).
						Cursor(ui.CursorPointer).Label(f.Path).Tooltip(branchDiffText("gitReadOnly"))
					if row.Hovered() {
						row.Background(colors(c).Pill)
					}
					row.OnClick(func() { a.openBranchDiff(a.focus, v.baseRef, v.baseName, f.Path) })
					row.Children(func() {
						fileIcon(c, filepath.Base(f.Path), 13)
						name := filepath.ToSlash(f.Path)
						if f.OldPath != "" {
							name = filepath.ToSlash(f.OldPath) + " → " + name
						}
						ui.Text(c, name).FontSize(11).SingleLine().Shrink(1)
						ui.Box(c).Grow(1)
						if f.Added < 0 {
							ui.Text(c, branchDiffText("gitBinary")).FontSize(10).TextColor(colors(c).Muted)
						} else {
							if f.Added > 0 {
								ui.Text(c, fmt.Sprintf("+%d", f.Added)).FontSize(10).TextColor(a.gitStatusColor('A'))
							}
							if f.Deleted > 0 {
								ui.Text(c, fmt.Sprintf("−%d", f.Deleted)).FontSize(10).TextColor(a.gitStatusColor('D'))
							}
						}
					})
				}
			})
		})
	}
}

func branchDiffTotals(files []branchDiffFile) (added, deleted int) {
	for _, f := range files {
		if f.Added >= 0 {
			added += f.Added
			deleted += f.Deleted
		}
	}
	return added, deleted
}

func branchRef(b git.Branch) string {
	if b.Remote {
		return "refs/remotes/" + b.Name
	}
	return "refs/heads/" + b.Name
}

func (a *App) openBaseBranchPicker(at ui.Rect, target *group) {
	if a.git.root == "" {
		return
	}
	a.openBranchPicker(at)
	a.git.picker.compare, a.git.picker.target = true, target
}

func (a *App) selectBranchBase(b git.Branch, target *group) {
	ref, name := branchRef(b), b.Name
	if a.git.branchPanel == nil || a.git.branchPanel.baseRef != ref {
		a.git.branchPanel = &branchDiffView{baseRef: ref, baseName: name, shown: branchDiffFilesAtOnce, collapsed: map[string]bool{}}
	}
	a.git.branchDiffMode = compareBaseMode
	a.loadBranchDiff(a.git.branchPanel, false)
	if target != nil {
		a.openBranchDiff(target, ref, name, "")
	}
}

func (a *App) openBranchDiff(g *group, baseRef, baseName, path string) {
	if g == nil {
		g = a.focus
	}
	if g == nil {
		return
	}
	a.git.branchDiffMode = compareBaseMode
	if a.git.branchPanel == nil || a.git.branchPanel.baseRef != baseRef {
		a.git.branchPanel = &branchDiffView{baseRef: baseRef, baseName: baseName, shown: branchDiffFilesAtOnce, collapsed: map[string]bool{}}
		a.loadBranchDiff(a.git.branchPanel, false)
	}
	key := branchDiffTab(baseRef)
	if v := g.diffs[key]; v != nil && v.branch != nil {
		v.branch.jumpPath = path
		if path == "" {
			v.branch.scroll.Y = 0
		} else {
			v.branch.reveal(path)
		}
		a.activateIn(g, key)
		return
	}
	v := &branchDiffView{baseRef: baseRef, baseName: baseName, shown: branchDiffFilesAtOnce, collapsed: map[string]bool{}, jumpPath: path}
	panel := a.git.branchPanel
	if panel.baseRef == baseRef && panel.result != nil && panel.fingerprint == a.git.branchFingerprint {
		*v = *panel
		v.scroll, v.jumpPath = ui.ScrollState{}, path
		v.collapsed = cloneBoolMap(panel.collapsed)
		v.seq = 0
		v.reveal(path)
	} else {
		a.loadBranchDiff(v, false)
	}
	if g.diffs == nil {
		g.diffs = map[string]*gitDiffTab{}
	}
	g.diffs[key] = &gitDiffTab{name: branchDiffText("gitSince") + " " + baseName, branch: v}
	g.addTab(key, len(g.order))
	a.activateIn(g, key)
	a.term.maximized = false
}

func (v *branchDiffView) reveal(path string) {
	if path == "" {
		return
	}
	if v.collapsed == nil {
		v.collapsed = map[string]bool{}
	}
	v.collapsed[path] = false
	for i, f := range v.files {
		if f.Path == path {
			v.shown = max(v.shown, i+1)
			return
		}
	}
}

func cloneBoolMap(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (a *App) refreshBranchDiffs() {
	seen := map[*branchDiffView]bool{}
	refresh := func(v *branchDiffView) {
		if v != nil && !seen[v] {
			seen[v] = true
			a.loadBranchDiff(v, false)
		}
	}
	refresh(a.git.branchPanel)
	for _, g := range a.groups() {
		for _, v := range g.diffs {
			if v != nil {
				refresh(v.branch)
			}
		}
	}
}

func (a *App) loadBranchDiff(v *branchDiffView, force bool) {
	if v == nil || v.baseRef == "" || a.git.root == "" || a.wsCtx == nil || v.loading && !force {
		return
	}
	fingerprint := a.git.branchFingerprint
	if !force && v.fingerprint == fingerprint && (v.result != nil || v.err != "") {
		return
	}
	v.seq++
	v.loading, v.err = true, ""
	seq, gen, baseRef, root, ctx := v.seq, a.gen, v.baseRef, a.git.root, a.wsCtx
	go func() {
		result, err := git.CompareBranch(ctx, root, baseRef)
		var patches []editor.FilePatch
		if err == nil {
			patches, err = editor.ParsePatch(string(result.Patch))
		}
		a.post.Post(func() {
			a.acceptBranchDiff(v, seq, gen, baseRef, root, fingerprint, result, patches, err)
		})
	}()
}

func (a *App) acceptBranchDiff(v *branchDiffView, seq uint64, gen int, baseRef, root, fingerprint string, result git.BranchDiffResult, patches []editor.FilePatch, err error) {
	if a.gen != gen || a.git.root != root || v.seq != seq || v.baseRef != baseRef {
		return
	}
	v.loading = false
	if err != nil {
		v.err = gitErrLine(err)
		v.fingerprint = fingerprint
		return
	}
	if v.result != nil && bytes.Equal(v.patch, result.Patch) {
		v.result, v.fingerprint = &result, fingerprint
		v.reveal(v.jumpPath)
		return
	}
	byName := make(map[string]editor.FilePatch, len(patches))
	for _, p := range patches {
		name := p.NewName
		if name == "" {
			name = p.OldName
		}
		byName[name] = p
	}
	files := make([]branchDiffFile, 0, len(result.Files))
	for _, f := range result.Files {
		bf := branchDiffFile{FileStat: f.FileStat, untracked: f.Untracked}
		if p, ok := byName[f.Path]; ok {
			bf.binary = p.Binary
			if !p.Binary && len(p.Hunks) > 0 {
				d := &editor.DiffState{Theme: a.edTheme, FitContent: true}
				a.applyDiffPrefs(d)
				d.SetPatch(p)
				bf.diff = d
			}
		}
		files = append(files, bf)
	}
	v.result, v.files, v.patch, v.fingerprint = &result, files, bytes.Clone(result.Patch), fingerprint
	if v.shown == 0 {
		v.shown = branchDiffFilesAtOnce
	}
	v.reveal(v.jumpPath)
}

func (a *App) branchDiffContent(c *ui.Context, g *group, v *branchDiffView) {
	switch {
	case v.loading && v.result == nil:
		ui.Row(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, branchDiffText("gitReadingBranchDiff")).FontSize(13).TextColor(colors(c).Muted)
		})
	case v.err != "":
		ui.Text(c, v.err).FontSize(13).TextColor(colors(c).Err).Padding(16)
	case v.result == nil:
		ui.Text(c, branchDiffText("gitReadingBranchDiff")).FontSize(13).TextColor(colors(c).Muted).Padding(16)
	case len(v.files) == 0:
		ui.Column(c).Grow(1).Fill().Center().Children(func() {
			ui.Text(c, branchDiffText("gitNoBranchChanges")).FontSize(14).TextColor(colors(c).Muted)
		})
	default:
		ui.Scroll(c.Key(v.baseRef)).Grow(1).FillWidth().TrackScroll(&v.scroll).Label(branchDiffText("gitCompareAgainstBase")).Children(func() {
			ui.Column(c).FillWidth().Padding(16, 20, 24).Gap(10).Children(func() {
				for i, f := range v.files {
					if i >= v.shown {
						rest := len(v.files) - v.shown
						if ui.Button(c, branchDiffMoreFiles(rest)).FontSize(12).Clicked() {
							v.shown += branchDiffFilesAtOnce
						}
						return
					}
					ui.Column(c.Key(f.Path)).FillWidth().Radius(6).BorderWidth(1).BorderColor(colors(c).Separator).Children(func() {
						header := ui.Row(c).FillWidth().Height(32).Padding(0, 10).Gap(8).AlignItems(ui.Center).
							Background(colors(c).Pill).Cursor(ui.CursorPointer).Label(f.Path)
						header.OnClick(func() { v.collapsed[f.Path] = !v.collapsed[f.Path] })
						if v.jumpPath == f.Path {
							header.ScrollIntoView()
							v.jumpPath = ""
						}
						header.Children(func() {
							turn := float32(90)
							if v.collapsed[f.Path] {
								turn = 0
							}
							symbolIcon(c, icChevron, 9, colors(c).Muted).Rotate(turn)
							fileIcon(c, filepath.Base(f.Path), 14)
							name := filepath.ToSlash(f.Path)
							if f.OldPath != "" {
								name = filepath.ToSlash(f.OldPath) + " → " + name
							}
							ui.Text(c, name).FontSize(12).Bold().SingleLine().Shrink(1)
							ui.Box(c).Grow(1)
							if f.untracked {
								ui.Text(c, branchDiffText("gitUntracked")).FontSize(10).TextColor(colors(c).Muted)
							}
							if f.Added < 0 {
								ui.Text(c, branchDiffText("gitBinary")).FontSize(11).TextColor(colors(c).Muted)
							} else {
								ui.Text(c, fmt.Sprintf("+%d", f.Added)).FontSize(11).TextColor(a.gitStatusColor('A'))
								ui.Text(c, fmt.Sprintf("−%d", f.Deleted)).FontSize(11).TextColor(a.gitStatusColor('D'))
							}
						})
						if v.collapsed[f.Path] {
							return
						}
						switch {
						case f.diff != nil:
							editor.DiffView(c, f.diff).FillWidth()
						case f.binary:
							ui.Text(c, branchDiffText("gitBinaryNotShown")).FontSize(12).TextColor(colors(c).Muted).Padding(10, 12)
						default:
							ui.Text(c, branchDiffText("gitNoTextDiff")).FontSize(12).TextColor(colors(c).Muted).Padding(10, 12)
						}
					})
				}
			})
		})
	}
}
