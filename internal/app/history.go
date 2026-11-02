package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
)

// The Git history, as Xcode's source control navigator and Peek: the
// commits of the branch, or of every branch, as a graph in the navigator,
// uncommitted changes on top; a commit opens in an editor with its
// message and files, and a file of it opens as a diff.

const (
	gitModeChanges = iota
	gitModeHistory
)

// historyPage is how many commits are read at a time.
const historyPage = 200

type gitHistory struct {
	commits  []git.Commit
	lanes    []string // the lanes the commits read leave for the next page
	lanesW   int      // the most lanes a row has, for the graph's width
	all      bool     // every branch, not only HEAD's
	head     string   // HEAD when the history was read
	loading  bool
	done     bool // no commits are left to read
	stale    bool // to be read again
	err      string
	seq      uint64
	list     ui.ListState
	unpushed map[string]bool // HEAD's commits the branch it tracks has not
}

// commitView is a commit open in an editor.
type commitView struct {
	commit  git.Commit
	details git.CommitDetails
	loading bool
	err     string
	scroll  ui.ScrollState
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
	seq, gen, ctx, root, all, head := h.seq, a.gen, a.wsCtx, a.git.root, h.all, a.git.head
	go func() {
		commits, err := git.Log(ctx, root, all, skip, historyPage)
		lanes = git.Graph(commits, lanes)
		var unpushed map[string]bool
		if reset {
			unpushed = git.Unpushed(ctx, root)
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
			}
			h.commits = append(h.commits, commits...)
			h.lanes, h.head, h.done = lanes, head, len(commits) < historyPage
			for _, c := range commits {
				h.lanesW = max(h.lanesW, len(c.Before), len(c.After), c.Lane+1)
			}
		})
	}()
}

// gitHeading is the Git navigator's title line, as the project
// navigator's: its two scopes as words, the chosen one in the text's
// color, as Xcode's source control navigator, and the actions on the
// repository at its end.
func (a *App) gitHeading(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(4, 12, 4, sidebarInset+2).Gap(2).AlignItems(ui.Center).Children(func() {
		for mode, label := range []string{"Changes", "History"} {
			a.scopeTab(c, mode, label)
		}
		ui.Box(c).Grow(1)
		if a.git.syncing != "" || a.git.history.loading && a.git.mode == gitModeHistory {
			ui.Spinner(c).FontSize(10).Tooltip(a.git.syncing)
		}
		if a.git.mode == gitModeHistory {
			h := &a.git.history
			tip := "Show Every Branch"
			if h.all {
				tip = "Show Only " + a.git.branch
			}
			if glyphButton(c, icBranches, "All Branches", tip, h.all, false).Clicked() {
				h.all, h.stale = !h.all, true
				a.syncHistory()
			}
		}
		a.syncButtons(c)
		tip := "Refresh Status"
		if a.hasRemote() {
			tip = "Fetch and Refresh Status"
		}
		if glyphButton(c, icRefresh, "Refresh", tip, false, a.git.busy).Clicked() {
			a.fetchAndRefresh()
		}
	})
}

// scopeTab is one of the Git navigator's scopes.
func (a *App) scopeTab(c *ui.Context, mode int, label string) {
	chosen := a.git.mode == mode
	b := ui.ButtonBase(c.Key(label)).Label(label).Padding(2, 6).Radius(5).KeepFocus()
	if b.Hovered() && !chosen {
		b.Background(colors(c).Pill)
	}
	b.OnClick(func() {
		a.git.mode = mode
		if mode == gitModeHistory {
			a.syncHistory()
		}
	})
	b.Children(func() {
		t := ui.Text(c, label).FontSize(12).Bold().SingleLine()
		if !chosen {
			t.TextColor(colors(c).Dim)
		}
	})
}

// historyNavigator is the history in the navigator.
func (a *App) historyNavigator(c *ui.Context) {
	h := &a.git.history
	a.syncHistory()
	if n := len(a.git.changes); n > 0 {
		a.uncommittedRow(c, n)
	}
	switch {
	case h.err != "":
		ui.Text(c, h.err).FontSize(12).TextColor(colors(c).Err).Padding(10, 14)
	case h.commits == nil && h.loading:
		ui.Row(c).Padding(8, 14).Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "Reading history…").FontSize(12).TextColor(colors(c).Muted)
		})
	case len(h.commits) == 0 && a.git.head == "":
		ui.Text(c, "No Commits").FontSize(12).TextColor(colors(c).Muted).Padding(10, 14)
	default:
		ui.List(c, &h.list, len(h.commits), func(i int) { a.historyRow(c, i) }).Grow(1).FillWidth().Padding(0, sidebarInset).Label("History")
	}
}

