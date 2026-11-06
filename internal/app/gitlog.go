package app

import (
	"fmt"
	"path/filepath"
	"see/locales"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
)

// The Git log, as Fork's and Peek's: the history in a tab of its own,
// with room beside each commit for who made it and when, over the
// commit chosen, whose files show as diffs one after another. It reads
// the navigator's history, a page at a time.

// gitLogTab is the log's tab. It is not kept between launches.
const gitLogTab = diffTabPrefix + "log"

// logFilesAtOnce is how many of a commit's files show as diffs before
// the rest wait for a click, so that a large commit opens quickly.
const logFilesAtOnce = 20

type gitLogView struct {
	query      string
	selected   string // the commit chosen, by hash
	list       ui.ListState
	horizontal ui.ScrollState
	detail     *logCommit
}

// logCommit is the commit the log shows under its rows.
type logCommit struct {
	hash    string
	commit  git.Commit
	body    string
	files   []logFile
	shown   int // files shown as diffs
	loading bool
	err     string
	scroll  ui.ScrollState
}

// logFile is a file of a commit, and its diff unless git has no lines
// of it to show.
type logFile struct {
	path, oldPath  string
	added, deleted int // -1 each for a binary file
	diff           *editor.DiffState
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
	g.diffs[gitLogTab] = &gitDiffTab{name: "Git Log", log: &gitLogView{}}
	g.addTab(gitLogTab, len(g.order))
	a.activateIn(g, gitLogTab)
	a.term.maximized = false
	a.syncHistory()
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
	if v.detail != nil && v.detail.hash == cm.Hash {
		return
	}
	v.selected = cm.Hash
	lc := &logCommit{hash: cm.Hash, commit: cm, loading: true, shown: logFilesAtOnce}
	v.detail = lc
	gen, ctx, root := a.gen, a.wsCtx, a.git.root
	go func() {
		d, err := git.Show(ctx, root, cm.Hash)
		var patches []editor.FilePatch
		if err == nil {
			var out []byte
			if out, err = git.CommitPatch(ctx, root, cm.Hash); err == nil {
				patches, _ = editor.ParsePatch(string(out))
			}
		}
		a.post.Post(func() {
			if a.gen != gen || v.detail != lc {
				return
			}
			lc.loading = false
			if err != nil {
				lc.err = gitErrLine(err)
				return
			}
			lc.body = d.Body
			lc.files = a.logFiles(d.Files, patches)
		})
	}()
}

// logFiles are a commit's files in git's order, each with the diff of
// its patch.
func (a *App) logFiles(stats []git.FileStat, patches []editor.FilePatch) []logFile {
	byName := map[string]editor.FilePatch{}
	for _, p := range patches {
		name := p.NewName
		if name == "" {
			name = p.OldName
		}
		byName[name] = p
	}
	files := make([]logFile, 0, len(stats))
	for _, f := range stats {
		lf := logFile{path: f.Path, oldPath: f.OldPath, added: f.Added, deleted: f.Deleted}
		if p, ok := byName[f.Path]; ok && !p.Binary && len(p.Hunks) > 0 {
			d := &editor.DiffState{Theme: a.edTheme, FitContent: true}
			a.applyDiffPrefs(d)
			d.SetPatch(p)
			lf.diff = d
		}
		files = append(files, lf)
	}
	return files
}

