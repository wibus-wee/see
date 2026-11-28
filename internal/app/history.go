package app

import (
	"fmt"
	"path/filepath"
	"see/locales"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
)

// The Git history, as Xcode's source control navigator and Peek: the
// commits of the branch, or of every branch, as a graph in the navigator,
// uncommitted changes on top; a commit opens in an editor with its
// message and files, and a file of it opens as a diff.

// historyPage is how many commits are read at a time.
const historyPage = 200

type gitHistory struct {
	commits  []git.Commit
	lanes    []string // the lanes the commits read leave for the next page
	lanesW   int      // the most lanes a row has, for the graph's width
	all      bool     // every branch, not only HEAD's
	path     string   // only commits that changed it, as the log's file scope
	head     string   // HEAD when the history was read
	loading  bool
	done     bool // no commits are left to read
	stale    bool // to be read again
	err      string
	seq      uint64
	list     ui.ListState
	total    int             // how many commits the walk has, -1 while unknown
	mainline map[string]bool // commits the unpushed and incoming counts are of
	unpushed map[string]bool // HEAD's commits the branch it tracks has not
	incoming map[string]bool
}

// commitView is a commit's details, in a tab or under the log.
type commitView struct {
	commit       git.Commit
	details      git.CommitDetails
	message      git.Message
	files        []commitFile
	filesLoaded  bool
	filesLoading bool
	shown        int
	collapsed    map[string]bool
	loading      bool
	err          string
	scroll       ui.ScrollState
}

func newCommitView(cm git.Commit) *commitView {
	return &commitView{commit: cm, loading: true, shown: commitFilesAtOnce, collapsed: map[string]bool{}}
}

// commitTab and commitFileTab are the keys of a commit's tab and of the
// diff of one of its files. They are diff tabs: not files of the
// workspace, and not kept between launches.
func commitTab(hash string) string           { return diffTabPrefix + "commit:" + hash }
func commitFileTab(hash, path string) string { return diffTabPrefix + hash + ":" + path }

// syncHistory reads the history again when it is shown and out of date.
func (a *App) syncHistory() {
	h := &a.git.history
	if h.loading || a.git.root == "" || a.git.head == "" {
		return
	}
	if h.commits == nil && h.err == "" || h.stale || h.head != a.git.head {
		a.loadHistory(true)
	}
}

// loadHistory reads a page of the history: the first again with reset,
// else the one after the commits read.
func (a *App) loadHistory(reset bool) {
	h := &a.git.history
	if a.git.root == "" || h.loading && !reset || !reset && h.done {
		return
	}
	h.seq++
	h.loading, h.stale, h.err = true, false, ""
	skip, lanes := len(h.commits), h.lanes
	if reset {
		skip, lanes = 0, nil
	}
	seq, gen, ctx, root, all, head, path := h.seq, a.gen, a.wsCtx, a.git.root, h.all, a.git.head, h.path
	go func() {
		commits, err := git.LogFile(ctx, root, all, skip, historyPage, path)
		lanes = git.Graph(commits, lanes)
		var unpushed, incoming, mainline map[string]bool
		total := -1
		if reset {
			unpushed = git.Unpushed(ctx, root)
			incoming = git.Incoming(ctx, root)
			mainline = map[string]bool{}
			for hash := range unpushed {
				mainline[hash] = true
			}
			for hash := range incoming {
				mainline[hash] = true
			}
			if path == "" {
				total = git.CountCommits(ctx, root, all)
			}
		}
		a.post.Post(func() {
			if a.gen != gen || h.seq != seq {
				return
			}
			h.loading = false
			if err != nil {
				h.err = gitErrLine(err)
				return
			}
			if reset {
				h.commits, h.lanesW, h.unpushed = nil, 0, unpushed
				h.incoming, h.mainline, h.total = incoming, mainline, total
			}
			h.commits = append(h.commits, commits...)
			h.lanes, h.head, h.done = lanes, head, len(commits) < historyPage
			for _, c := range commits {
				h.lanesW = max(h.lanesW, len(c.Before), len(c.After), c.Lane+1)
			}
		})
	}()
}

