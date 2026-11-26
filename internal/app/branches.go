package app

import (
	"context"
	"fmt"
	"path/filepath"
	"see/locales"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"see/internal/git"
	"see/internal/workspace"
)

const branchPreviewDebounce = 140 * time.Millisecond

// The branch picker, as Xcode's: the branch under the project's name in
// the toolbar opens a list of the branches to switch to, local ones then
// remote ones, with a field that filters them and makes a new one.

type branchPicker struct {
	open         bool
	query        string
	appliedQuery string
	sel          int
	list         ui.ListState
	branches     []git.Branch
	loading      bool
	err          string
	seq          uint64
	at           ui.Rect // the button it hangs from
	anchor       ui.Rect // the toolbar's branch, as last drawn
	compare      bool
	target       *group

	previewTarget    string
	previewBranch    git.Branch
	preview          *git.BranchPreview
	previewFiles     []branchPreviewFile
	previewOpenCount int
	previewLoading   bool
	previewErr       string
	previewSeq       uint64
	previewTimer     *time.Timer
	previewCancel    context.CancelFunc
	hoveredBranchKey string
	pointerMoveSeq   uint64
	hoveredBranchSeq uint64
}

type branchPreviewOpenFile struct {
	path, relative string
	unsaved        bool
}

type branchPreviewFile struct {
	git.BranchFileChange
	unsaved bool
}

// branchItem is a row of the picker: a section's heading, a branch, or
// the row that makes a branch of the query.
type branchItem struct {
	heading string
	branch  *git.Branch
	create  string
}

func (it branchItem) choosable() bool { return it.heading == "" }

// openBranchPicker shows the branches, read again each time it opens.
func (a *App) openBranchPicker(at ui.Rect) {
	if a.git.root == "" {
		a.note(locales.MsgThisFolderIsNotInAGitRepository(), true)
		return
	}
	p := &a.git.picker
	a.clearBranchPreview()
	a.clearBranchHover()
	p.open, p.query, p.appliedQuery, p.sel, p.err, p.loading, p.at = true, "", "", 0, "", true, at
	p.compare, p.target = false, nil
	p.list.Selected = &p.sel
	p.seq++
	seq, gen, scopeSeq, ctx, root := p.seq, a.gen, a.git.scopeSeq, a.wsCtx, a.git.root
	go func() {
		bs, err := git.Branches(ctx, root)
		a.post.Post(func() {
			if a.gen != gen || a.git.scopeSeq != scopeSeq || p.seq != seq {
				return
			}
			p.loading, p.branches = false, bs
			if err != nil {
				p.err = err.Error()
			}
			p.sel = firstChoosable(a.branchItems())
			items := a.branchItems()
			if p.sel < len(items) {
				a.previewBranchItem(items[p.sel])
			}
		})
	}()
}

func (a *App) closeBranchPicker() {
	p := &a.git.picker
	p.open = false
	p.seq++
	a.clearBranchPreview()
	a.clearBranchHover()
}

func (a *App) clearBranchPreview() {
	p := &a.git.picker
	p.previewSeq++
	if p.previewTimer != nil {
		p.previewTimer.Stop()
		p.previewTimer = nil
	}
	if p.previewCancel != nil {
		p.previewCancel()
		p.previewCancel = nil
	}
	p.previewTarget, p.previewErr = "", ""
	p.previewBranch = git.Branch{}
	p.preview = nil
	p.previewFiles = nil
	p.previewOpenCount = 0
	p.previewLoading = false
}

func (a *App) clearBranchHover() {
	p := &a.git.picker
	p.hoveredBranchKey = ""
	p.hoveredBranchSeq = p.pointerMoveSeq
}

func (a *App) previewBranchItem(it branchItem) {
	p := &a.git.picker
	if p.compare || !p.open || it.branch == nil {
		a.clearBranchPreview()
		return
	}
	b := *it.branch
	key := branchPreviewKey(b)
	if p.previewTarget == key && (p.previewLoading || p.preview != nil || p.previewErr != "") {
		return
	}
	a.clearBranchPreview()
	p.previewTarget, p.previewBranch = key, b
	p.previewLoading = true
	openFiles := a.branchPreviewOpenFiles()
	p.previewOpenCount = len(openFiles)
	branches := slices.Clone(p.branches)
	root, gen, seq := a.git.root, a.gen, p.previewSeq
	ctx, cancel := context.WithCancel(a.wsCtx)
	p.previewCancel = cancel
	p.previewTimer = time.AfterFunc(branchPreviewDebounce, func() {
		if ctx.Err() != nil {
			return
		}
		a.post.Post(func() {
			if !a.branchPreviewCurrent(key, gen, seq, ctx) {
				return
			}
			go func() {
				preview, err := git.PreviewBranch(ctx, root, b, branches)
				files := affectedOpenBranchFiles(openFiles, preview.Changes)
				a.post.Post(func() { a.finishBranchPreview(key, gen, seq, ctx, preview, files, err, cancel) })
			}()
		})
	})
}

