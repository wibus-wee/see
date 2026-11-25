package app

import (
	"see/locales"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"see/internal/git"
)

// Jumping through the log, and the commit rows' actions: the
// files a commit changed, its comparison with its parent on the host,
// and the date of the history the log opens, as Fork's and Peek's.

// jumpToDate is the log's date field's overlay.
type jumpToDate struct {
	open, loading bool
	query, err    string
}

// showTheWholeHistory opens the log at the repository's history again,
// from a scope of one file's.
func (a *App) openWholeLog() {
	a.openGitLog()
	v := a.focus.diffs[gitLogTab].log
	if v.path == "" {
		return
	}
	v.path, v.pending, v.reading = "", "", false
	v.detail, v.selected = nil, ""
	a.git.history.stale = true
	a.syncHistory()
}

// canShowFileHistory reports whether the focused tab is a file of the
// repository, whose history there is to show.
func (a *App) canShowFileHistory() bool {
	if a.focus == nil || a.git.root == "" {
		return false
	}
	_, ok := a.gitRelPath(a.activePath())
	return ok
}

// showActiveFileHistory opens the log at the active file's history.
func (a *App) showActiveFileHistory() {
	if a.canShowFileHistory() {
		a.showFileHistory(a.activePath())
	}
}

// revealLogRef shows the commit revision names in the log: HEAD, a
// branch's tip.
func (a *App) revealLogRef(revision string) {
	if a.git.root == "" {
		return
	}
	gen, scopeSeq, ctx, root := a.gen, a.git.scopeSeq, a.wsCtx, a.git.root
	go func() {
		hash := git.Resolve(ctx, root, revision)
		a.post.Post(func() {
			if a.gen != gen || a.git.scopeSeq != scopeSeq {
				return
			}
			if hash == "" {
				a.note(logText("gitLogNoRevision")+revision+"”", true)
				return
			}
			a.revealLogCommit(hash)
		})
	}()
}

// openJumpToDate shows the date field of the log's Jump to Date.
func (a *App) openJumpToDate() {
	if a.git.root == "" {
		return
	}
	a.jumpDate = jumpToDate{open: true}
}

// jumpToDatePanel is the date field's overlay, as the command palette's.
func (a *App) jumpToDatePanel(c *ui.Context) {
	j := &a.jumpDate
	if c.Shortcut(0, ui.KeyEscape) {
		j.open = false
		return
	}
	ui.Overlay(c, func() {
		backdrop := ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).AlignItems(ui.Center).Background(ui.RGBA(0, 0, 0, 0.18))
		backdrop.OnClick(func() { j.open = false })
		backdrop.Children(func() {
			panel := ui.Column(c).Margin(90, 0, 0, 0).Width(420).Radius(14).Padding(8).Gap(6).
				Material(popoverGlass()).Border(1, colors(c).PopoverEdge).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.5))
			panel.OnClick(func() {}) // clicks on the panel stay on it
			panel.Children(func() {
				ui.Row(c).Padding(4, 8).Gap(8).AlignItems(ui.Center).Children(func() {
					symbolIcon(c, icSearch, 16, colors(c).Muted)
					in := ui.TextInput(c, &j.query).Label(logText("gitLogJumpToDate")).Placeholder("2006-01-02").
						FontSize(16).Grow(1).AutoFocus().Background(ui.Transparent).BorderWidth(0)
					switch {
					case in.Submitted():
						a.jumpLogToDate()
					case in.Shortcut(0, ui.KeyEscape):
						j.open = false
					}
					if j.loading {
						ui.Spinner(c).FontSize(12)
					}
				})
				if j.err != "" {
					ui.Text(c, j.err).TextColor(colors(c).Err).FontSize(12).Padding(8)
				}
			})
		})
	})
}

