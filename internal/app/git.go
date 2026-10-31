package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
	"see/internal/workspace"
)

type gitState struct {
	root, branch, message  string
	head                   string // HEAD's commit, so committed versions are read again only when it moves
	changes                []git.Change
	decor                  map[string]byte
	err                    string
	loading, pending, busy bool
	diffSeq                uint64
	peek                   *gitPeek
}

type gitPeek struct {
	doc    *doc
	group  *group
	line   int
	diff   *editor.DiffState
	scroll ui.ScrollState
}

type gitDiffTab struct {
	state  *editor.DiffState
	patch  editor.FilePatch
	name   string
	staged bool
}

func (a *App) newGitDiffTab(p editor.FilePatch, name string, staged bool) *gitDiffTab {
	d := &editor.DiffState{Theme: a.edTheme}
	a.applyDiffPrefs(d)
	d.SetPatch(p)
	d.GoToHunk(0)
	return &gitDiffTab{state: d, patch: p, name: name, staged: staged}
}

func (a *App) startGit() {
	if watchFiles == nil {
		return
	}
	a.refreshGit()
	ctx, gen := a.wsCtx, a.gen
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.post.Post(func() {
					if a.gen == gen {
						a.refreshGit()
					}
				})
			}
		}
	}()
}

func (a *App) refreshGit() {
	if a.root == "" || a.wsCtx == nil {
		return
	}
	if a.git.loading || a.git.busy {
		a.git.pending = true
		return
	}
	a.git.loading = true
	ctx, root, gen := a.wsCtx, a.root, a.gen
	go func() {
		r, b, changes, err := git.Read(ctx, root)
		var head string
		if err == nil {
			head = git.Head(ctx, r)
		}
		a.post.Post(func() {
			if a.gen != gen {
				return
			}
			a.git.loading = false
			a.git.root, a.git.branch, a.git.changes, a.git.head = r, b, changes, head
			a.git.decor = git.Decorations(r, changes)
			for _, d := range a.docs {
				if d.buf != nil {
					a.loadGitBase(d)
				}
			}
			a.git.err = ""
			if err != nil {
				a.git.err = err.Error()
			}
			if a.git.pending {
				a.git.pending = false
				a.refreshGit()
			}
		})
	}()
}

// loadGitBase reads the committed version off the UI thread. A tracked file
// added to the index has an empty committed version. It is read again only
// when the repository, HEAD, the file's path or its status changed: git's
// status refreshes every few seconds, and reading every open file's
// committed version each time spawned two git processes a file.
func (a *App) loadGitBase(d *doc) {
	if key := a.gitBaseKey(d); key != "" && key == d.gitBaseKey {
		return
	}
	d.gitBaseSeq++
	d.gitBaseKey = ""
	seq, gen, root, ctx, path := d.gitBaseSeq, a.gen, a.git.root, a.wsCtx, d.path
	if root == "" || ctx == nil || d.untitled {
		return
	}
	key := a.gitBaseKey(d)
	gitPath := path
	if !workspace.Within(root, gitPath) && a.realRoot != "" && workspace.Within(a.root, path) {
		if sub, err := filepath.Rel(a.root, path); err == nil {
			gitPath = filepath.Join(a.realRoot, sub)
		}
	}
	if !workspace.Within(root, gitPath) {
		return
	}
	rel, err := filepath.Rel(root, gitPath)
	if err != nil {
		return
	}
	d.gitBaseKey = key
	go func() {
		_, trackedErr := git.Command(ctx, root, "ls-files", "--error-unmatch", "--", rel)
		var base []byte
		if trackedErr == nil {
			base, err = git.Command(ctx, root, "show", "HEAD:"+filepath.ToSlash(rel))
		}
		// A file newly added to the index has no committed contents.
		if err != nil {
			base = nil
		}
		valid := trackedErr == nil && len(base) <= 4<<20 && utf8.Valid(base) && !strings.ContainsRune(string(base), 0)
		a.post.Post(func() {
			if a.gen != gen || a.docs[path] != d || d.gitBaseSeq != seq {
				return
			}
			if trackedErr != nil || err != nil {
				// Untracked, newly added or a failed read: ask again on
				// the next refresh rather than trusting it.
				d.gitBaseKey = ""
			}
			if valid && d.gitBase != nil && d.gitBase.String() == string(base) || !valid && d.gitBase == nil {
				return
			}
			d.gitBase = nil
			d.gitChanges = nil
			if valid {
				d.gitBase = editor.NewBuffer(string(base))
				d.gitChanges = &editor.DiffState{Old: d.gitBase, New: d.buf}
			}
			for _, g := range a.groups() {
				if st := g.eds[path]; st != nil {
					st.DiffBase = d.gitBase
				}
			}
			if a.git.peek != nil && a.git.peek.doc == d {
				a.git.peek = nil
			}
		})
	}()
}