// uncommittedRow stands for the working tree's changes over the history,
// as Peek's: an orange disc in place of an avatar; it leads to them.
func (a *App) uncommittedRow(c *ui.Context, n int) {
	row := ui.Row(c).FillWidth().Height(historyRowHeight).Margin(0, sidebarInset).Padding(0, 8, 0, 0).Gap(historyGap).AlignItems(ui.Center).Radius(6).
		Cursor(ui.CursorPointer).Label("Uncommitted Changes")
	if row.Hovered() {
		row.Background(colors(c).Pill)
	}
	row.OnClick(func() { a.git.mode = gitModeChanges })
	orange := a.gitStatusColor('M')
	if a.edTheme != nil {
		orange = ui.RGB(0xff, 0x9f, 0x0a)
	}
	row.Children(func() {
		lanes := min(max(a.git.history.lanesW, 1), graphMaxLanes)
		ui.Box(c).Size(graphX(lanes-1)+graphLane, historyRowHeight).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
			x, y := r.X+graphX(0), r.Y+r.H/2
			var path ui.Path
			path.MoveTo(x, y+4).LineTo(x, r.Y+r.H).Dash(0, 2, 2)
			p.StrokePath(&path, 1.5, orange)
			var dot ui.Path
			dot.Circle(x, y, 4)
			p.StrokePath(&dot, 1.5, orange)
		})
		ui.Box(c).Size(avatarSize, avatarSize).Shrink(0).Radius(avatarSize / 2).Center().Background(orange.Alpha(0.16)).Children(func() {
			symbolIcon(c, icPage, 11, orange)
		})
		ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
			ui.Text(c, "Uncommitted Changes").FontSize(12).Bold().SingleLine()
			ui.Text(c, fmt.Sprintf("%d file%s", n, plural(n))).FontSize(11).TextColor(colors(c).Muted).SingleLine()
		})
	})
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
	unpushed := h.unpushed[cm.Hash]
	tip := cm.Short + " " + cm.Subject
	if unpushed {
		tip += "\n\nNot pushed yet"
	}
	row := ui.Row(c.Key(cm.Hash)).FillWidth().Height(historyRowHeight).Padding(0, 8, 0, 0).Gap(historyGap).AlignItems(ui.Center).Radius(6).
		Label(cm.Subject).Tooltip(tip)
	// A light selection, for the graph to show through.
	switch {
	case open:
		row.Background(c.Theme().Accent.Alpha(0.2))
	case row.Hovered():
		row.Background(colors(c).Pill)
	}
	row.OnClick(func() { a.openCommit(cm) })
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("Copy Commit Hash").Chosen() {
			c.Services().WriteClipboard(cm.Hash)
		}
		if m.Item("Copy Subject").Chosen() {
			c.Services().WriteClipboard(cm.Subject)
		}
		if link := a.commitLink(cm.Hash); link != "" {
			m.Separator()
			if m.Item("Open on " + hostName(a.git.web)).Chosen() {
				c.Services().OpenURL(link)
			}
		}
	})
	row.Children(func() {
		lanes := min(max(h.lanesW, 1), graphMaxLanes)
		ui.Box(c).Size(graphX(lanes-1)+graphLane, historyRowHeight).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
			drawGraphRow(p, r, cm)
		})
		a.commitAvatars(c, cm, avatarSize)
		ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
			ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, cm.Subject).FontSize(12).Bold().SingleLine().Shrink(1)
				if unpushed {
					ui.Text(c, "↑").FontSize(11).Bold().TextColor(c.Theme().Accent).Shrink(0)
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
	for _, ref := range cm.Refs {
		if ref != "HEAD" && ref != a.git.branch {
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
		return cm.Author + " and " + cm.CoAuthors[0].Name
	}
	n := len(cm.CoAuthors)
	return fmt.Sprintf("%s and %d other%s", cm.Author, n, plural(n))
}

// shortDate is when, as short as a list has room for, as Peek's and
// Finder's: the time today, Yesterday, the weekday this week, else the
// date.
func shortDate(t, now time.Time) string {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch {
	case !t.Before(today):
		return t.Format("15:04")
	case !t.Before(today.AddDate(0, 0, -1)):
		return "Yesterday"
	case !t.Before(today.AddDate(0, 0, -6)):
		return t.Format("Monday")
	case t.Year() == now.Year():
		return t.Format("Jan 2")
	}
	return t.Format("2006/1/2")
}

// refBadge is a branch or tag pointing at a commit.
func refBadge(c *ui.Context, name string, current bool) {
	bg, fg := colors(c).Pill, colors(c).Muted
	if current {
		bg, fg = c.Theme().Accent.Alpha(0.18), c.Theme().Accent
	}
	ui.Text(c, name).FontSize(10).Bold().TextColor(fg).Background(bg).Padding(0, 5).Radius(4).SingleLine().Shrink(2)
}

// drawGraphRow draws a commit's row of the graph: the lanes passing by,
// those that end in its dot, and those leaving it for its parents.
func drawGraphRow(p *ui.Painter, r ui.Rect, cm git.Commit) {
	mid := r.Y + r.H/2
	x := func(lane int) float32 { return r.X + graphX(min(lane, graphMaxLanes-1)) }
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
	v := &commitView{commit: cm, loading: true}
	g.diffs[key] = &gitDiffTab{name: cm.Subject, rev: cm.Short, hash: cm.Hash, commit: v}
	// One commit at a time takes the preview tab, as files do.
	if old := g.preview; old != "" && old != key && strings.HasPrefix(old, diffTabPrefix+"commit:") {
		a.closeTab(g, old)
	}
	g.addTab(key, len(g.order))
	g.preview = key
	a.activateIn(g, key)
	a.term.maximized = false
	gen, ctx, root := a.gen, a.wsCtx, a.git.root
	go func() {
		d, err := git.Show(ctx, root, cm.Hash)
		a.post.Post(func() {
			if a.gen != gen {
				return
			}
			v.loading, v.details = false, d
			if err != nil {
				v.err = gitErrLine(err)
			}
		})
	}()
}

// openCommitFile shows the diff of a file of a commit beside the others.
func (a *App) openCommitFile(g *group, cm git.Commit, f git.FileStat) {
	key := commitFileTab(cm.Hash, f.Path)
	if g.has(key) && g.diffs[key] != nil {
		a.activateIn(g, key)
		return
	}
	a.git.diffSeq++
	seq, gen, ctx, root := a.git.diffSeq, a.gen, a.wsCtx, a.git.root
	go func() {
		out, err := git.CommitFileDiff(ctx, root, cm.Hash, f)
		patches, parseErr := editor.ParsePatch(string(out))
		a.post.Post(func() {
			if a.gen != gen || a.git.diffSeq != seq || a.layout.find(g) == nil {
				return
			}
			switch {
			case err != nil:
				a.note("Git diff: "+gitErrLine(err), true)
				return
			case parseErr != nil || len(patches) == 0 || patches[0].Binary:
				a.note("No text diff available for "+f.Path, true)
				return
			}
			v := a.newGitDiffTab(patches[0], f.Path, false)
			v.rev, v.hash, v.oldPath = cm.Short, cm.Hash, f.OldPath
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
	cm, d := v.commit, v.details
	ui.Scroll(c.Key(cm.Hash)).Grow(1).FillWidth().TrackScroll(&v.scroll).Children(func() {
		ui.Column(c).FillWidth().Padding(20, 24).Gap(14).Children(func() {
			ui.Text(c, cm.Subject).FontSize(18).Bold().Selectable()
			when := cm.Date.Format("Monday, January 2, 2006 at 15:04")
			if d.Committer != "" && d.Committer != cm.Author {
				when += " · committed by " + d.Committer
			}
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				a.personLine(c, git.Person{Name: cm.Author, Email: cm.Email}, "", when)
				for _, p := range cm.CoAuthors {
					a.personLine(c.Key(p.Email), p, "Co-author", p.Email)
				}
			})
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, cm.Hash).FontSize(12).TextColor(colors(c).Muted).Selectable()
				if glyphButton(c, icCopy, "Copy Commit Hash", "Copy Commit Hash", false, false).Clicked() {
					c.Services().WriteClipboard(cm.Hash)
					a.note("Copied "+cm.Short+".", false)
				}
				for _, ref := range cm.Refs {
					if ref != "HEAD" {
						refBadge(c, ref, ref == a.git.branch)
					}
				}
				if link := a.commitLink(cm.Hash); link != "" {
					ui.Box(c).Grow(1)
					host := hostName(a.git.web)
					if ui.Button(c, "Open on "+host).FontSize(12).Tooltip(link).Clicked() {
						c.Services().OpenURL(link)
					}
				}
			})
			if len(cm.Parents) > 0 {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					label := "Parent"
					if len(cm.Parents) > 1 {
						label = "Parents"
					}
					ui.Text(c, label).FontSize(12).TextColor(colors(c).Muted)
					for _, parent := range cm.Parents {
						short := parent[:min(len(parent), len(cm.Short))]
						link := ui.ButtonBase(c.Key(parent)).Label("Open parent " + short).Cursor(ui.CursorPointer)
						link.Children(func() { ui.Text(c, short).FontSize(12).TextColor(c.Theme().Accent) })
						if link.Clicked() {
							if pc, ok := a.historyCommit(parent); ok {
								a.openCommit(pc)
							} else {
								a.note("That commit is further back than the history read so far.", false)
							}
						}
					}
				})
			}
			if body := messageBody(d.Body, cm.Subject); body != "" {
				ui.Text(c, body).FontSize(13).Selectable().FillWidth()
			}
			ui.Box(c).Height(1).FillWidth().Background(colors(c).Separator)
			switch {
			case v.loading:
				ui.Row(c).Gap(8).Children(func() {
					ui.Spinner(c)
					ui.Text(c, "Reading the commit…").FontSize(12).TextColor(colors(c).Muted)
				})
			case v.err != "":
				ui.Text(c, v.err).FontSize(12).TextColor(colors(c).Err)
			default:
				a.commitFiles(c, g, v)
			}
		})
	})
}