// gitChangesHeader is the head of the Git navigator's changes: their
// number, and the actions on the repository at its end.
func (a *App) gitChangesHeader(c *ui.Context) {
	count := len(a.git.changes)
	if a.git.branchDiffMode == compareBaseMode {
		count = 0
		if a.git.branchPanel != nil {
			count = len(a.git.branchPanel.files)
		}
	}
	a.gitSectionHeader(c, locales.MsgChanges(), count, &a.git.changesClosed, func() {
		a.branchDiffModePicker(c)
		if a.git.changesClosed || a.git.branchDiffMode == compareBaseMode {
			return
		}
		a.gitChangesViewButton(c)
		if len(a.git.changes) > 0 && glyphButton(c, icStash, locales.MsgStashChanges(), locales.MsgStashChanges()+"…", a.git.stashing, a.git.busy).Clicked() {
			if a.git.stashing {
				a.endStash()
			} else {
				a.beginStash()
			}
		}
		if a.git.syncing != "" {
			ui.Spinner(c).FontSize(10).Tooltip(a.git.syncing)
		}
		a.syncButtons(c)
		tip := locales.MsgRefreshStatus()
		if a.hasRemote() {
			tip = locales.MsgFetchAndRefreshStatus()
		}
		if glyphButton(c, icRefresh, locales.MsgRefresh(), tip, false, a.git.busy).Clicked() {
			a.fetchAndRefresh()
		}
	})
}

// gitHistoryHeader is the head of the Git navigator's history, with
// the choice of every branch or HEAD's at its end.
func (a *App) gitHistoryHeader(c *ui.Context) {
	h := &a.git.history
	a.gitSectionHeader(c, locales.MsgHistory(), -1, &a.git.historyClosed, func() {
		if a.git.historyClosed {
			return
		}
		if h.loading {
			ui.Spinner(c).FontSize(10)
		}
		tip := locales.MsgShowEveryBranch()
		if h.all {
			tip = fmt.Sprintf(locales.MsgShowOnlySAndUpstream(), a.git.branch)
		}
		if glyphToggle(c, icBranches, locales.MsgAllBranches(), tip, h.all, false).Clicked() {
			h.all, h.stale = !h.all, true
			a.syncHistory()
		}
		if glyphButton(c, icGit, locales.MsgShowLog(), locales.MsgShowTheLogInATab(), false, false).Clicked() {
			a.openGitLog()
		}
	})
}

// gitSectionHeader is the head of a section of the Git navigator, as
// VS Code's views': an arrow and its title, which a click folds and
// unfolds, a count when not negative, and actions at its end.
func (a *App) gitSectionHeader(c *ui.Context, title string, count int, closed *bool, actions func()) {
	a.gitSectionHeaderStyle(c, title, count, closed, actions, true)
}

func (a *App) gitCompactSectionHeader(c *ui.Context, title string, count int, closed *bool, actions func()) {
	a.gitSectionHeaderStyle(c, title, count, closed, actions, false)
}

func (a *App) gitSectionHeaderStyle(c *ui.Context, title string, count int, closed *bool, actions func(), main bool) {
	height, inset := float32(gitSidebarGroupHeaderHeight), float32(sidebarInset+gitSidebarTreeIndent)
	if main {
		height, inset = float32(gitSidebarMainHeaderHeight), float32(sidebarInset)
	}
	ui.Row(c.Key(title)).Shrink(0).FillWidth().Height(height).Padding(0, inset).Gap(2).AlignItems(ui.Center).Children(func() {
		b := ui.ButtonBase(c).Label(title).Expanded(!*closed).Padding(0, 4, 0, 0).Radius(4).Gap(gitSidebarColumnGap).
			AlignItems(ui.Center).MinWidth(0).Shrink(1).KeepFocus()
		if b.Hovered() {
			b.Background(colors(c).Pill)
		}
		b.OnClick(func() {
			*closed = !*closed
			if closed == &a.git.historyClosed && !*closed {
				a.syncHistory()
			}
		})
		turn := float32(90)
		if *closed {
			turn = 0
		}
		b.Children(func() {
			ui.Box(c).Size(gitSidebarIconColumn, 12).Shrink(0).Center().Children(func() {
				symbolIcon(c, icChevron, 9, colors(c).Dim).Rotate(turn)
			})
			if main {
				gitSidebarMainLabel(c, title)
			} else {
				gitSidebarGroupLabel(c, title)
			}
			gitSidebarCount(c, count)
		})
		ui.Box(c).Grow(1).MinWidth(0).Shrink(1)
		actions()
	})
}