// jumpLogToDate finds the newest commit at or before the date typed and
// reveals it in the log.
func (a *App) jumpLogToDate() {
	j := &a.jumpDate
	date := strings.TrimSpace(j.query)
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		j.err = logText("gitLogNotADate")
		return
	}
	if j.loading {
		return
	}
	j.loading, j.err = true, ""
	gen, scopeSeq, ctx, root := a.gen, a.git.scopeSeq, a.wsCtx, a.git.root
	go func() {
		hash, err := git.CommitBefore(ctx, root, date)
		a.post.Post(func() {
			if a.gen != gen || a.git.scopeSeq != scopeSeq {
				return
			}
			j.loading = false
			switch {
			case err != nil:
				j.err = gitErrLine(err)
			case hash == "":
				j.err = logText("gitLogNoCommitOnOrBefore") + date
			default:
				j.open = false
				a.revealLogCommit(hash)
			}
		})
	}()
}

// changedPaths are the paths a commit changed, one a line: the new
// path, and the old one before it when it renamed the file, as
// "old -> new".
func changedPaths(files []git.FileStat) string {
	var lines []string
	for _, f := range files {
		line := f.Path
		if f.OldPath != "" {
			line = f.OldPath + " -> " + line
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// compareLink is the page comparing a commit with its first parent on
// the repository's host, as hosts put it at compare/base...head; "" for
// a host whose comparison is elsewhere, as Bitbucket's.
func (a *App) compareLink(cm git.Commit) string {
	if len(cm.Parents) == 0 || a.git.web == "" {
		return ""
	}
	switch hostName(a.git.web) {
	case "GitHub", "GitLab", "Codeberg":
	default:
		return ""
	}
	return a.git.web + "/compare/" + cm.Parents[0] + "..." + cm.Hash
}

// copyChangedPaths copies a commit's changed paths, reading them when
// the row has them not yet.
func (a *App) copyChangedPaths(cm git.Commit) {
	if a.git.root == "" {
		return
	}
	gen, scopeSeq, ctx, root := a.gen, a.git.scopeSeq, a.wsCtx, a.git.root
	go func() {
		d, err := git.Show(ctx, root, cm.Hash)
		a.post.Post(func() {
			if a.gen != gen || a.git.scopeSeq != scopeSeq {
				return
			}
			if err != nil {
				a.note(locales.MsgGit()+gitErrLine(err), true)
				return
			}
			if len(d.Files) == 0 {
				a.note(locales.MsgTheCommitChangesNoFiles(), false)
				return
			}
			text := changedPaths(d.Files)
			a.services.WriteClipboard(text)
			a.note(locales.MsgCopied()+firstPath(text), false)
		})
	}()
}

// firstPath is a changed-paths text's first line, for the note.
func firstPath(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i] + "…"
	}
	return text
}

// commitRowItems are the items of a commit row's menu, the log's and
// the navigator's: copies, and its pages on the repository's host.
func (a *App) commitRowItems(m *ui.Menu, c *ui.Context, cm git.Commit) {
	if m.Item(locales.MsgCopyChangedFilePaths()).Chosen() {
		a.copyChangedPaths(cm)
	}
	if link := a.commitLink(cm.Hash); link != "" {
		m.Separator()
		if m.Item(locales.Catalog.Format(i18n.Locale(), "editorContextOpenOnHost", map[string]any{"host": hostName(a.git.web)})).Chosen() {
			c.Services().OpenURL(link)
		}
	}
	if link := a.compareLink(cm); link != "" {
		if m.Item(locales.MsgOpenCompareOn() + hostName(a.git.web)).Chosen() {
			c.Services().OpenURL(link)
		}
	}
	m.Separator()
	if m.Item(locales.GitCreateTagAction()).Disabled(a.git.busy).Chosen() {
		a.openCreateTag(cm.Hash, cm.Short+" · "+cm.Subject)
	}
	if m.Item(locales.GitCherryPick()).Disabled(a.git.busy).Chosen() {
		a.cherryPick(cm)
	}
}
