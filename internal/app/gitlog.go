package app

import (
	"fmt"
	"path/filepath"
	"see/locales"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"see/internal/git"
)

// The Git log, as Fork's and Peek's: the history in a tab of its own,
// with room beside each commit for who made it and when, over the
// commit chosen, whose files show as diffs one after another. It reads
// the navigator's history, a page at a time.

// gitLogTab is the log's tab. It is not kept between launches.
const gitLogTab = diffTabPrefix + "log"

type gitLogView struct {
	pending    string
	reading    bool
	query      string
	path       string // the file its history is of, or "" for the repository's
	selected   string // the commit chosen, by hash
	list       ui.ListState
	horizontal ui.ScrollState
	detail     *commitView
}

// openGitLog shows the log in the focused group, where it is already
// open if it is.
func (a *App) openGitLog() {
	for _, g := range a.groups() {
		if g.has(gitLogTab) && g.diffs[gitLogTab] != nil {
			a.activateIn(g, gitLogTab)
			return
		}
	}
	g := a.focus
	if g.diffs == nil {
		g.diffs = map[string]*gitDiffTab{}
	}
	g.diffs[gitLogTab] = &gitDiffTab{name: locales.GitLogTitle(), log: &gitLogView{}}
	g.addTab(gitLogTab, len(g.order))
	a.activateIn(g, gitLogTab)
	a.term.maximized = false
	a.syncHistory()
}

// openFileLog shows the log scoped to the commits that changed rel, a
// path in the repository, in the tab the log already has when it does.
func (a *App) openFileLog(rel string) {
	if rel == "" {
		a.openGitLog()
		return
	}
	a.openGitLog()
	v := a.focus.diffs[gitLogTab].log
	if v.path == rel {
		return
	}
	v.path, v.pending, v.reading = rel, "", false
	v.detail, v.selected = nil, ""
	a.git.history.stale = true
	a.syncHistory()
}

// showFileHistory opens the log at the history of path, a file of the
// workspace, when it is the repository's.
func (a *App) showFileHistory(path string) {
	rel, ok := a.gitRelPath(path)
	if !ok {
		a.fail(locales.MsgNoHistoryFor() + a.workspacePath(path) + locales.MsgNotInTheRepository())
		return
	}
	a.openFileLog(rel)
}

// logMatches reports whether a commit has every word of q in its
// subject, its people, its branches or tags, or its hash from the start.
func logMatches(cm git.Commit, q string) bool {
	words := strings.Fields(strings.ToLower(q))
	if len(words) == 0 {
		return true
	}
	parts := []string{cm.Subject, cm.Author, cm.Email}
	for _, p := range cm.CoAuthors {
		parts = append(parts, p.Name, p.Email)
	}
	parts = append(parts, cm.Refs...)
	text := strings.ToLower(strings.Join(parts, "\x00"))
	for _, w := range words {
		if !strings.Contains(text, w) && !strings.HasPrefix(cm.Hash, w) {
			return false
		}
	}
	return true
}

// chooseLogCommit shows a commit under the log's rows, reading its
// message and its patch.
func (a *App) chooseLogCommit(v *gitLogView, cm git.Commit) {
	v.pending = ""
	if v.detail != nil && v.detail.commit.Hash == cm.Hash {
		return
	}
	v.selected = cm.Hash
	lc := newCommitView(cm)
	v.detail = lc
	a.loadCommitViewWhen(lc, func() bool { return v.detail == lc })
}