// historyNavigator is the history in the navigator.
func (a *App) historyNavigator(c *ui.Context) {
	h := &a.git.history
	a.syncHistory()
	switch {
	case h.err != "":
		ui.Text(c, h.err).FontSize(12).TextColor(colors(c).Err).Padding(10, 14)
	case h.commits == nil && h.loading:
		ui.Row(c).Padding(8, 14).Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.MsgReadingHistory()).FontSize(12).TextColor(colors(c).Muted)
		})
	case len(h.commits) == 0 && a.git.head == "":
		ui.Text(c, locales.MsgNoCommits()).FontSize(12).TextColor(colors(c).Muted).Padding(10, 14)
	default:
		ui.List(c.Key("git-history-list"), &h.list, len(h.commits), func(i int) { a.historyRow(c, i) }).Grow(1).FillWidth().Padding(0, sidebarInset).Label(locales.MsgHistory())
	}
}

// The graph's geometry: rows of one height, so that the lanes of one row
// meet those of the next.
const (
	historyRowHeight = 40
	historyGap       = 8
	avatarSize       = 22
	graphLane        = 12
	graphPad         = 8
	graphMaxLanes    = 8
)

func graphX(lane int) float32 { return graphPad + float32(lane)*graphLane }

// laneColors are the graph's lanes', by column.
var laneColors = []ui.Color{
	ui.RGB(0x0a, 0x84, 0xff), ui.RGB(0xff, 0x9f, 0x0a), ui.RGB(0x30, 0xd1, 0x58), ui.RGB(0xbf, 0x5a, 0xf2),
	ui.RGB(0xff, 0x37, 0x5f), ui.RGB(0x64, 0xd2, 0xff), ui.RGB(0xff, 0xd6, 0x0a), ui.RGB(0xac, 0x8e, 0x68),
}

func laneColor(i int) ui.Color { return laneColors[i%len(laneColors)] }

// historyRow is a commit, as Peek's: after its lanes of the graph, its
// author's avatar with the first co-author's over its corner, its
// subject, and under it the branches and tags at it, when and who; an
// arrow up marks a commit not pushed yet, as Xcode's.
func (a *App) historyRow(c *ui.Context, i int) {
	h := &a.git.history
	if i == len(h.commits)-1 && !h.done && !h.loading {
		a.loadHistory(false)
	}
	cm := h.commits[i]
	open := a.focus.active == commitTab(cm.Hash)
	// The arrows belong to HEAD's lane alone: of every branch, a commit
	// that is also unpushed or incoming is not one to push or pull here.
	mainline := h.mainline == nil || h.mainline[cm.Hash]
	unpushed := mainline && h.unpushed[cm.Hash]
	incoming := mainline && h.incoming[cm.Hash]
	row := ui.Row(c.Key(cm.Hash)).FillWidth().Height(historyRowHeight).Padding(0, 8, 0, 0).Gap(historyGap).AlignItems(ui.Center).Radius(6).
		Label(cm.Subject)
	// A light selection, for the graph to show through.
	switch {
	case open:
		row.Background(c.Theme().Accent.Alpha(0.2))
	case row.Hovered():
		row.Background(colors(c).Pill)
	}
	row.OnClick(func() { a.openCommit(cm) })
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.MsgCopyCommitHash()).Chosen() {
			c.Services().WriteClipboard(cm.Hash)
		}
		if m.Item(locales.MsgCopySubject()).Chosen() {
			c.Services().WriteClipboard(cm.Subject)
		}
		a.commitRowItems(m, c, cm)
	})
	row.Children(func() {
		lanes := min(max(h.lanesW, 1), graphMaxLanes)
		ui.Box(c).Size(graphX(lanes-1)+graphLane, historyRowHeight).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
			drawGraphRow(p, r, cm)
		})
		a.commitAvatars(c, cm, avatarSize)
		ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
			ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
				commitSubject(c, cm.Subject, commitSubjectRow)
				if unpushed {
					ui.Text(c, "↑").FontSize(11).Bold().TextColor(c.Theme().Accent).Shrink(0).Tooltip(locales.MsgNotPushedYet())
				}
				if incoming {
					ui.Text(c, "↓").FontSize(11).Bold().TextColor(c.Theme().Accent).Shrink(0).Tooltip(logText("gitIncomingCommit"))
				}
			})
			ui.Text(c, a.commitByline(cm)).FontSize(11).TextColor(colors(c).Muted).SingleLine()
		})
	})
}