// commitFiles lists a commit's files, with the lines each added and
// deleted; a click opens the file's diff.
func (a *App) commitFiles(c *ui.Context, g *group, v *commitView) {
	files := v.details.Files
	added, deleted := 0, 0
	for _, f := range files {
		added += max(f.Added, 0)
		deleted += max(f.Deleted, 0)
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf("%d file%s changed", len(files), plural(len(files)))).FontSize(13).Bold()
		ui.Text(c, fmt.Sprintf("+%d", added)).FontSize(12).TextColor(a.gitStatusColor('A'))
		ui.Text(c, fmt.Sprintf("−%d", deleted)).FontSize(12).TextColor(a.gitStatusColor('D'))
	})
	ui.Column(c).FillWidth().Gap(1).Children(func() {
		for _, f := range files {
			key := commitFileTab(v.commit.Hash, f.Path)
			row := ui.Row(c.Key(f.Path)).FillWidth().Padding(5, 8).Gap(8).Radius(6).AlignItems(ui.Center).
				Cursor(ui.CursorPointer).Label(f.Path).Tooltip(f.Path)
			if g.has(key) {
				row.Background(colors(c).SelectedRow)
			} else if row.Hovered() {
				row.Background(colors(c).Pill)
			}
			row.OnClick(func() { a.openCommitFile(g, v.commit, f) })
			row.Children(func() {
				fileIcon(c, filepath.Base(f.Path), 15)
				ui.Text(c, filepath.Base(f.Path)).FontSize(12).SingleLine().Shrink(0)
				dir := filepath.Dir(f.Path)
				if f.OldPath != "" {
					dir = f.OldPath + " →"
				}
				if dir != "." {
					ui.Text(c, filepath.ToSlash(dir)).FontSize(11).TextColor(colors(c).Dim).SingleLine().Shrink(1)
				}
				ui.Box(c).Grow(1)
				if f.Added < 0 {
					ui.Text(c, "Binary").FontSize(11).TextColor(colors(c).Muted)
					return
				}
				ui.Text(c, fmt.Sprintf("+%d", f.Added)).FontSize(11).TextColor(a.gitStatusColor('A'))
				ui.Text(c, fmt.Sprintf("−%d", f.Deleted)).FontSize(11).TextColor(a.gitStatusColor('D'))
				statBar(c, f.Added, f.Deleted, a.gitStatusColor('A'), a.gitStatusColor('D'), colors(c).Pill)
			})
		}
	})
}

// statBar is GitHub's five squares: how much of a file's change was
// added and how much deleted.
func statBar(c *ui.Context, added, deleted int, add, del, none ui.Color) {
	const n = 5
	total := added + deleted
	greens := 0
	if total > 0 {
		filled := min(n, max(1, total*n/max(total, 20)))
		greens = (added*filled + total/2) / total
		ui.Row(c).Gap(1).Shrink(0).Children(func() {
			for i := range n {
				color := none
				switch {
				case i < greens:
					color = add
				case i < filled:
					color = del
				}
				ui.Box(c).Size(7, 7).Radius(1.5).Background(color)
			}
		})
	}
}

// historyCommit is a commit of the history read, by hash.
func (a *App) historyCommit(hash string) (git.Commit, bool) {
	for _, cm := range a.git.history.commits {
		if cm.Hash == hash {
			return cm, true
		}
	}
	return git.Commit{}, false
}

// messageBody is a commit message without its subject, nor the
// Co-authored-by trailers its people show above.
func messageBody(message, subject string) string {
	var keep []string
	for line := range strings.SplitSeq(strings.TrimPrefix(message, subject), "\n") {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "co-authored-by:") {
			keep = append(keep, line)
		}
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}