// gitLogContent is the log's tab: a filter over the rows of the
// commits, and the commit chosen under them. It reads the history of
// the file it is scoped to when it is.
func (a *App) gitLogContent(c *ui.Context, g *group, v *gitLogView) {
	h := &a.git.history
	if h.path != v.path && !h.loading {
		h.path, h.stale = v.path, true
	}
	a.syncHistory()
	a.resolveLogTarget(v)
	var rows []int
	for i, cm := range h.commits {
		if logMatches(cm, v.query) {
			rows = append(rows, i)
		}
	}
	if v.selected == "" && v.pending == "" && len(rows) > 0 {
		a.chooseLogCommit(v, h.commits[rows[0]])
	}
	width := g.focusHandle.Bounds(c).W
	if width <= 0 {
		width = 900
	}
	columns := logColumns(width, h.lanesW)
	columns.refs = max(columns.refs, logRefsWidth(c, a.git.branch, h.commits, rows))
	columns.author = max(logLabelWidth(c, "gitLogAuthor"), logAuthorWidth(c, h.commits, rows))
	columns.date = max(logLabelWidth(c, "gitLogDate"), logDateWidth(c, h.commits, rows))
	columns.hash = max(logLabelWidth(c, "gitLogHash"), logHashWidth(c, h.commits, rows))
	graphVisible := strings.TrimSpace(v.query) == ""
	ui.Column(c).Grow(1).FillWidth().Children(func() {
		ui.Row(c).Shrink(0).FillWidth().Height(36).Padding(0, 12).Gap(8).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Separator).Children(func() {
			if v.path != "" {
				chip := ui.Row(c).Gap(4).AlignItems(ui.Center).Padding(3, 4, 3, 7).Radius(5).Background(colors(c).Pill).Shrink(0).
					Label(logText("gitLogFileScope") + " " + v.path)
				chip.Children(func() {
					fileIcon(c, filepath.Base(v.path), 13)
					ui.Text(c, v.path).FontSize(11).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1).MaxWidth(200)
					cl := ui.ButtonBase(c).Label(locales.MsgShowTheWholeHistory()).Tooltip(locales.MsgShowTheWholeHistory()).Size(14, 14).Radius(3).Center()
					if cl.Hovered() {
						cl.Background(colors(c).Separator)
					}
					cl.OnClick(func() { a.openWholeLog() })
					cl.Children(func() { symbolIcon(c, icClose, 9, colors(c).Muted) })
				})
			}
			ui.TextInput(c, &v.query).Label(locales.MsgFilterCommits()).Placeholder(locales.MsgFilterBySubjectAuthorBranchOrHash()).
				Grow(1).MaxWidth(420).Height(24).Padding(0, 8).FontSize(12)
			count := locales.MsgDCommitS(len(rows))
			if !h.done {
				count = fmt.Sprintf(locales.MsgDOfTheFirstDCommits(), len(rows), len(h.commits))
				if v.query == "" {
					count = fmt.Sprintf(locales.MsgDPlusCommits(), len(h.commits))
					// rev-list counts the whole walk ahead: say how many
					// there are, not how many are read so far.
					if h.total > len(h.commits) {
						count = fmt.Sprintf(locales.MsgDOfDCommits(), len(h.commits), h.total)
					}
				}
			}
			ui.Text(c, count).FontSize(11).TextColor(colors(c).Muted).SingleLine()
			ui.Box(c).Grow(1)
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
			jump := ui.ButtonBase(c.Key("log-jump")).Label(logText("gitLogJump")).Tooltip(logText("gitLogJump")).Padding(4, 6).Radius(5).KeepFocus()
			if jump.Hovered() || jump.Pressed() {
				jump.Background(colors(c).Pill)
			}
			jump.Children(func() {
				ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
					symbolIcon(c, icCommitArrow, 12, colors(c).Muted)
					symbolIcon(c, icChevron, 8, colors(c).Dim)
				})
			})
			jump.Menu(func(m *ui.Menu) {
				if m.Item(logText("gitLogRevealHEAD")).Chosen() {
					a.revealLogRef("HEAD")
				}
				if m.Item(logText("gitLogJumpToDate") + "…").Chosen() {
					a.openJumpToDate()
				}
			})
		})
		if !graphVisible {
			ui.Text(c, logText("gitLogFilteredGraph")).FontSize(11).TextColor(colors(c).Muted).Padding(4, 12).FillWidth()
		}
		if a.git.logSplit <= 0 {
			a.git.logSplit = 40
		}
		// The commits over the one chosen, the bar between them dragged
		// to share the height, as the Git navigator's sections.
		ui.SplitVerticalPercent(c, &a.git.logSplit, func() {
			ui.ScrollHorizontal(c).Fill().TrackScroll(&v.horizontal).Label(logText("gitLogTable")).Children(func() {
				ui.Column(c).Width(max(width, columns.minimumWidth())).Shrink(0).FillHeight().Children(func() {
					logTableHeader(c, columns)
					switch {
					case h.err != "":
						ui.Text(c, h.err).FontSize(12).TextColor(colors(c).Err).Padding(10, 14)
					case h.commits == nil && h.loading:
						ui.Row(c).Padding(10, 14).Gap(8).Children(func() {
							ui.Spinner(c)
							ui.Text(c, locales.MsgReadingHistory()).FontSize(12).TextColor(colors(c).Muted)
						})
					case len(rows) == 0 && v.query != "" && h.done:
						ui.Text(c, locales.MsgNoCommitsMatch()+v.query+"”").FontSize(12).TextColor(colors(c).Muted).Padding(10, 14)
					case len(h.commits) == 0:
						ui.Text(c, locales.MsgNoCommits()).FontSize(12).TextColor(colors(c).Muted).Padding(10, 14)
					default:
						ui.List(c, &v.list, len(rows)).ItemKey(func(i int) any { return h.commits[rows[i]].Hash }).Grow(1).FillWidth().Label(locales.MsgCommits()).Rows(func(item ui.ListRow) {
							i := item.Index
							if i == len(rows)-1 && !h.done && !h.loading {
								a.loadHistory(false)
							}
							a.logRow(item.Context, v, h.commits[rows[i]], columns, graphVisible)
						})
					}
					if !h.done && !h.loading {
						ui.Row(c).Shrink(0).FillWidth().Padding(6, 12).BorderWidth(1, 0, 0, 0).BorderColor(colors(c).Separator).Children(func() {
							if ui.Button(c, logText("gitLogLoadMore")).FontSize(12).Clicked() {
								a.loadHistory(false)
							}
						})
					}
				})
			})
		}, func() {
			ui.Column(c).Fill().Children(func() {
				if v.detail == nil {
					ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
						ui.Text(c, locales.MsgNoCommitChosen()).FontSize(16).TextColor(colors(c).Dim)
					})
					return
				}
				a.logCommitContent(c, g, v.detail)
			})
		}).Grow(1).FillWidth()
	})
}

