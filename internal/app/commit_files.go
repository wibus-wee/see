package app

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
	"see/locales"
)

// commitFilesAtOnce is how many of a commit's files show as diffs
// before the rest wait for a click, so that a large commit opens
// quickly.
const commitFilesAtOnce = 20

// commitFile is a file of a commit, and its diff unless git has no
// lines of it to show.
type commitFile struct {
	path, oldPath  string
	commit         string // a stash's untracked file lives in a commit of its own
	added, deleted int    // -1 each for a binary file
	diff           *editor.DiffState
	patch          editor.FilePatch
}

// readCommitFiles reads the files of a commit and their diffs. A
// stash's untracked files are in commits of their own, whose diffs come
// with them. Files stay listed when a patch cannot be read.
func (a *App) readCommitFiles(ctx context.Context, root, hash string, d git.CommitDetails) []commitFile {
	var patches []editor.FilePatch
	if out, err := git.CommitPatch(ctx, root, hash); err == nil {
		patches, _ = editor.ParsePatch(string(out))
	}
	for _, f := range d.Files {
		if f.Commit == "" {
			continue
		}
		diff, err := git.CommitFileDiff(ctx, root, f.Commit, f)
		if err != nil {
			continue
		}
		p, _ := editor.ParsePatch(string(diff))
		patches = append(patches, p...)
	}
	return a.commitFilesFromPatches(d.Files, patches)
}

// readCommitSummary is a commit's message and the files it changed,
// before their patches.
func (a *App) readCommitSummary(ctx context.Context, root, hash, email string) (git.CommitDetails, git.Message, error) {
	d, err := git.Show(ctx, root, hash)
	if err != nil {
		return d, git.Message{}, err
	}
	return d, git.ParseMessage(d.Body, email), nil
}

// readCommitDetails is what every commit detail loads: its message,
// and its files with their diffs.
func (a *App) readCommitDetails(ctx context.Context, root, hash, email string) (git.CommitDetails, git.Message, []commitFile, error) {
	d, msg, err := a.readCommitSummary(ctx, root, hash, email)
	if err != nil {
		return d, msg, nil, err
	}
	return d, msg, a.readCommitFiles(ctx, root, hash, d), nil
}

// commitFilesFromPatches are a commit's files in git's order, each with
// the diff of its patch.
func (a *App) commitFilesFromPatches(stats []git.FileStat, patches []editor.FilePatch) []commitFile {
	byName := map[string]editor.FilePatch{}
	for _, p := range patches {
		name := p.NewName
		if name == "" {
			name = p.OldName
		}
		byName[name] = p
	}
	files := make([]commitFile, 0, len(stats))
	for _, f := range stats {
		cf := commitFile{path: f.Path, oldPath: f.OldPath, commit: f.Commit, added: f.Added, deleted: f.Deleted}
		if p, ok := byName[f.Path]; ok && !p.Binary && len(p.Hunks) > 0 {
			d := &editor.DiffState{Theme: a.edTheme, FitContent: true}
			a.setDiffPatch(d, p, false)
			a.applyDiffPrefs(d)
			cf.diff = d
			cf.patch = p
		}
		files = append(files, cf)
	}
	return files
}

// commitFilesBlock is a commit's files: a summary line, then each file
// under a head that folds its diff away, the first ones unfolded.
func (a *App) commitFilesBlock(c *ui.Context, g *group, v *commitView) {
	files, cm := v.files, v.commit
	type fileStat struct{ add, del int }
	stats := make([]fileStat, len(files))
	added, deleted := 0, 0
	for i, f := range files {
		plus, minus := a.diffStats(f.diff, f.added, f.deleted)
		stats[i] = fileStat{plus, minus}
		added += max(plus, 0)
		deleted += max(minus, 0)
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.MsgDFileSChanged(len(files))).FontSize(13).Bold()
		ui.Text(c, fmt.Sprintf("+%d", added)).FontSize(12).TextColor(a.gitStatusColor('A'))
		ui.Text(c, fmt.Sprintf("−%d", deleted)).FontSize(12).TextColor(a.gitStatusColor('D'))
	})
	if v.collapsed == nil {
		v.collapsed = map[string]bool{}
	}
	if v.shown <= 0 {
		v.shown = commitFilesAtOnce
	}
	for i, f := range files {
		if i >= v.shown {
			rest := len(files) - v.shown
			if ui.Button(c, locales.MsgShowDMoreFileS(rest)).FontSize(12).Clicked() {
				v.shown += commitFilesAtOnce
			}
			return
		}
		folded, stat := v.collapsed[f.path], stats[i]
		ui.Column(c.Key(f.path)).FillWidth().Radius(6).BorderWidth(1).BorderColor(colors(c).Separator).Children(func() {
			head := ui.Row(c).FillWidth().Height(30).Padding(0, 10).Gap(8).AlignItems(ui.Center).Background(colors(c).Pill).
				Cursor(ui.CursorPointer).Label(f.path).Tooltip(commitFileHint(folded))
			head.OnClick(func() { v.collapsed[f.path] = !v.collapsed[f.path] })
			head.ContextMenu(func(m *ui.Menu) {
				if m.Item(locales.MsgCommitFileOpenDiff()).Chosen() {
					a.openCommitFile(g, cm, git.FileStat{
						Path: f.path, OldPath: f.oldPath, Added: f.added, Deleted: f.deleted, Commit: f.commit,
					})
				}
			})
			head.Children(func() {
				fileIcon(c, filepath.Base(f.path), 14)
				name := filepath.ToSlash(f.path)
				if f.oldPath != "" {
					name = filepath.ToSlash(f.oldPath) + " → " + name
				}
				ui.Text(c, name).FontSize(12).Bold().SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
				ui.Box(c).Grow(1)
				if f.added < 0 {
					ui.Text(c, locales.MsgBinary()).FontSize(11).TextColor(colors(c).Muted)
				} else {
					ui.Text(c, fmt.Sprintf("+%d", stat.add)).FontSize(11).TextColor(a.gitStatusColor('A'))
					ui.Text(c, fmt.Sprintf("−%d", stat.del)).FontSize(11).TextColor(a.gitStatusColor('D'))
				}
				if folded {
					symbolIcon(c, icChangeDown, 10, colors(c).Muted)
				} else {
					symbolIcon(c, icChangeUp, 10, colors(c).Muted)
				}
			})
			if folded {
				return
			}
			if f.diff == nil {
				what := locales.MsgNoChangesToShow()
				if f.added < 0 {
					what = locales.MsgBinaryFileNotShown()
				}
				ui.Text(c, what).FontSize(12).TextColor(colors(c).Muted).Padding(10, 12)
				return
			}
			if a.diffChangesHidden(f.diff) {
				a.hiddenWhitespaceDiff(c)
			} else {
				editor.DiffView(c, f.diff).FillWidth()
			}
		})
	}
}

func commitFileHint(folded bool) string {
	if folded {
		return locales.MsgCommitFileUnfoldHint()
	}
	return locales.MsgCommitFileFoldHint()
}
