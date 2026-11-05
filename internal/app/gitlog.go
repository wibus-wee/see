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
	query    string
	selected string // the commit chosen, by hash
	list     ui.ListState
	detail   *logCommit
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
	if v.selected == "" && len(h.commits) > 0 {
		a.chooseLogCommit(v, h.commits[0])
	}
	ui.Column(c).Grow(1).FillWidth().Children(func() {
		ui.Row(c).Shrink(0).FillWidth().Height(36).Padding(0, 12).Gap(8).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Separator).Children(func() {
			ui.TextInput(c, &v.query).Label("Filter Commits").Placeholder("Filter by subject, author, branch or hash").
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
		if a.git.logSplit <= 0 {
			a.git.logSplit = 40
		}
		// The commits over the one chosen, the bar between them dragged
		// to share the height, as the Git navigator's sections.
		ui.SplitVerticalPercent(c, &a.git.logSplit, func() {
			ui.Column(c).Fill().Children(func() {
				switch {
				case h.err != "":
					ui.Text(c, h.err).FontSize(12).TextColor(colors(c).Err).Padding(10, 14)
				case h.commits == nil && h.loading:
					ui.Row(c).Padding(10, 14).Gap(8).Children(func() {
						ui.Spinner(c)
						ui.Text(c, "Reading history…").FontSize(12).TextColor(colors(c).Muted)
					})
				case len(rows) == 0 && v.query != "" && h.done:
					ui.Text(c, "No commits match “"+v.query+"”").FontSize(12).TextColor(colors(c).Muted).Padding(10, 14)
				case len(h.commits) == 0:
					ui.Text(c, "No Commits").FontSize(12).TextColor(colors(c).Muted).Padding(10, 14)
				default:
					ui.List(c, &v.list, len(rows), func(i int) {
						if i == len(rows)-1 && !h.done && !h.loading {
							a.loadHistory(false)
						}
						a.logRow(c, v, h.commits[rows[i]])
					}).Grow(1).FillWidth().Padding(4, 6).Label("Commits")
				}
			})
		}, func() {
			ui.Column(c).Fill().Children(func() {
				if v.detail == nil {
					ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
						ui.Text(c, "No Commit Chosen").FontSize(16).TextColor(colors(c).Dim)
					})
					return
				}
				a.logCommitContent(c, g, v.detail)
			})
		}).Grow(1).FillWidth()
	})
}

// The log's columns beside the subject, and its rows' height.
const (
	logRowHeight   = 28
	logAuthorWidth = 150
	logDateWidth   = 120
	logHashWidth   = 72
)

// logRow is a commit of the log: its lanes of the graph, its subject
// after the branches and tags at it, who made it, when, and its hash.
func (a *App) logRow(c *ui.Context, v *gitLogView, cm git.Commit) {
	h := &a.git.history
	chosen := v.selected == cm.Hash
	row := ui.Row(c.Key(cm.Hash)).FillWidth().Height(logRowHeight).Padding(0, 8, 0, 0).Gap(10).AlignItems(ui.Center).Radius(5).
		Label(cm.Subject).Tooltip(cm.Short + " " + cm.Subject)
	switch {
	case chosen:
		row.Background(c.Theme().Accent.Alpha(0.2))
	case row.Hovered():
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
		lanes := min(max(h.lanesW, 1), graphMaxLanes)
		ui.Box(c).Size(graphX(lanes-1)+graphLane, logRowHeight).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
			drawGraphRow(p, r, cm)
		})
		ui.Row(c).Grow(1).Shrink(1).Basis(0).Gap(5).AlignItems(ui.Center).Children(func() {
			for _, ref := range cm.Refs {
				if ref != "HEAD" {
					refBadge(c.Key(ref), ref, ref == a.git.branch)
				}
			}
			ui.Text(c, cm.Subject).FontSize(12).SingleLine().Shrink(1)
			if h.unpushed[cm.Hash] {
				ui.Text(c, "↑").FontSize(11).Bold().TextColor(c.Theme().Accent).Shrink(0).Tooltip("Not pushed yet")
			}
		})
		ui.Row(c).Width(logAuthorWidth).Shrink(0).Gap(6).AlignItems(ui.Center).Children(func() {
			a.commitAvatars(c, cm, 16)
			ui.Text(c, authors(cm)).FontSize(12).TextColor(colors(c).Muted).SingleLine().Shrink(1)
		})
		ui.Text(c, logDate(cm.Date, time.Now())).Width(logDateWidth).Shrink(0).FontSize(12).TextColor(colors(c).Muted).SingleLine()
		ui.Text(c, cm.Short).Width(logHashWidth).Shrink(0).FontSize(11).TextColor(colors(c).Dim).SingleLine()
	})
}

// logDate is when, with room for the time too: today's and yesterday's
// by name, this year's without the year.
func logDate(t, now time.Time) string {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch {
	case !t.Before(today):
		return "Today " + t.Format("15:04")
	case !t.Before(today.AddDate(0, 0, -1)):
		return "Yesterday " + t.Format("15:04")
	case t.Year() == now.Year():
		return t.Format("Jan 2 15:04")
	}
	return t.Format("Jan 2, 2006")
}

// logCommitContent is the chosen commit: its message, who and when, and
// its files' diffs one after another.
func (a *App) logCommitContent(c *ui.Context, g *group, lc *logCommit) {
	cm := lc.commit
	ui.Scroll(c.Key(cm.Hash)).Grow(1).FillWidth().TrackScroll(&lc.scroll).Label("Commit").Children(func() {
		ui.Column(c).FillWidth().Padding(16, 20).Gap(12).Children(func() {
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, cm.Subject).FontSize(16).Bold().Selectable().Grow(1).Shrink(1)
				if ui.Button(c, "Open in Tab").FontSize(12).Clicked() {
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
						a.note("Copied "+cm.Short+".", false)
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
					ui.Text(c, "Reading the commit…").FontSize(12).TextColor(colors(c).Muted)
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
		ui.Text(c, fmt.Sprintf("%d file%s changed", len(lc.files), plural(len(lc.files)))).FontSize(13).Bold()
		ui.Text(c, fmt.Sprintf("+%d", added)).FontSize(12).TextColor(a.gitStatusColor('A'))
		ui.Text(c, fmt.Sprintf("−%d", deleted)).FontSize(12).TextColor(a.gitStatusColor('D'))
	})
	for i, f := range lc.files {
		if i >= lc.shown {
			rest := len(lc.files) - lc.shown
			if ui.Button(c, fmt.Sprintf("Show %d More File%s", rest, plural(rest))).FontSize(12).Clicked() {
				lc.shown += logFilesAtOnce
			}
			return
		}
		ui.Column(c.Key(f.path)).FillWidth().Radius(6).BorderWidth(1).BorderColor(colors(c).Separator).Children(func() {
			head := ui.Row(c).FillWidth().Height(30).Padding(0, 10).Gap(8).AlignItems(ui.Center).Background(colors(c).Pill).
				Cursor(ui.CursorPointer).Label(f.path).Tooltip("Open " + f.path + " as a Diff")
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
					ui.Text(c, "Binary").FontSize(11).TextColor(colors(c).Muted)
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