// gitLogContent is the log's tab: a filter over the rows of the
// commits, and the commit chosen under them.
func (a *App) gitLogContent(c *ui.Context, g *group, v *gitLogView) {
	h := &a.git.history
	a.syncHistory()
	var rows []int
	for i, cm := range h.commits {
		if logMatches(cm, v.query) {
			rows = append(rows, i)
		}
	}
	if v.selected == "" && len(rows) > 0 {
		a.chooseLogCommit(v, h.commits[rows[0]])
	}
	width := g.focusHandle.Bounds(c).W
	if width <= 0 {
		width = 900
	}
	columns := logColumns(width, h.lanesW)
	graphVisible := strings.TrimSpace(v.query) == ""
	ui.Column(c).Grow(1).FillWidth().Children(func() {
		ui.Row(c).Shrink(0).FillWidth().Height(36).Padding(0, 12).Gap(8).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Separator).Children(func() {
			ui.TextInput(c, &v.query).Label(locales.MsgFilterCommits()).Placeholder(locales.MsgFilterBySubjectAuthorBranchOrHash()).
				Grow(1).MaxWidth(420).Height(24).Padding(0, 8).FontSize(12)
			count := fmt.Sprintf("%d commit%s", len(rows), plural(len(rows)))
			if !h.done {
				count = fmt.Sprintf("%d of the first %d commits", len(rows), len(h.commits))
				if v.query == "" {
					count = fmt.Sprintf("%d+ commits", len(h.commits))
				}
			}
			ui.Text(c, count).FontSize(11).TextColor(colors(c).Muted).SingleLine()
			ui.Box(c).Grow(1)
			if h.loading {
				ui.Spinner(c).FontSize(10)
			}
			tip := "Show Every Branch"
			if h.all {
				tip = "Show Only " + a.git.branch
			}
			if glyphButton(c, icBranches, "All Branches", tip, h.all, false).Clicked() {
				h.all, h.stale = !h.all, true
				a.syncHistory()
			}
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
						ui.Button(c, logText("gitLogLoadMore")).OnClick(func() { a.loadHistory(false) })
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
	columns := logTableColumns{refs: 144, graph: max(62, graphX(max(lanes, 1)-1)+graphLane), date: 92}
	if width >= 900 {
		columns.author = 140
	}
	if width >= 1100 {
		columns.hash = 72
	}
	if width < 700 {
		columns.refs = 108
	}
	return columns
}

func (columns logTableColumns) minimumWidth() float32 {
	count := 4
	if columns.author > 0 {
		count++
	}
	if columns.hash > 0 {
		count++
	}
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
		if columns.author > 0 {
			label("gitLogAuthor", columns.author)
		}
		label("gitLogDate", columns.date)
		if columns.hash > 0 {
			label("gitLogHash", columns.hash)
		}
	})
}

func (a *App) logRow(c *ui.Context, v *gitLogView, cm git.Commit, columns logTableColumns, graphVisible bool) {
	chosen := v.selected == cm.Hash
	row := ui.Row(c.Key(cm.Hash)).FillWidth().Height(logRowHeight).Padding(0, 12).Gap(12).AlignItems(ui.Center).
		Label(cm.Subject).Tooltip(cm.Short + " " + cm.Subject)
	if chosen {
		row.Background(c.Theme().Accent.Alpha(0.18))
	} else if row.Hovered() {
		row.Background(colors(c).Pill)
	}
	row.OnClick(func() { a.chooseLogCommit(v, cm) })
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("Open in Tab").Chosen() {
			a.openCommit(cm)
		}
		m.Separator()
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
		ui.Row(c).Width(columns.refs).Shrink(0).Gap(4).AlignItems(ui.Center).Tooltip(strings.Join(cm.Refs, "\n")).Children(func() {
			refs := slices.DeleteFunc(slices.Clone(cm.Refs), func(ref string) bool { return ref == "HEAD" })
			for _, ref := range refs[:min(2, len(refs))] {
				refBadge(c.Key(ref), ref, ref == a.git.branch)
			}
			if len(refs) > 2 {
				ui.Text(c, fmt.Sprintf("+%d", len(refs)-2)).FontSize(10).TextColor(colors(c).Muted).Shrink(0)
			}
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
			ui.Text(c, cm.Subject).FontSize(12).SingleLine().Shrink(1)
			if a.git.history.unpushed[cm.Hash] {
				ui.Text(c, "↑").FontSize(11).Bold().TextColor(c.Theme().Accent).Shrink(0).Tooltip(locales.MsgNotPushedYet())
			}
		})
		if columns.author > 0 {
			ui.Text(c, authors(cm)).Width(columns.author).Shrink(0).FontSize(12).TextColor(colors(c).Muted).SingleLine().Tooltip(authors(cm))
		}
		ui.Text(c, shortDate(cm.Date, time.Now())).Width(columns.date).Shrink(0).FontSize(11).TextColor(colors(c).Muted).SingleLine().Tooltip(cm.Date.Format(time.RFC1123))
		if columns.hash > 0 {
			ui.Text(c, cm.Short).Width(columns.hash).Shrink(0).FontSize(11).TextColor(colors(c).Dim).SingleLine()
		}
	})
}

