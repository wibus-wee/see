package main

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type gitChange struct {
	path        string
	oldPath     string
	index, work byte
}
type gitState struct {
	root, branch, message  string
	changes                []gitChange
	decor                  map[string]byte
	err                    string
	loading, pending, busy bool
	diff                   *editor.DiffState
	diffName               string
	diffStaged             bool
	diffSeq                uint64
}

func gitCommand(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args = append([]string{"-c", "core.fsmonitor=false", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_LITERAL_PATHSPECS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func readGit(ctx context.Context, dir string) (string, string, []gitChange, error) {
	root, err := gitCommand(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", nil, err
	}
	r := strings.TrimSpace(string(root))
	branch, err := gitCommand(ctx, r, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		branch, err = gitCommand(ctx, r, "rev-parse", "--short", "HEAD")
	}
	if err != nil {
		return "", "", nil, err
	}
	out, err := gitCommand(ctx, r, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return "", "", nil, err
	}
	return r, strings.TrimSpace(string(branch)), parseGitStatus(out), nil
}

func parseGitStatus(out []byte) []gitChange {
	parts := strings.Split(string(out), "\x00")
	var changes []gitChange
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if len(p) < 4 {
			continue
		}
		change := gitChange{path: p[3:], index: p[0], work: p[1]}
		if p[0] == 'R' || p[0] == 'C' || p[1] == 'R' || p[1] == 'C' {
			i++
			if i < len(parts) {
				change.oldPath = parts[i]
			}
		}
		changes = append(changes, change)
	}
	return changes
}

func (a *app) startGit() {
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
				a.post(func() {
					if a.gen == gen {
						a.refreshGit()
					}
				})
			}
		}
	}()
}

func (a *app) refreshGit() {
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
		r, b, changes, err := readGit(ctx, root)
		a.post(func() {
			if a.gen != gen {
				return
			}
			a.git.loading = false
			a.git.root, a.git.branch, a.git.changes = r, b, changes
			a.git.decor = gitDecorations(r, changes)
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

// gitDecorations indexes changed paths and their parents for the file tree.
func gitDecorations(root string, changes []gitChange) map[string]byte {
	out := make(map[string]byte)
	if root == "" {
		return out
	}
	for _, ch := range changes {
		status := gitStatus(ch)
		p := filepath.Join(root, filepath.FromSlash(ch.path))
		out[p] = status
		for dir := filepath.Dir(p); dir != root && dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			if !within(root, dir) {
				break
			}
			if out[dir] == 0 || gitStatusPriority(status) > gitStatusPriority(out[dir]) {
				out[dir] = status
			}
		}
	}
	return out
}

func gitStatus(ch gitChange) byte {
	if ch.index == '?' {
		return 'U'
	}
	if ch.index == 'U' || ch.work == 'U' || (ch.index == 'A' && ch.work == 'A') || (ch.index == 'D' && ch.work == 'D') {
		return '!'
	}
	if ch.work != ' ' {
		return ch.work
	}
	return ch.index
}

func gitStatusPriority(s byte) int {
	switch s {
	case '!':
		return 4
	case 'D':
		return 3
	case 'M', 'R', 'C':
		return 2
	default:
		return 1
	}
}

func (a *app) gitStatusColor(status byte) ui.Color {
	if a.edTheme == nil {
		return colMuted
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

func (a *app) gitAction(args ...string) {
	if a.git.busy || a.git.root == "" {
		return
	}
	a.git.busy = true
	ctx, root, gen := a.wsCtx, a.git.root, a.gen
	go func() {
		_, err := gitCommand(ctx, root, args...)
		a.post(func() {
			if a.gen != gen {
				return
			}
			a.git.busy = false
			if err != nil {
				a.note("Git: "+err.Error(), true)
			} else if args[0] == "commit" {
				a.git.message = ""
				a.note("Committed staged changes.", false)
			}
			a.refreshGit()
		})
	}()
}

func (a *app) showGitDiff(ch gitChange, staged bool) {
	if a.git.root == "" {
		return
	}
	a.git.diffSeq++
	seq, gen, root, ctx := a.git.diffSeq, a.gen, a.git.root, a.wsCtx
	go func() {
		args := []string{"diff", "--no-ext-diff", "--no-color"}
		if staged {
			args = append(args, "--cached")
		}
		if ch.index == '?' {
			args = []string{"diff", "--no-index", "--no-ext-diff", "--no-color", "--", "/dev/null", filepath.Join(root, ch.path)}
		} else {
			args = append(args, "--", ch.path)
		}
		out, err := gitCommand(ctx, root, args...)
		if ch.index == '?' && len(out) > 0 {
			err = nil
		}
		patches, parseErr := editor.ParsePatch(string(out))
		a.post(func() {
			if a.gen != gen || a.git.diffSeq != seq {
				return
			}
			if err != nil {
				a.note("Git diff: "+err.Error(), true)
				return
			}
			if parseErr != nil || len(patches) == 0 || patches[0].Binary {
				a.note("No text diff available for "+ch.path, true)
				return
			}
			d := &editor.DiffState{Theme: a.edTheme, FitContent: true}
			a.applyDiffPrefs(d)
			d.SetPatch(patches[0])
			a.git.diff, a.git.diffName, a.git.diffStaged = d, ch.path, staged
		})
	}()
}

func (a *app) gitNavigator(c *ui.Context) {
	if a.git.root == "" {
		msg := "Looking for a Git repository…"
		if a.git.err != "" {
			msg = "No Git repository found, or Git is unavailable."
		}
		ui.Text(c, msg).TextColor(colMuted).FontSize(12).Padding(12, 10).Tooltip(a.git.err)
		return
	}
	staged, unstaged := 0, 0
	for _, ch := range a.git.changes {
		if ch.index != ' ' && ch.index != '?' {
			staged++
		}
		if ch.work != ' ' || ch.index == '?' {
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
				ui.Text(c, "No changes").FontSize(12).TextColor(colMuted).Padding(10)
			}
			if staged > 0 {
				a.gitSection(c, "Staged Changes", staged)
			}
			for _, ch := range a.git.changes {
				if ch.index != ' ' && ch.index != '?' {
					a.gitRow(c, ch, true)
				}
			}
			if unstaged > 0 {
				a.gitSection(c, "Changes", unstaged)
			}
			for _, ch := range a.git.changes {
				if ch.work != ' ' || ch.index == '?' {
					a.gitRow(c, ch, false)
				}
			}
		})
	})
}

func (a *app) gitSection(c *ui.Context, name string, count int) {
	ui.Row(c).FillWidth().Padding(9, 16, 4, 16).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, name).FontSize(11).Bold().TextColor(colMuted)
		ui.Text(c, fmt.Sprint(count)).FontSize(11).TextColor(colDim)
	})
}