// commitByline is the line under a commit's subject: the branches and
// tags at it but the one checked out, when, and who.
func (a *App) commitByline(cm git.Commit) string {
	var parts []string
	shown, active := visibleCommitRefs(cm, a.git.branch)
	for _, ref := range shown {
		if ref != active {
			parts = append(parts, ref)
		}
	}
	return strings.Join(append(parts, shortDate(cm.Date, time.Now()), authors(cm)), " · ")
}

// authors names who made a commit, as Peek: the author, and one
// co-author by name or more by their number.
func authors(cm git.Commit) string {
	switch len(cm.CoAuthors) {
	case 0:
		return cm.Author
	case 1:
		return locales.Catalog.Format(i18n.Locale(), "msgAuthorAndCoAuthor", map[string]any{"author": cm.Author, "coauthor": cm.CoAuthors[0].Name})
	}
	n := len(cm.CoAuthors)
	return locales.Catalog.Format(i18n.Locale(), "msgAuthorAndNOthers", map[string]any{"author": cm.Author, "count": n})
}

// shortDate is when, as short as a list has room for, as Peek's and
// Finder's: the time today, Yesterday, the weekday this week, else the
// date.
func shortDate(t, now time.Time) string {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	text := func(key string) string { return locales.Catalog.Format(i18n.Locale(), key, nil) }
	switch {
	case !t.Before(today):
		return t.Format("15:04")
	case !t.Before(today.AddDate(0, 0, -1)):
		return text("relativeYesterday")
	case !t.Before(today.AddDate(0, 0, -6)):
		return text("relative" + t.Weekday().String())
	case t.Year() == now.Year():
		return t.Format(text("relativeDateThisYear"))
	}
	return t.Format(text("relativeDateWithYear"))
}

// fullDate is a commit's date and weekday in full, in the app's language.
func fullDate(t time.Time) string {
	text := func(key string) string { return locales.Catalog.Format(i18n.Locale(), key, nil) }
	return locales.Catalog.Format(i18n.Locale(), "relativeFullDate", map[string]any{
		"weekday": text("relative" + t.Weekday().String()),
		"date":    t.Format(text("relativeDateWithYear")),
		"time":    t.Format("15:04"),
	})
}

// refBadge is a branch or tag pointing at a commit.
func refBadge(c *ui.Context, name string, current bool) {
	bg, fg := colors(c).Pill, colors(c).Muted
	if current {
		bg, fg = c.Theme().Accent.Alpha(0.18), c.Theme().Accent
	}
	// MinWidth(0) lets the badge give way inside its column: without it
	// the text keeps its full width and the column's clip cuts it
	// mid-glyph, without even the ellipsis.
	ui.Text(c, name).FontSize(10).Bold().TextColor(fg).Background(bg).Padding(0, 5).Radius(4).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(2)
}

// drawGraphRow draws a commit's row of the graph: the lanes passing by,
// those that end in its dot, and those leaving it for its parents.
func drawGraphRow(p *ui.Painter, r ui.Rect, cm git.Commit) {
	drawGraphRowLanes(p, r, cm, graphMaxLanes)
}