// logCommitContent is the chosen commit: its message, who and when, and
// its files' diffs one after another.
func (a *App) logCommitContent(c *ui.Context, g *group, lc *logCommit) {
	cm := lc.commit
	ui.Scroll(c.Key(cm.Hash)).Grow(1).FillWidth().TrackScroll(&lc.scroll).Label(locales.MsgCommit()).Children(func() {
		ui.Column(c).FillWidth().Padding(16, 20).Gap(12).Children(func() {
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, cm.Subject).FontSize(16).Bold().Selectable().Grow(1).Shrink(1)
				if ui.Button(c, locales.MsgOpenInTab()).FontSize(12).Clicked() {
					a.openCommit(cm)
				}
			})
			ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Box(c).Grow(1).Shrink(1).Basis(240).Children(func() {
					a.personLine(c, git.Person{Name: cm.Author, Email: cm.Email}, "", cm.Date.Format("Monday, January 2, 2006 at 15:04"))
				})
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, cm.Short).FontSize(12).TextColor(colors(c).Muted).Selectable()
					if glyphButton(c, icCopy, "Copy Commit Hash", "Copy Commit Hash", false, false).Clicked() {
						c.Services().WriteClipboard(cm.Hash)
						a.note(locales.MsgCopied()+cm.Short+".", false)
					}
				})
			})
			if body := messageBody(lc.body, cm.Subject); body != "" {
				ui.Text(c, body).FontSize(13).Selectable().FillWidth()
			}
			switch {
			case lc.loading:
				ui.Row(c).Gap(8).Children(func() {
					ui.Spinner(c)
					ui.Text(c, locales.MsgReadingTheCommit()).FontSize(12).TextColor(colors(c).Muted)
				})
			case lc.err != "":
				ui.Text(c, lc.err).FontSize(12).TextColor(colors(c).Err)
			default:
				a.logCommitFiles(c, g, lc)
			}
		})
	})
}

// logCommitFiles are a commit's files, each under a head with its path
// and its lines added and deleted, the first ones as diffs.
func (a *App) logCommitFiles(c *ui.Context, g *group, lc *logCommit) {
	added, deleted := 0, 0
	for _, f := range lc.files {
		added += max(f.added, 0)
		deleted += max(f.deleted, 0)
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.MsgDFileSChanged(len(lc.files))).FontSize(13).Bold()
		ui.Text(c, fmt.Sprintf("+%d", added)).FontSize(12).TextColor(a.gitStatusColor('A'))
		ui.Text(c, fmt.Sprintf("−%d", deleted)).FontSize(12).TextColor(a.gitStatusColor('D'))
	})
	for i, f := range lc.files {
		if i >= lc.shown {
			rest := len(lc.files) - lc.shown
			if ui.Button(c, locales.MsgShowDMoreFileS(rest)).FontSize(12).Clicked() {
				lc.shown += logFilesAtOnce
			}
			return
		}
		ui.Column(c.Key(f.path)).FillWidth().Radius(6).BorderWidth(1).BorderColor(colors(c).Separator).Children(func() {
			head := ui.Row(c).FillWidth().Height(30).Padding(0, 10).Gap(8).AlignItems(ui.Center).Background(colors(c).Pill).
				Cursor(ui.CursorPointer).Label(f.path).Tooltip(locales.MsgOpen2() + f.path + locales.MsgAsADiff())
			head.OnClick(func() {
				a.openCommitFile(g, lc.commit, git.FileStat{Path: f.path, OldPath: f.oldPath, Added: f.added, Deleted: f.deleted})
			})
			head.Children(func() {
				fileIcon(c, filepath.Base(f.path), 14)
				name := filepath.ToSlash(f.path)
				if f.oldPath != "" {
					name = filepath.ToSlash(f.oldPath) + " → " + name
				}
				ui.Text(c, name).FontSize(12).Bold().SingleLine().Shrink(1)
				ui.Box(c).Grow(1)
				if f.added < 0 {
					ui.Text(c, locales.MsgBinary()).FontSize(11).TextColor(colors(c).Muted)
					return
				}
				ui.Text(c, fmt.Sprintf("+%d", f.added)).FontSize(11).TextColor(a.gitStatusColor('A'))
				ui.Text(c, fmt.Sprintf("−%d", f.deleted)).FontSize(11).TextColor(a.gitStatusColor('D'))
			})
			if f.diff == nil {
				what := "No changes to show"
				if f.added < 0 {
					what = "Binary file not shown"
				}
				ui.Text(c, what).FontSize(12).TextColor(colors(c).Muted).Padding(10, 12)
				return
			}
			editor.DiffView(c, f.diff).FillWidth()
		})
	}
}