const logRowHeight = 34

type logTableColumns struct{ refs, graph, author, date, hash float32 }

func logColumns(width float32, lanes int) logTableColumns {
	columns := logTableColumns{refs: 144, graph: max(62, graphX(max(lanes, 1)-1)+graphLane)}
	if width < 700 {
		columns.refs = 108
	}
	return columns
}

// logTextWidth is the room text takes at size, with a pixel to spare.
func logTextWidth(c *ui.Context, text string, size float32) float32 {
	w, _ := c.MeasureText(0, ui.Span{Text: text, Size: size})
	return w + 2
}

// logLabelWidth is the room a column header needs at its size.
func logLabelWidth(c *ui.Context, key string) float32 {
	return logTextWidth(c, logText(key), 11)
}

// logRefsWidth is the room the refs column needs to show the badges of
// the rows whole: the widest set of branches and tags one commit has, at
// the size and padding refBadge draws them. The column takes it as its
// width, so a long branch name is not middle-truncated.
func logRefsWidth(c *ui.Context, branch string, commits []git.Commit, rows []int) float32 {
	var width float32
	for _, i := range rows {
		cm := commits[i]
		if len(cm.Refs) == 0 {
			continue
		}
		shown, _ := visibleCommitRefs(cm, branch)
		var w float32
		for j, ref := range shown {
			textW, _ := c.MeasureText(0, ui.Span{Text: ref, Size: 10, Weight: 700})
			w += textW + 10 // the badge's padding, 5 each side
			if j > 0 {
				w += 4 // the gap between badges
			}
		}
		width = max(width, w)
	}
	return width
}

// logAuthorWidth is the room the author column needs for the widest
// byline of the rows, at the size logRow draws it.
func logAuthorWidth(c *ui.Context, commits []git.Commit, rows []int) float32 {
	var width float32
	for _, i := range rows {
		width = max(width, logTextWidth(c, authors(commits[i]), 12))
	}
	return width
}

// logDate is when a commit was made, to the second: the log's date
// column has room for the whole stamp, unlike the navigator's byline.
func logDate(t time.Time) string { return t.Format("2006-01-02 15:04:05") }

// logDateWidth is the room the date column needs: a stand-in stamp of
// the widest digits keeps the column steady as the clock ticks, and the
// rows widen it when the font draws their own stamps wider.
func logDateWidth(c *ui.Context, commits []git.Commit, rows []int) float32 {
	width := logTextWidth(c, "8888-88-88 88:88:88", 11)
	for _, i := range rows {
		width = max(width, logTextWidth(c, logDate(commits[i].Date), 11))
	}
	return width + 4
}

// logHashWidth is the room the hash column needs for a commit's short
// hash.
func logHashWidth(c *ui.Context, commits []git.Commit, rows []int) float32 {
	var width float32
	for _, i := range rows {
		width = max(width, logTextWidth(c, commits[i].Short, 11))
	}
	return width
}

func (columns logTableColumns) minimumWidth() float32 {
	const count = 6
	return columns.refs + columns.graph + columns.author + columns.date + columns.hash + 160 + 24 + float32(count-1)*12
}

func logText(key string) string { return locales.Catalog.Format(i18n.Locale(), key, nil) }