func drawGraphRowLanes(p *ui.Painter, r ui.Rect, cm git.Commit, laneLimit int) {
	mid := r.Y + r.H/2
	x := func(lane int) float32 { return r.X + graphX(min(lane, laneLimit-1)) }
	curve := func(x0, y0, x1, y1 float32, color ui.Color) {
		var path ui.Path
		if x0 == x1 {
			path.MoveTo(x0, y0).LineTo(x1, y1)
		} else {
			m := (y0 + y1) / 2
			path.MoveTo(x0, y0).CubeTo(x0, m, x1, m, x1, y1)
		}
		p.StrokePath(&path, 1.5, color)
	}
	for i, hash := range cm.Before {
		switch {
		case hash == "":
		case hash != cm.Hash:
			curve(x(i), r.Y, x(i), r.Y+r.H, laneColor(i)) // passing by
		default:
			curve(x(i), r.Y, x(cm.Lane), mid, laneColor(i)) // ending here
		}
	}
	for _, parent := range cm.Parents {
		for j, hash := range cm.After {
			if hash == parent {
				color := laneColor(j)
				if j == cm.Lane {
					color = laneColor(cm.Lane)
				}
				curve(x(cm.Lane), mid, x(j), r.Y+r.H, color)
				break
			}
		}
	}
	color := laneColor(cm.Lane)
	var dot ui.Path
	dot.Circle(x(cm.Lane), mid, 4)
	if len(cm.Parents) > 1 {
		p.FillPath(&dot, p.Theme().Background)
		p.StrokePath(&dot, 2, color)
	} else {
		p.FillPath(&dot, color)
	}
	if len(cm.Refs) > 0 && cm.Refs[0] == "HEAD" {
		var ring ui.Path
		ring.Circle(x(cm.Lane), mid, 6.5)
		p.StrokePath(&ring, 1.5, color)
	}
}

// openCommit shows a commit in the focused editor, reading its message
// and files.
func (a *App) openCommit(cm git.Commit) {
	g := a.focus
	key := commitTab(cm.Hash)
	if g.diffs == nil {
		g.diffs = map[string]*gitDiffTab{}
	}
	if v := g.diffs[key]; v != nil && g.has(key) {
		a.activateIn(g, key)
		return
	}
	v := newCommitView(cm)
	g.diffs[key] = &gitDiffTab{name: cm.Subject, rev: cm.Short, hash: cm.Hash, commit: v}
	// One commit at a time takes the preview tab, as files do.
	if old := g.preview; old != "" && old != key && strings.HasPrefix(old, diffTabPrefix+"commit:") {
		a.closeTab(g, old)
	}
	g.addTab(key, len(g.order))
	g.preview = key
	a.activateIn(g, key)
	a.term.maximized = false
	a.loadCommitView(v)
}

// loadCommitView reads a commit's message and files into v.
func (a *App) loadCommitView(v *commitView) { a.loadCommitViewWhen(v, nil) }

// loadCommitViewWhen is loadCommitView for a view still current when
// valid says so, as the log's chosen commit is while it stays chosen.
func (a *App) loadCommitViewWhen(v *commitView, valid func() bool) {
	cm := v.commit
	v.loading, v.err = true, ""
	v.filesLoaded, v.filesLoading = false, false
	gen, scopeSeq, ctx, root := a.gen, a.git.scopeSeq, a.wsCtx, a.git.root
	go func() {
		d, msg, files, err := a.readCommitDetails(ctx, root, cm.Hash, cm.Email)
		a.post.Post(func() {
			if a.gen != gen || a.git.scopeSeq != scopeSeq {
				return
			}
			if valid != nil && !valid() {
				return
			}
			v.loading, v.details, v.message, v.files = false, d, msg, files
			v.filesLoaded = err == nil
			if err != nil {
				v.err = gitErrLine(err)
			}
		})
	}()
}