// gitBaseKey names what d's committed version depends on, or "" when it
// cannot be told.
func (a *App) gitBaseKey(d *doc) string {
	root := a.git.root
	if root == "" || a.git.head == "" || d.untitled {
		return ""
	}
	gitPath := d.path
	if !workspace.Within(root, gitPath) && a.realRoot != "" && workspace.Within(a.root, gitPath) {
		if sub, err := filepath.Rel(a.root, gitPath); err == nil {
			gitPath = filepath.Join(a.realRoot, sub)
		}
	}
	rel, err := filepath.Rel(root, gitPath)
	if err != nil || !workspace.Within(root, gitPath) {
		return ""
	}
	rel = filepath.ToSlash(rel)
	status := "  "
	for _, ch := range a.git.changes {
		if ch.Path == rel || ch.OldPath == rel {
			status = string([]byte{ch.Index, ch.Work})
			break
		}
	}
	return root + "\x00" + a.git.head + "\x00" + rel + "\x00" + status
}

func (a *App) showGitPeek(g *group, d *doc, st *editor.State, line int) {
	if st.DiffBase != nil {
		diff := &editor.DiffState{Old: st.DiffBase, New: d.buf, Layout: editor.DiffUnified, Theme: a.edTheme, FitContent: true}
		a.applyDiffPrefs(diff)
		for i, h := range diff.Hunks() {
			if line >= h.NewStart && line < max(h.NewEnd, h.NewStart+1) || h.NewStart >= d.buf.LineCount() && line == d.buf.LineCount()-1 {
				a.choices = nil
				diff.GoToHunk(i)
				a.git.peek = &gitPeek{doc: d, group: g, line: line, diff: diff}
				a.git.peek.scroll.Y = diffHunkOffset(st, diff, i)
				return
			}
		}
	}
	a.git.peek = nil
	start := d.buf.Offset(editor.Pos{Line: line})
	end := d.buf.Len()
	if line+1 < d.buf.LineCount() {
		end = d.buf.Offset(editor.Pos{Line: line + 1})
	}
	st.Select(start, end)
}

func (a *App) gitStatusColor(status byte) ui.Color {
	if a.edTheme == nil {
		return a.colors().Muted
	}
	switch status {
	case 'U', 'A':
		return a.edTheme.Added
	case 'D', '!':
		return a.edTheme.Deleted
	default:
		return a.edTheme.Modified
	}
}

func (a *App) gitAction(args ...string) {
	if a.git.busy || a.git.root == "" {
		return
	}
	a.git.busy = true
	ctx, root, gen := a.wsCtx, a.git.root, a.gen
	go func() {
		_, err := git.Command(ctx, root, args...)
		a.post.Post(func() {
			if a.gen != gen {
				return
			}
			a.git.busy = false
			if err != nil {
				a.note("Git: "+err.Error(), true)
			} else {
				a.git.diffSeq++
				if args[0] == "commit" {
					a.git.message = ""
					a.note("Committed staged changes.", false)
				}
			}
			a.refreshGit()
		})
	}()
}

func (a *App) openGitFile(ch git.Change) {
	path := filepath.Join(a.git.root, filepath.FromSlash(ch.Path))
	a.term.maximized = false
	a.git.diffSeq++
	a.userOpen(path)
}