func logTableHeader(c *ui.Context, columns logTableColumns) {
	ui.Row(c).FillWidth().Height(26).Padding(0, 12).Gap(12).AlignItems(ui.Center).
		Background(colors(c).TabBar).BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Separator).Children(func() {
		label := func(key string, width float32) {
			ui.Text(c, logText(key)).Width(width).Shrink(0).FontSize(11).TextColor(colors(c).Muted).SingleLine()
		}
		label("gitLogRefs", columns.refs)
		label("gitLogGraph", columns.graph)
		ui.Text(c, logText("gitLogMessage")).Grow(1).Basis(0).FontSize(11).TextColor(colors(c).Muted).SingleLine()
		label("gitLogAuthor", columns.author)
		label("gitLogDate", columns.date)
		label("gitLogHash", columns.hash)
	})
}

func (a *App) logRow(c *ui.Context, v *gitLogView, cm git.Commit, columns logTableColumns, graphVisible bool) {
	chosen := v.selected == cm.Hash
	row := ui.Row(c.Key(cm.Hash)).FillWidth().Height(logRowHeight).Padding(0, 12).Gap(12).AlignItems(ui.Center).
		Label(cm.Subject)
	if chosen {
		row.Background(c.Theme().Accent.Alpha(0.18))
	} else if row.Hovered() {
		row.Background(colors(c).Pill)
	}
	row.OnClick(func() { a.chooseLogCommit(v, cm) })
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.MsgOpenInTab()).Chosen() {
			a.openCommit(cm)
		}
		m.Separator()
		if m.Item(locales.MsgCopyCommitHash()).Chosen() {
			c.Services().WriteClipboard(cm.Hash)
		}
		if m.Item(locales.MsgCopySubject()).Chosen() {
			c.Services().WriteClipboard(cm.Subject)
		}
		a.commitRowItems(m, c, cm)
	})

	row.Children(func() {
		ui.Row(c).Width(columns.refs).Shrink(0).Gap(4).AlignItems(ui.Center).Tooltip(strings.Join(cm.Refs, "\n")).Children(func() {
			a.commitRefs(c, cm)
		})
		graph := ui.Box(c).Size(columns.graph, logRowHeight).Shrink(0)
		if graphVisible {
			graph.Label(logText("gitLogGraph") + " " + cm.Short).Draw(func(p *ui.Painter, r ui.Rect) { drawGraphRowLanes(p, r, cm, max(a.git.history.lanesW, 1)) })
			// Decorated tips carry author identity; ordinary commits keep compact dots.
			if len(cm.Refs) > 0 {
				graph.Children(func() {
					ui.Box(c).Size(20, 20).Absolute().Left(graphX(cm.Lane)-10).Top((logRowHeight-20)/2).
						Radius(10).Border(1.5, laneColor(cm.Lane)).Background(colors(c).Editor).Center().Children(func() { a.personAvatar(c, cm.Author, cm.Email, 16) })
				})
			}
		}
		ui.Row(c).Grow(1).Shrink(1).Basis(0).Gap(5).AlignItems(ui.Center).Children(func() {
			commitSubject(c, cm.Subject, commitSubjectRow)
			h := &a.git.history
			mainline := h.mainline == nil || h.mainline[cm.Hash]
			if mainline && h.unpushed[cm.Hash] {
				ui.Text(c, "↑").FontSize(11).Bold().TextColor(c.Theme().Accent).Shrink(0).Tooltip(locales.MsgNotPushedYet())
			}
			if mainline && h.incoming[cm.Hash] {
				ui.Text(c, "↓").FontSize(11).Bold().TextColor(c.Theme().Accent).Shrink(0).Tooltip(logText("gitIncomingCommit"))
			}
		})
		ui.Text(c, authors(cm)).Width(columns.author).Shrink(0).FontSize(12).TextColor(colors(c).Muted).SingleLine()
		ui.Text(c, logDate(cm.Date)).Width(columns.date).Shrink(0).FontSize(11).TextColor(colors(c).Muted).SingleLine()
		ui.Text(c, cm.Short).Width(columns.hash).Shrink(0).FontSize(11).TextColor(colors(c).Dim).SingleLine()
	})
}

// logCommitContent is the chosen commit: its message, who and when, and
// its files' diffs one after another.
func (a *App) logCommitContent(c *ui.Context, g *group, lc *commitView) {
	cm := lc.commit
	ui.Scroll(c.Key(cm.Hash)).Grow(1).FillWidth().TrackScroll(&lc.scroll).Label(locales.MsgCommit()).Children(func() {
		ui.Column(c).FillWidth().Padding(16, 20).Gap(12).Children(func() {
			a.commitDetailContent(c, g, lc, true)
		})
	})
}