func (a *App) finishBranchPreview(key string, gen int, seq uint64, ctx context.Context, preview git.BranchPreview, files []branchPreviewFile, err error, cancel context.CancelFunc) {
	if !a.branchPreviewCurrent(key, gen, seq, ctx) {
		return
	}
	p := &a.git.picker
	if p.previewTimer != nil {
		p.previewTimer.Stop()
		p.previewTimer = nil
	}
	p.previewCancel, p.previewLoading = nil, false
	if err != nil {
		p.previewErr = err.Error()
	} else {
		p.preview, p.previewFiles = &preview, files
	}
	if cancel != nil {
		cancel()
	}
}

func branchPreviewKey(b git.Branch) string {
	if b.Remote {
		return "remote\x00" + b.Name
	}
	return "local\x00" + b.Name
}

func (a *App) branchPreviewCurrent(key string, gen int, seq uint64, ctx context.Context) bool {
	p := &a.git.picker
	return ctx.Err() == nil && a.gen == gen && p.open && !p.compare && p.previewSeq == seq && p.previewTarget == key
}

func (a *App) branchPreviewOpenFiles() []branchPreviewOpenFile {
	root := a.branchPreviewRoot()
	if a.root == "" || root == "" {
		return nil
	}
	seen := map[string]bool{}
	var files []branchPreviewOpenFile
	for _, g := range a.groups() {
		for _, tab := range g.order {
			if isDiffTab(tab) {
				continue
			}
			path := tabPath(tab)
			d := a.docs[path]
			if path == "" || d == nil || d.untitled || !workspace.Within(a.root, path) || !workspace.Within(root, path) {
				continue
			}
			path = filepath.Clean(path)
			if seen[path] {
				continue
			}
			seen[path] = true
			rel, err := filepath.Rel(root, path)
			if err != nil || rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			files = append(files, branchPreviewOpenFile{
				path: path, relative: filepath.ToSlash(rel),
				unsaved: d.buf != nil && d.buf.Modified(),
			})
		}
	}
	return files
}