func (a *App) discardGitChange(ch git.Change) {
	if a.git.busy || a.git.root == "" || a.deleteConfirm != nil {
		return
	}
	path := workspace.DocPath(a.root, a.realRoot, filepath.Join(a.git.root, filepath.FromSlash(ch.Path)))
	if d := a.docs[path]; d != nil && d.buf != nil && d.buf.Modified() {
		a.note("Save or close the file's unsaved edits before discarding its Git changes.", true)
		return
	}
	message := fmt.Sprintf("Discard working tree changes to %q? This cannot be undone.", ch.Path)
	if ch.Index == '?' {
		message = fmt.Sprintf("Move untracked file %q to Trash?", ch.Path)
	}
	q := &confirmation{message: message}
	q.actions = []action{
		{"Discard Changes", func() {
			if a.deleteConfirm != q || a.git.busy {
				return
			}
			a.deleteConfirm = nil
			if d := a.docs[path]; d != nil && d.buf != nil && d.buf.Modified() {
				a.note("Save or close the file's unsaved edits before discarding its Git changes.", true)
				return
			}
			a.git.busy = true
			ctx, root, gen, trash := a.wsCtx, a.git.root, a.gen, a.host.TrashItem
			go func() {
				var err error
				if ch.Index == '?' {
					err = trash(path)
				} else {
					_, err = git.Command(ctx, root, "restore", "--worktree", "--", ch.Path)
				}
				a.post.Post(func() {
					if a.gen != gen {
						return
					}
					a.git.busy = false
					if err != nil {
						a.note("Could not discard Git changes: "+err.Error(), true)
					} else {
						a.git.diffSeq++
						if ch.Index == '?' {
							a.removed(path)
						} else if d := a.docs[path]; d != nil {
							a.reloadDoc(d)
						}
						a.refreshDir(filepath.Dir(path))
						a.quick.files = nil
						a.filter.forQuery = ""
					}
					a.refreshGit()
				})
			}()
		}},
		{"Cancel", func() {
			if a.deleteConfirm == q {
				a.deleteConfirm = nil
			}
		}},
	}
	a.deleteConfirm = q
}

func (a *App) showGitDiff(ch git.Change, staged bool) {
	a.showGitDiffIn(ch, staged, a.focus, sideNone, len(a.focus.order))
}

func (a *App) showGitDiffIn(ch git.Change, staged bool, target *group, placement side, at int) {
	if a.git.root == "" {
		return
	}
	a.term.maximized = false
	a.git.peek = nil
	a.git.diffSeq++
	seq, gen, root, ctx := a.git.diffSeq, a.gen, a.git.root, a.wsCtx
	go func() {
		args := []string{"diff", "--no-ext-diff", "--no-color", "--unified=2147483647"}
		if staged {
			args = append(args, "--cached")
		}
		if ch.Index == '?' {
			args = []string{"diff", "--no-index", "--no-ext-diff", "--no-color", "--unified=2147483647", "--", "/dev/null", filepath.Join(root, ch.Path)}
		} else {
			args = append(args, "--", ch.Path)
		}
		out, err := git.Command(ctx, root, args...)
		if ch.Index == '?' && len(out) > 0 {
			err = nil
		}
		patches, parseErr := editor.ParsePatch(string(out))
		a.post.Post(func() {
			if a.gen != gen || a.git.diffSeq != seq || a.layout.find(target) == nil {
				return
			}
			if err != nil {
				a.note("Git diff: "+err.Error(), true)
				return
			}
			if parseErr != nil || len(patches) == 0 || patches[0].Binary {
				a.note("No text diff available for "+ch.Path, true)
				return
			}
			v := a.newGitDiffTab(patches[0], ch.Path, staged)
			key := diffTab(ch.Path, staged)
			if placement != sideNone {
				target = a.splitBeside(target, placement)
				at = 0
			}
			if target.diffs == nil {
				target.diffs = map[string]*gitDiffTab{}
			}
			target.diffs[key] = v
			target.addTab(key, at)
			a.activateIn(target, key)
		})
	}()
}