func (a *app) gitRow(c *ui.Context, ch gitChange, staged bool) {
	label, action := "Unstaged", "Stage"
	if staged {
		label, action = "Staged", "Unstage"
	}
	ui.Row(c.Key(fmt.Sprintf("%t:%s", staged, ch.path))).FillWidth().Padding(1, 13).Gap(2).AlignItems(ui.Center).Children(func() {
		row := ui.ButtonBase(c).Label(label+" diff: "+ch.path).Tooltip(ch.path).Grow(1).Padding(4, 4).Radius(4)
		if a.git.diff != nil && a.git.diffName == ch.path && a.git.diffStaged == staged {
			row.Background(colSelectedRow)
		} else if row.Hovered() {
			row.Background(colPill)
		}
		row.Children(func() {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				fileIcon(c, filepath.Base(ch.path), 15)
				ui.Text(c, filepath.Base(ch.path)).FontSize(12).SingleLine().Shrink(1)
				dir := filepath.Dir(ch.path)
				if dir != "." {
					ui.Text(c, filepath.ToSlash(dir)).FontSize(11).TextColor(colDim).SingleLine().Shrink(1)
				}
				ui.Box(c).Grow(1)
				status := ch.work
				if staged {
					status = ch.index
				}
				if status == '?' {
					status = 'U'
				}
				ui.Text(c, string(status)).FontSize(11).TextColor(a.gitStatusColor(status))
			})
		})
		row.OnClick(func() { a.showGitDiff(ch, staged) })
		icon := icPlus
		if staged {
			icon = icMinus
		}
		if glyphButton(c, icon, action+" "+ch.path, action, false, a.git.busy).Clicked() {
			paths := []string{ch.path}
			if ch.oldPath != "" {
				paths = append(paths, ch.oldPath)
			}
			if staged {
				a.gitAction(append([]string{"reset", "-q", "--"}, paths...)...)
			} else {
				a.gitAction(append([]string{"add", "--"}, paths...)...)
			}
		}
	})
}

func (a *app) gitDiffView(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Padding(6, 10).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(colHairline).Children(func() {
			fileIcon(c, filepath.Base(a.git.diffName), 16)
			ui.Text(c, a.git.diffName).FontSize(12).SingleLine().Shrink(1)
			kind := "Working Tree"
			if a.git.diffStaged {
				kind = "Staged"
			}
			ui.Text(c, kind).FontSize(11).TextColor(colMuted)
			ui.Box(c).Grow(1)
			if glyphButton(c, icClose, "Close Diff", "Close Diff", false, false).Clicked() {
				a.git.diff = nil
				a.git.diffSeq++
			}
		})
		if a.git.diff != nil {
			ui.Scroll(c).Grow(1).FillWidth().Children(func() {
				editor.DiffView(c, a.git.diff).FillWidth()
			})
		}
	})
}