func (a *App) branchPreviewRoot() string {
	root := a.git.root
	realWorkspaceRoot := a.realRoot
	if realWorkspaceRoot == "" {
		realWorkspaceRoot = a.root
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if resolved, err := filepath.EvalSymlinks(realWorkspaceRoot); err == nil {
		realWorkspaceRoot = resolved
	}
	if workspace.Within(realWorkspaceRoot, root) || workspace.Within(root, realWorkspaceRoot) {
		if rel, err := filepath.Rel(realWorkspaceRoot, root); err == nil {
			return filepath.Clean(filepath.Join(a.root, rel))
		}
	}
	return workspace.DocPath(a.root, a.realRoot, a.git.root)
}

func affectedOpenBranchFiles(open []branchPreviewOpenFile, changes []git.BranchFileChange) []branchPreviewFile {
	var files []branchPreviewFile
	for _, file := range open {
		for _, change := range changes {
			if file.relative == change.Path || file.relative == change.OldPath {
				files = append(files, branchPreviewFile{BranchFileChange: change, unsaved: file.unsaved})
				break
			}
		}
	}
	return files
}

// branchItems are the picker's rows for its query.
func (a *App) branchItems() []branchItem {
	p := &a.git.picker
	q := strings.ToLower(strings.TrimSpace(p.query))
	var items []branchItem
	exact := false
	section := func(heading string, remote bool) {
		first := true
		for i := range p.branches {
			b := &p.branches[i]
			if b.Remote != remote || q != "" && !strings.Contains(strings.ToLower(b.Name), q) {
				continue
			}
			if strings.EqualFold(b.Name, strings.TrimSpace(p.query)) {
				exact = true
			}
			if first {
				items = append(items, branchItem{heading: heading})
				first = false
			}
			items = append(items, branchItem{branch: b})
		}
	}
	section("Branches", false)
	section("Remote Branches", true)
	// Last, so that Return takes the first branch that matches.
	if name := strings.TrimSpace(p.query); name != "" && !exact && !p.compare {
		items = append(items, branchItem{create: name})
	}
	return items
}

func firstChoosable(items []branchItem) int {
	for i, it := range items {
		if it.choosable() {
			return i
		}
	}
	return 0
}

// moveBranchSel moves the selection by dir, over the headings.
func (a *App) moveBranchSel(items []branchItem, dir int) {
	p := &a.git.picker
	for i := p.sel + dir; i >= 0 && i < len(items); i += dir {
		if items[i].choosable() {
			p.sel = i
			p.list.ScrollTo(i, ui.Center)
			a.previewBranchItem(items[i])
			return
		}
	}
}

func (a *App) chooseBranch(it branchItem) {
	p := &a.git.picker
	switch {
	case it.create != "":
		a.closeBranchPicker()
		ctx, root := a.wsCtx, a.git.root
		a.runBranchJob("Created and switched to "+it.create+".", func() error {
			return git.CreateBranch(ctx, root, it.create)
		})
	case it.branch != nil && it.branch.Current:
		if p.compare {
			a.closeBranchPicker()
			a.selectBranchBase(*it.branch, p.target)
		} else {
			a.closeBranchPicker()
		}
	case it.branch != nil:
		a.closeBranchPicker()
		if p.compare {
			a.selectBranchBase(*it.branch, p.target)
		} else {
			b, all := *it.branch, p.branches
			ctx, root := a.wsCtx, a.git.root
			a.runBranchJob("Switched to "+b.LocalName()+".", func() error {
				return git.Switch(ctx, root, b, all)
			})
		}
	}
}

// runBranchJob switches the working tree off the UI thread; the files it
// changes come back through the watcher, and the tree is read again for
// a folder that is not watched.
func (a *App) runBranchJob(done string, job func() error) {
	if a.git.busy {
		return
	}
	a.git.busy = true
	gen, scopeSeq := a.gen, a.git.scopeSeq
	go func() {
		err := job()
		a.post.Post(func() {
			if a.gen != gen || a.git.scopeSeq != scopeSeq {
				return
			}
			a.git.busy = false
			if err != nil {
				a.note(locales.MsgGit()+gitErrLine(err), true)
			} else {
				a.note(done, false)
				if !a.watching {
					a.refreshAll()
				}
			}
			a.refreshGit()
		})
	}()
}

// gitErrLine is the part of a git error worth a line: its fatal or error
// message, else its first line.
func gitErrLine(err error) string {
	s := err.Error()
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "error: "); i >= 0 {
			return line[i+len("error: "):]
		}
		if i := strings.Index(line, "fatal: "); i >= 0 {
			return line[i+len("fatal: "):]
		}
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// branchButton is the branch under the project's name in the toolbar.
func (a *App) branchButton(c *ui.Context) {
	if a.git.branch == "" {
		return
	}
	a.git.picker.anchor = a.branchHandle.Bounds(c)
	b := ui.ButtonBase(c).Label(locales.MsgSwitchBranch()).Tooltip(locales.MsgSwitchBranch2()+a.git.branch).
		Padding(0, 4, 0, 2).Radius(5).Shrink(1).KeepFocus().Bind(&a.branchHandle)
	if b.Hovered() || b.Pressed() || a.git.picker.open {
		b.Background(colors(c).Pill)
	}
	b.OnClick(func() {
		if a.git.picker.open {
			a.closeBranchPicker()
			return
		}
		a.openBranchPicker(a.git.picker.anchor)
	})
	b.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			symbolIcon(c, icGit, 11, colors(c).Muted).Shrink(0)
			ui.Text(c, a.git.branch).FontSize(11).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
			if s := aheadBehind(a.git.ahead, a.git.behind); s != "" {
				ui.Text(c, s).FontSize(10).TextColor(colors(c).Dim).SingleLine().Shrink(0)
			}
			symbolIcon(c, icChevron, 8, colors(c).Dim).Shrink(0)
		})
	})
}