// loadCommitFilesView reads a commit's diffs after its summary, as a
// restored tab does when it first shows.
func (a *App) loadCommitFilesView(v *commitView) {
	if v.filesLoaded || v.filesLoading {
		return
	}
	v.filesLoading = true
	cm, d := v.commit, v.details
	gen, scopeSeq, ctx, root := a.gen, a.git.scopeSeq, a.wsCtx, a.git.root
	go func() {
		files := a.readCommitFiles(ctx, root, cm.Hash, d)
		a.post.Post(func() {
			if a.gen != gen || a.git.scopeSeq != scopeSeq {
				return
			}
			v.files, v.filesLoaded, v.filesLoading = files, true, false
		})
	}()
}

// openCommitFile shows the diff of a file of a commit beside the others.
func (a *App) openCommitFile(g *group, cm git.Commit, f git.FileStat) {
	hash := fileCommit(cm, f)
	key := commitFileTab(hash, f.Path)
	if g.has(key) && g.diffs[key] != nil {
		a.activateIn(g, key)
		return
	}
	a.git.diffSeq++
	seq, gen, ctx, root := a.git.diffSeq, a.gen, a.wsCtx, a.git.root
	go func() {
		out, err := git.CommitFileDiff(ctx, root, hash, f)
		patches, parseErr := editor.ParsePatch(string(out))
		a.post.Post(func() {
			if a.gen != gen || a.git.diffSeq != seq || a.layout.find(g) == nil {
				return
			}
			switch {
			case err != nil:
				a.fail(locales.MsgGitDiff() + gitErrLine(err))
				return
			case parseErr != nil || len(patches) == 0 || patches[0].Binary:
				a.fail(locales.MsgNoTextDiffAvailableFor() + f.Path)
				return
			}
			v := a.newGitDiffTab(patches[0], f.Path, false)
			v.rev, v.hash, v.oldPath = cm.Short, hash, f.OldPath
			v.state.LineSelection = true
			v.state.NoLineNumbers = false
			v.state.OnLineSelection = func(r editor.DiffLineRange) {
				v.lineSel, v.lineSelOn = r, true
			}
			if g.diffs == nil {
				g.diffs = map[string]*gitDiffTab{}
			}
			g.diffs[key] = v
			at := len(g.order)
			if i := indexOf(g.order, commitTab(cm.Hash)); i >= 0 {
				at = i + 1
			}
			if g.preview == commitTab(cm.Hash) {
				g.preview = "" // a commit looked into stays
			}
			g.addTab(key, at)
			a.activateIn(g, key)
		})
	}()
}

// commitFileBar is the bar under a commit's file open as a diff: the
// commit it is of, and the permalink of the lines selected at it.
func (a *App) commitFileBar(c *ui.Context, v *gitDiffTab) {
	p := colors(c)
	ui.Row(c).FillWidth().Height(30).Padding(0, 8).Gap(6).AlignItems(ui.Center).
		BorderWidth(1, 0, 0, 0).BorderColor(p.Separator).Children(func() {
		ui.Text(c, v.rev).FontSize(11).TextColor(p.Muted).SingleLine()
		if v.oldPath != "" {
			ui.Text(c, filepath.ToSlash(v.oldPath)+" → "+filepath.ToSlash(v.name)).FontSize(11).TextColor(p.Muted).SingleLine().Ellipsis("…").Shrink(1)
		}
		ui.Box(c).Grow(1)
		if v.state != nil {
			add, del := v.state.Stats()
			ui.Text(c, fmt.Sprintf("+%d −%d", add, del)).FontSize(11).TextColor(p.Muted)
		}
		a.diffLinkButton(c, v, a.commitFileLinkTarget(v))
	})
}

// fileCommit is the commit f's diff is read from: a stash's untracked
// files are of a commit of their own.
func fileCommit(cm git.Commit, f git.FileStat) string {
	if f.Commit != "" {
		return f.Commit
	}
	return cm.Hash
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// commitContent is a commit's tab: its message, who made it and when,
// and the files it changed.
func (a *App) commitContent(c *ui.Context, g *group, v *commitView) {
	cm := v.commit
	ui.Scroll(c.Key(cm.Hash)).Grow(1).FillWidth().TrackScroll(&v.scroll).Children(func() {
		ui.Column(c).FillWidth().Padding(20, 24).Gap(14).Children(func() {
			a.commitDetailContent(c, g, v, false)
		})
	})
}