func (a *App) gitNavigator(c *ui.Context) {
	if a.git.root == "" {
		msg := "Looking for a Git repository…"
		if a.git.err != "" {
			msg = "No Git repository found, or Git is unavailable."
		}
		ui.Text(c, msg).TextColor(colors(c).Muted).FontSize(12).Padding(12, 10).Tooltip(a.git.err)
		return
	}
	staged, unstaged := 0, 0
	for _, ch := range a.git.changes {
		if ch.Index != ' ' && ch.Index != '?' {
			staged++
		}
		if ch.Work != ' ' || ch.Index == '?' {
			unstaged++
		}
	}
	ui.Column(c).FillWidth().Padding(10, 10, 10, 10).Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c, &a.git.message).Placeholder("Commit message").Label("Commit message").Grow(1)
			if glyphButton(c, icRefresh, "Refresh Git", "Refresh Git status", false, false).Clicked() {
				a.refreshGit()
			}
		})
		ui.PrimaryButton(c, "Commit").FillWidth().Disabled(a.git.busy || staged == 0 || strings.TrimSpace(a.git.message) == "").OnClick(func() { a.gitAction("commit", "-m", a.git.message) })
	})
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Children(func() {
			if len(a.git.changes) == 0 {
				ui.Text(c, "No changes").FontSize(12).TextColor(colors(c).Muted).Padding(10)
			}
			if staged > 0 {
				a.gitSection(c, "Staged Changes", staged)
			}
			for _, ch := range a.git.changes {
				if ch.Index != ' ' && ch.Index != '?' {
					a.gitRow(c, ch, true)
				}
			}
			if unstaged > 0 {
				a.gitSection(c, "Changes", unstaged)
			}
			for _, ch := range a.git.changes {
				if ch.Work != ' ' || ch.Index == '?' {
					a.gitRow(c, ch, false)
				}
			}
		})
	})
}

func (a *App) gitSection(c *ui.Context, name string, count int) {
	ui.Row(c).FillWidth().Padding(9, 16, 4, 16).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, name).FontSize(11).Bold().TextColor(colors(c).Muted)
		ui.Text(c, fmt.Sprint(count)).FontSize(11).TextColor(colors(c).Dim)
	})
}

func (a *App) gitRow(c *ui.Context, ch git.Change, staged bool) {
	label, action := "Unstaged", "Stage"
	if staged {
		label, action = "Staged", "Unstage"
	}
	container := ui.Row(c.Key(fmt.Sprintf("%t:%s", staged, ch.Path))).FillWidth().Padding(1, 13).Gap(2).Radius(4).AlignItems(ui.Center)
	hovered := container.Hovered()
	if a.focus.active == diffTab(ch.Path, staged) {
		container.Background(colors(c).SelectedRow)
	} else if hovered {
		container.Background(colors(c).Pill)
	}
	container.Children(func() {
		row := ui.ButtonBase(c).Label(label+" diff: "+ch.Path).Tooltip(ch.Path).Grow(1).Padding(4, 4).Radius(4)
		row.Drag(&tabDrag{change: &ch, staged: staged})
		row.Children(func() {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				fileIcon(c, filepath.Base(ch.Path), 15)
				ui.Text(c, filepath.Base(ch.Path)).FontSize(12).SingleLine().Shrink(1)
				dir := filepath.Dir(ch.Path)
				if dir != "." {
					ui.Text(c, filepath.ToSlash(dir)).FontSize(11).TextColor(colors(c).Dim).SingleLine().Shrink(1)
				}
			})
		})
		row.OnClick(func() { a.showGitDiff(ch, staged) })
		if hovered {
			if ch.Work != 'D' && !(ch.Index == 'D' && ch.Work == ' ') && glyphButton(c, icGitOpen, "Open file "+ch.Path, "Open File", false, false).Clicked() {
				a.openGitFile(ch)
			}
			if !staged && glyphButton(c, icGitDiscard, "Discard changes "+ch.Path, "Discard Changes", false, a.git.busy).Clicked() {
				a.discardGitChange(ch)
			}
			icon := icPlus
			if staged {
				icon = icMinus
			}
			if glyphButton(c, icon, action+" "+ch.Path, action, false, a.git.busy).Clicked() {
				paths := []string{ch.Path}
				if ch.OldPath != "" {
					paths = append(paths, ch.OldPath)
				}
				if staged {
					a.gitAction(append([]string{"reset", "-q", "--"}, paths...)...)
				} else {
					a.gitAction(append([]string{"add", "--"}, paths...)...)
				}
			}
		}
		status := ch.Work
		if staged {
			status = ch.Index
		}
		if status == '?' {
			status = 'U'
		}
		ui.Text(c, string(status)).FontSize(11).TextColor(a.gitStatusColor(status)).Padding(0, 4)
	})
}