// branchPickerPanel is the picker's popover, under its button.
func (a *App) branchPickerPanel(c *ui.Context) {
	p := &a.git.picker
	items := a.branchItems()
	p.sel = min(max(p.sel, 0), max(len(items)-1, 0))
	ui.Overlay(c, func() {
		backdrop := ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0)
		backdrop.OnClick(a.closeBranchPicker)
		left, top, width, height := max(p.at.X-8, 8), p.at.Y+p.at.H+6, float32(360), float32(440)
		bottom, opensAbove, stack := float32(0), false, false
		if p.compare {
			w, h := c.Size()
			bounds := compareBranchPickerBounds(p.at, w, h)
			left, top, width, height = bounds.X, bounds.Y, bounds.W, bounds.H
			opensAbove = bounds.Y < p.at.Y
			if opensAbove {
				margin := min(float32(8), max(0, min(w, h)/2))
				panelBottom := max(margin, min(p.at.Y-6, h-margin))
				bottom = h - panelBottom
			}
		} else {
			w, h := c.Size()
			bounds, above, vertical := branchPickerBounds(p.at, w, h)
			left, top, width, height = bounds.X, bounds.Y, bounds.W, bounds.H
			opensAbove, stack = above, vertical
			if opensAbove {
				bottom = h - bounds.Y - bounds.H
			}
		}
		panel := ui.Column(c).Absolute().Left(left).Width(width)
		panel.MaxHeight(height)
		if p.compare && opensAbove {
			panel.Bottom(bottom)
		} else {
			panel.Top(top)
		}
		panel.Radius(12).Padding(6).Gap(4).Material(popoverGlass()).Border(1, colors(c).PopoverEdge)
		if !p.compare {
			panel.Role(ui.RoleGroup).Label(locales.MsgBranches())
		}
		panel.Shadow(0, 12, 36, 0, ui.RGBA(0, 0, 0, 0.35))
		panel.OnClick(func() {})
		panel.Children(func() {
			ui.Row(c).Padding(4, 6).Gap(6).AlignItems(ui.Center).Children(func() {
				symbolIcon(c, icSearch, 14, colors(c).Muted)
				placeholder, label := locales.MsgFilterOrNameANewBranch(), locales.MsgFilterBranches()
				if p.compare {
					placeholder, label = branchDiffText("gitSearchBaseBranch"), branchDiffText("gitChooseBaseBranch")
				}
				in := ui.TextInput(c, &p.query).Placeholder(placeholder).Label(label).
					FontSize(13).Grow(1).AutoFocus().Background(ui.Transparent).BorderWidth(0)
				switch {
				case in.Changed():
					if p.query == p.appliedQuery {
						break
					}
					p.appliedQuery = p.query
					filtered := a.branchItems()
					p.sel = firstChoosable(filtered)
					if p.sel < len(filtered) {
						a.previewBranchItem(filtered[p.sel])
					}
				case in.Submitted():
					if p.sel < len(items) {
						a.chooseBranch(items[p.sel])
					}
				case in.Shortcut(0, ui.KeyEscape):
					a.closeBranchPicker()
				case in.Shortcut(0, ui.KeyDown):
					a.moveBranchSel(items, 1)
				case in.Shortcut(0, ui.KeyUp):
					a.moveBranchSel(items, -1)
				}
			})
			ui.Box(c).Height(1).FillWidth().Background(colors(c).Separator)
			if p.compare {
				a.branchPickerList(c, items, 380).Grow(1).FillWidth()
			} else if stack {
				contentHeight := max(0, height-74)
				listHeight := min(float32(250), contentHeight*0.45)
				previewHeight := max(0, contentHeight-listHeight-8)
				a.branchPickerList(c, items, listHeight).MaxHeight(listHeight).FillWidth()
				ui.Box(c).Height(1).FillWidth().Background(colors(c).Separator)
				a.branchPreviewPane(c).MaxHeight(previewHeight).MinHeight(0).FillWidth()
			} else {
				listHeight := min(float32(380), max(0, height-64))
				previewHeight := max(0, height-64)
				ui.Row(c).FillWidth().MinHeight(0).Gap(8).AlignItems(ui.Start).Children(func() {
					a.branchPickerList(c, items, listHeight).Width(280).MaxHeight(listHeight)
					ui.Box(c).Width(1).AlignSelf(ui.Stretch).Background(colors(c).Separator)
					a.branchPreviewPane(c).Grow(1).MinWidth(0).MaxHeight(previewHeight).MinHeight(0)
				})
			}
		})
	})
}

func (a *App) branchPickerList(c *ui.Context, items []branchItem, maxHeight float32) ui.Element {
	p := &a.git.picker
	switch {
	case p.loading:
		return ui.Row(c).Padding(8).Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.MsgReadingBranches()).FontSize(12).TextColor(colors(c).Muted)
		})
	case p.err != "":
		return ui.Text(c, p.err).FontSize(12).TextColor(colors(c).Err).Padding(8)
	case len(items) == 0:
		return ui.Text(c, locales.MsgNoBranches()).FontSize(12).TextColor(colors(c).Muted).Padding(8)
	default:
		hoveredBranch := false
		list := ui.List(c, &p.list, len(items), func(i int) { a.branchRow(c, items, i, &hoveredBranch) }).
			MaxHeight(maxHeight).MinHeight(branchPickerListMinHeight(c, items, maxHeight)).Label(locales.MsgBranches())
		if !p.compare && !hoveredBranch {
			a.clearBranchHover()
		}
		return list
	}
}

func branchPickerListMinHeight(c *ui.Context, items []branchItem, maxHeight float32) float32 {
	var height float32
	for _, item := range items {
		switch {
		case item.heading != "":
			height += c.Theme().Space(8)
		case item.branch != nil:
			height += c.Theme().Space(14)
		default:
			height += c.Theme().Space(9)
		}
		if height >= maxHeight {
			return maxHeight
		}
	}
	return height
}

func (a *App) branchPreviewPane(c *ui.Context) ui.Element {
	p := &a.git.picker
	pane := ui.Column(c.Key("branch-preview-pane")).FillWidth().MinWidth(0).MinHeight(0).Padding(8, 10).Gap(8)
	pane.Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, p.previewBranch.Name).FontSize(14).Bold().SingleLine().Ellipsis("…").MinWidth(0).Shrink(1).Tooltip(p.previewBranch.Name)
			if p.preview != nil && p.preview.TargetRef != branchRef(p.previewBranch) {
				destination := strings.TrimPrefix(p.preview.TargetRef, "refs/heads/")
				localTarget := branchPreviewLocalTarget(destination)
				ui.Text(c, localTarget).FontSize(10).TextColor(colors(c).Muted).
					SingleLine().Ellipsis("…").Shrink(0).Tooltip(localTarget)
			}
		})
		ui.Text(c, branchPreviewText("gitBranchPreviewBasis")).FontSize(10).TextColor(colors(c).Muted).
			SingleLine().Ellipsis("…").Tooltip(branchPreviewText("gitBranchPreviewBasis"))
		ui.Text(c, branchPreviewText("gitBranchPreviewDirtyWarning")).FontSize(10).TextColor(colors(c).Muted).
			SingleLine().Ellipsis("…").Tooltip(branchPreviewText("gitBranchPreviewDirtyWarning"))
		ui.Box(c).Height(1).FillWidth().Background(colors(c).Separator)
		ui.Scroll(c.Key("branch-preview-scroll")).Grow(1).MinHeight(0).FillWidth().Children(func() {
			switch {
			case p.previewLoading:
				ui.Row(c).Padding(8, 2).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c)
					ui.Text(c, branchPreviewText("gitBranchPreviewLoading")).FontSize(12).TextColor(colors(c).Muted)
				})
			case p.previewErr != "":
				ui.Column(c).Padding(6, 2).Gap(5).Children(func() {
					ui.Text(c, branchPreviewText("gitBranchPreviewError")).FontSize(12).Bold().TextColor(colors(c).Err)
					ui.Text(c, p.previewErr).FontSize(11).TextColor(colors(c).Muted).Tooltip(p.previewErr)
				})
			case p.preview == nil:
				ui.Text(c, branchPreviewText("gitBranchPreviewPrompt")).FontSize(12).TextColor(colors(c).Muted).Padding(8, 2)
			default:
				a.branchPreviewContents(c)
			}
		})
	})
	return pane
}

func (a *App) branchPreviewContents(c *ui.Context) {
	p := &a.git.picker
	ui.Column(c).FillWidth().Gap(10).Padding(2, 0).Children(func() {
		ui.Column(c).FillWidth().Gap(5).Children(func() {
			ui.Text(c, branchPreviewText("gitBranchPreviewRecentCommits")).FontSize(11).Bold().TextColor(colors(c).Muted)
			if len(p.preview.Commits) == 0 {
				ui.Text(c, branchPreviewText("gitBranchPreviewNoCommits")).FontSize(11).TextColor(colors(c).Muted).Padding(2, 0)
			}
			for i, commit := range p.preview.Commits {
				ui.Column(c.Key(fmt.Sprintf("branch-preview-commit-%d", i))).FillWidth().Gap(2).Children(func() {
					ui.Text(c, commit.Subject).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Tooltip(commit.Subject)
					ui.Row(c).FillWidth().Gap(6).Children(func() {
						ui.Text(c, commit.Short).FontSize(10).TextColor(colors(c).Muted).Shrink(0)
						ui.Text(c, commit.Author).FontSize(10).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1).Tooltip(commit.Author)
						ui.Box(c).Grow(1)
						if !commit.Date.IsZero() {
							ui.Text(c, relativeTime(commit.Date, time.Now())).FontSize(10).TextColor(colors(c).Muted).SingleLine().Shrink(0)
						}
					})
				})
			}
		})
		ui.Column(c).FillWidth().Gap(5).Children(func() {
			ui.Text(c, branchPreviewCount("gitBranchPreviewOpenFiles", len(p.previewFiles))).FontSize(11).Bold().TextColor(colors(c).Muted)
			if len(p.preview.Changes) == 0 {
				ui.Text(c, branchPreviewText("gitBranchPreviewNoChanges")).FontSize(11).TextColor(colors(c).Muted).Padding(2, 0)
			}
			switch {
			case p.previewOpenCount == 0:
				ui.Text(c, branchPreviewText("gitBranchPreviewNoOpenFiles")).FontSize(11).TextColor(colors(c).Muted).Padding(2, 0)
			case len(p.preview.Changes) > 0 && len(p.previewFiles) == 0:
				ui.Text(c, branchPreviewText("gitBranchPreviewNoAffectedOpenFiles")).FontSize(11).TextColor(colors(c).Muted).Padding(2, 0)
			default:
				for i, file := range p.previewFiles {
					path := file.Path
					if file.OldPath != "" {
						path = file.OldPath + " → " + file.Path
					}
					ui.Row(c.Key(fmt.Sprintf("branch-preview-file-%d", i))).FillWidth().Gap(6).AlignItems(ui.Center).Tooltip(path).Children(func() {
						ui.Text(c, string(file.Status)).FontSize(10).Bold().TextColor(a.gitStatusColor(file.Status)).Shrink(0)
						ui.Text(c, path).FontSize(11).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
						if file.unsaved {
							ui.Text(c, branchPreviewText("gitBranchPreviewUnsaved")).FontSize(9).TextColor(c.Theme().Warning).SingleLine().Shrink(0)
						}
					})
				}
			}
		})
	})
}

func branchPreviewText(key string) string {
	return locales.Catalog.Format(i18n.Locale(), key, nil)
}

func branchPreviewLocalTarget(name string) string {
	return locales.Catalog.Format(i18n.Locale(), "gitBranchPreviewLocalTarget", map[string]any{"branch": name})
}

func branchPreviewCount(key string, count int) string {
	return fmt.Sprintf(branchPreviewText(key), count)
}

func branchPickerBounds(at ui.Rect, width, height float32) (ui.Rect, bool, bool) {
	margin := min(float32(8), max(0, min(width, height)/2))
	availableWidth := max(0, width-2*margin)
	stack := availableWidth < 660
	panelWidth := min(float32(700), availableWidth)
	maxHeight := min(float32(520), max(0, height-2*margin))
	if stack {
		panelWidth = min(float32(460), availableWidth)
		maxHeight = min(float32(620), max(0, height-2*margin))
	}
	left := max(margin, min(at.X-8, width-panelWidth-margin))
	top := at.Y + at.H + 6
	panelHeight := min(maxHeight, max(0, height-margin-top))
	above := false
	if panelHeight < min(maxHeight, 300) {
		aboveHeight := max(0, at.Y-margin-6)
		if aboveHeight > panelHeight {
			panelHeight = min(maxHeight, aboveHeight)
			top = at.Y - 6 - panelHeight
			above = true
		}
	}
	top = max(margin, min(top, height-margin-panelHeight))
	return ui.Rect{X: left, Y: top, W: panelWidth, H: panelHeight}, above, stack
}

func compareBranchPickerBounds(at ui.Rect, width, height float32) ui.Rect {
	margin := min(float32(8), max(0, min(width, height)/2))
	panelWidth := min(float32(360), max(0, width-2*margin))
	maxHeight := min(float32(440), max(0, height-2*margin))
	left := max(margin, min(at.X-8, width-panelWidth-margin))
	top := at.Y + at.H + 6
	panelHeight := min(maxHeight, max(0, height-margin-top))
	if panelHeight < min(maxHeight, 220) {
		above := max(0, at.Y-margin-6)
		if above > panelHeight {
			panelHeight = min(maxHeight, above)
			top = at.Y - 6 - panelHeight
		}
	}
	top = max(margin, min(top, height-margin-panelHeight))
	return ui.Rect{X: left, Y: top, W: panelWidth, H: panelHeight}
}

func (a *App) branchRow(c *ui.Context, items []branchItem, i int, hoveredBranch *bool) {
	p := &a.git.picker
	it := items[i]
	if it.heading != "" {
		ui.Text(c.Key(i), it.heading).FontSize(11).Bold().TextColor(colors(c).Muted).Padding(8, 8, 3, 8)
		return
	}
	row := ui.Row(c.Key(i)).FillWidth().Padding(5, 8).Gap(8).Radius(6).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	if !p.compare {
		row.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind == ui.InputPointerMove {
				p.pointerMoveSeq++
			}
			return false
		})
	}
	hovered := row.Hovered()
	if !p.compare && hovered && it.branch != nil {
		*hoveredBranch = true
		key := branchPreviewKey(*it.branch)
		if p.hoveredBranchKey != key || p.hoveredBranchSeq != p.pointerMoveSeq {
			p.hoveredBranchKey, p.hoveredBranchSeq = key, p.pointerMoveSeq
			p.sel = i
			a.previewBranchItem(it)
		}
	}
	selected := i == p.sel
	text, sub := colors(c).Text, colors(c).Muted
	if selected {
		row.Background(c.Theme().Accent)
		text, sub = ui.RGB(0xff, 0xff, 0xff), ui.RGBA(0xff, 0xff, 0xff, 0.75)
	}
	if hovered && !selected {
		row.Background(colors(c).Pill)
	}
	row.OnClick(func() { a.chooseBranch(it) })
	if it.branch != nil {
		b := it.branch
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item(logText("gitLogRevealBranch") + b.Name).Chosen() {
				a.closeBranchPicker()
				a.revealLogRef(b.Name)
			}
		})
	}
	row.Children(func() {
		if it.create != "" {
			symbolIcon(c, icPlus, 13, text)
			ui.Text(c, locales.MsgCreateBranch()+it.create+"”").FontSize(13).TextColor(text).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
			return
		}
		b := it.branch
		check := ui.Box(c).Size(13, 13).Shrink(0)
		if b.Current {
			check.Children(func() { symbolIcon(c, icCheck, 13, text) })
		}
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			ui.Text(c, b.Name).FontSize(13).TextColor(text).SingleLine().Ellipsis("…")
			detail := b.Subject
			if b.Track != "" {
				detail = b.Track + " · " + detail
			}
			ui.Text(c, detail).FontSize(11).TextColor(sub).SingleLine().Ellipsis("…")
		})
		if !b.Date.IsZero() {
			ui.Text(c, relativeTime(b.Date, time.Now())).FontSize(11).TextColor(sub).SingleLine().Shrink(0)
		}
	})
}

// relativeTime tells how long ago t was, as Xcode's source control
// navigator: "5 min ago", "Yesterday", or the date.
func relativeTime(t, now time.Time) string {
	return relativeTimeIn(t, now, i18n.Locale())
}

func relativeTimeIn(t, now time.Time, locale string) string {
	text := func(key string) string { return locales.Catalog.Format(locale, key, nil) }
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return text("relativeJustNow")
	case d < time.Hour:
		return fmt.Sprintf(text("relativeMinutesAgo"), int(d/time.Minute))
	case d < 24*time.Hour && t.Day() == now.Day():
		return fmt.Sprintf(text("relativeHoursAgo"), int(d/time.Hour))
	case d < 48*time.Hour && t.Day() == now.AddDate(0, 0, -1).Day():
		return text("relativeYesterday")
	case d < 7*24*time.Hour:
		return text("relative" + t.Weekday().String())
	case t.Year() == now.Year():
		return t.Format(text("relativeDateThisYear"))
	}
	return t.Format(text("relativeDateWithYear"))
}
