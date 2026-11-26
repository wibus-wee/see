package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
	"see/locales"
)

func gitPartialText(key string) string { return locales.Catalog.Format(i18n.Locale(), key, nil) }

type gitPartialPatch struct {
	digest           [sha256.Size]byte
	oldMode, newMode string
}

func partialGitPatch(raw []byte, p editor.FilePatch, ch git.Change, staged bool) *gitPartialPatch {
	if ch.Status() == '!' || staged && ch.OldPath != "" || p.Binary || len(p.Hunks) != 1 ||
		p.OldName != "" && p.OldName != ch.Path || p.NewName != "" && p.NewName != ch.Path ||
		p.Hunks[0].OldStart != gitFirstLine(p.Hunks[0].OldLines) || p.Hunks[0].NewStart != gitFirstLine(p.Hunks[0].NewLines) {
		return nil
	}
	oldMode, newMode := p.OldMode, p.NewMode
	for line := range strings.SplitSeq(string(raw), "\n") {
		if strings.HasPrefix(line, "@@") {
			break
		}
		if fields := strings.Fields(line); len(fields) == 3 && fields[0] == "index" {
			oldMode, newMode = fields[2], fields[2]
		}
	}
	regular := func(mode string) bool { return mode == "100644" || mode == "100755" }
	if (p.OldName != "" && !regular(oldMode)) || (p.NewName != "" && !regular(newMode)) ||
		(p.OldName == "" && oldMode != "") || (p.NewName == "" && newMode != "") {
		return nil
	}
	return &gitPartialPatch{digest: sha256.Sum256(raw), oldMode: oldMode, newMode: newMode}
}

func gitFirstLine(count int) int {
	if count == 0 {
		return 0
	}
	return 1
}

type gitDiffPart struct {
	hunk      editor.DiffHunk
	lines     editor.DiffLineRange
	selection bool
}

func (p gitDiffPart) includes(side editor.DiffSide, line int) bool {
	if p.selection {
		return p.lines.Side == side && p.lines.Start <= line && line <= p.lines.End
	}
	if side == editor.DiffOld {
		return p.hunk.OldStart <= line && line < p.hunk.OldEnd
	}
	return p.hunk.NewStart <= line && line < p.hunk.NewEnd
}

func gitSelectedPart(state *editor.DiffState, hunks []editor.DiffHunk) (gitDiffPart, bool) {
	lines, ok := state.SelectedLines()
	part := gitDiffPart{lines: lines, selection: true}
	if ok {
		for _, h := range hunks {
			first, end := h.NewStart, h.NewEnd
			if lines.Side == editor.DiffOld {
				first, end = h.OldStart, h.OldEnd
			}
			if max(first, lines.Start) < min(end, lines.End+1) {
				return part, true
			}
		}
	}
	return gitDiffPart{}, false
}

func (p *gitPartialPatch) apply(ctx context.Context, root, name string, staged bool, file editor.FilePatch, part gitDiffPart) error {
	var before, after []string
	var oldLine, newLine int
	lines := file.Hunks[0].Lines
	for i, row := range lines {
		if len(row) == 0 || row[0] == '\\' {
			continue
		}
		line := row[1:]
		if i+1 == len(lines) || !strings.HasPrefix(lines[i+1], "\\") {
			line += "\n"
		}
		switch row[0] {
		case ' ':
			before, after = append(before, line), append(after, line)
			oldLine++
			newLine++
		case '-':
			if !staged {
				before = append(before, line)
			}
			if part.includes(editor.DiffOld, oldLine) == staged {
				after = append(after, line)
			}
			oldLine++
		case '+':
			if staged {
				before = append(before, line)
			}
			if part.includes(editor.DiffNew, newLine) != staged {
				after = append(after, line)
			}
			newLine++
		}
	}
	// A line that used to be last can become interior if only one side
	// of the EOF change was chosen.
	for i, line := range after[:max(0, len(after)-1)] {
		if !strings.HasSuffix(line, "\n") {
			after[i] += "\n"
		}
	}
	indexMode, otherMode := p.oldMode, p.newMode
	if staged {
		indexMode, otherMode = otherMode, indexMode
	}
	targetMode := indexMode
	if indexMode == "" || otherMode == "" && len(after) == 0 {
		targetMode = otherMode
	}
	if indexMode == targetMode && slices.Equal(before, after) {
		return nil
	}
	return git.ApplyIndexText(ctx, root, name, indexMode, targetMode, before, after)
}

type gitChangeView struct {
	file      editor.FilePatch
	partial   *gitPartialPatch
	untracked bool
}

func parseGitChangeView(raw []byte, ch git.Change, staged bool) (gitChangeView, error) {
	if len(raw) == 0 {
		return gitChangeView{}, nil
	}
	patches, err := editor.ParsePatch(string(raw))
	if err != nil || len(patches) == 0 || patches[0].Binary {
		return gitChangeView{}, errors.New(locales.MsgNoTextDiffAvailableFor() + ch.Path)
	}
	v := gitChangeView{file: patches[0], untracked: ch.Index == '?'}
	if len(patches) == 1 {
		v.partial = partialGitPatch(raw, v.file, ch, staged)
	}
	return v, nil
}

func readGitChangeView(ctx context.Context, root string, ch git.Change, staged bool) (gitChangeView, error) {
	raw, err := git.ChangePatch(ctx, root, ch.Path, staged, ch.Index == '?')
	if err != nil {
		return gitChangeView{}, err
	}
	return parseGitChangeView(raw, ch, staged)
}

func (a *App) setGitChangeView(v *gitDiffTab, view gitChangeView, hunk int) {
	if v.state == nil {
		v.state = &editor.DiffState{Theme: a.edTheme}
	}
	v.patch, v.partial, v.untracked = view.file, view.partial, view.untracked
	v.state.LineSelection = v.partial != nil
	v.state.SetSelectedLines(editor.DiffLineRange{}, false)
	setPatch(v.state, v.patch)
	a.applyDiffPrefs(v.state)
	v.state.GoToHunk(max(0, min(hunk, len(v.state.Hunks())-1)))
}

func (a *App) applyGitDiffPart(v *gitDiffTab, part gitDiffPart) {
	root := v.root
	if root == "" {
		root = a.git.root
	}
	workingCopy := a.git.workingCopies[root]
	groupScoped := workingCopy != nil
	busy := a.git.busy
	if groupScoped {
		busy = workingCopy.busy || root == a.git.root && a.git.busy
	}
	if busy || root == "" || v.partial == nil {
		return
	}
	if groupScoped {
		workingCopy.busy = true
		if root == a.git.root {
			a.git.busy = true
		}
	} else {
		a.git.busy = true
	}
	if workingCopy != nil {
		workingCopy.refreshSeq++
		workingCopy.loading = false
		workingCopy.diffSeq++
	}
	ctx, gen, scopeSeq := a.wsCtx, a.gen, a.git.scopeSeq
	name, staged, digest := v.name, v.staged, v.partial.digest
	go func() {
		err := applyCurrentGitDiffPart(ctx, root, name, staged, digest, part)
		var working, index gitChangeView
		ch, refreshErr := git.PathChange(ctx, root, name)
		if refreshErr == nil && ch.Path != "" {
			if ch.Work != ' ' || ch.Index == '?' {
				working, refreshErr = readGitChangeView(ctx, root, ch, false)
			}
			if refreshErr == nil && ch.Index != ' ' && ch.Index != '?' {
				index, refreshErr = readGitChangeView(ctx, root, ch, true)
			}
		}
		a.post.Post(func() {
			if a.gen != gen || groupScoped && a.git.workingCopies[root] != workingCopy ||
				!groupScoped && (a.git.scopeSeq != scopeSeq || a.git.root != root) {
				return
			}
			if groupScoped {
				workingCopy.busy = false
				if root == a.git.root {
					a.git.busy = false
				}
			} else {
				a.git.busy = false
			}
			if err != nil {
				a.note(locales.MsgGit()+gitErrLine(err), true)
			}
			if refreshErr == nil {
				a.updateGitChangeViews(root, name, working, index)
			}
			if root == a.git.root {
				a.refreshGit()
			} else {
				workingCopy.refreshSeq++
				workingCopy.loading = false
				a.refreshGitWorkingCopy(root)
			}
		})
	}()
}

func applyCurrentGitDiffPart(ctx context.Context, root, name string, staged bool, digest [sha256.Size]byte, part gitDiffPart) error {
	ch, err := git.PathChange(ctx, root, name)
	if err != nil {
		return err
	}
	if ch.Path != "" && ch.Status() != '!' && !(staged && ch.Index == '?') {
		view, err := readGitChangeView(ctx, root, ch, staged)
		if err != nil {
			return err
		}
		if view.partial != nil && view.partial.digest == digest {
			return view.partial.apply(ctx, root, name, staged, view.file, part)
		}
	}
	return errors.New(gitPartialText("gitPartialDiffChanged"))
}

func (a *App) updateGitChangeViews(root, name string, working, index gitChangeView) {
	for _, g := range a.groups() {
		for _, view := range []struct {
			staged bool
			patch  gitChangeView
		}{{false, working}, {true, index}} {
			v := g.diffs[a.gitChangeTabKey(root, git.Change{Path: name}, view.staged)]
			if v == nil || v.state == nil || !v.change || v.root != root && !(v.root == "" && root == a.git.root) {
				continue
			}
			if v.partial != nil && view.patch.partial != nil && v.partial.digest == view.patch.partial.digest {
				continue
			}
			hunk, _ := v.state.CurrentHunk()
			a.setGitChangeView(v, view.patch, hunk)
		}
	}
}

func (a *App) refreshGitWorkingDiffs(root string, changes []git.Change) {
	if root == "" || a.wsCtx == nil {
		return
	}
	byPath := make(map[string]git.Change, len(changes))
	for _, change := range changes {
		byPath[change.Path] = change
	}
	ctx, gen := a.wsCtx, a.gen
	workingCopy := a.git.workingCopies[root]
	for _, g := range a.groups() {
		for key, view := range g.diffs {
			if view == nil || !view.change || view.state == nil || a.gitDiffRoot(view) != root {
				continue
			}
			view.changeRefreshSeq++
			seq := view.changeRefreshSeq
			change, ok := byPath[view.name]
			if !ok || change.Conflicted() || !gitChangeInSection(change, view.staged) {
				a.setGitChangeView(view, gitChangeView{}, 0)
				continue
			}
			go func(g *group, key string, view *gitDiffTab, seq uint64, change git.Change, staged bool) {
				updated, err := readGitChangeView(ctx, root, change, staged)
				a.post.Post(func() {
					if a.gen != gen || g.diffs[key] != view || view.changeRefreshSeq != seq ||
						a.gitDiffRoot(view) != root || workingCopy != nil && a.git.workingCopies[root] != workingCopy {
						return
					}
					if err != nil {
						a.note(locales.MsgGitDiff()+err.Error(), true)
						updated = gitChangeView{}
					}
					hunk, _ := view.state.CurrentHunk()
					a.setGitChangeView(view, updated, hunk)
				})
			}(g, key, view, seq, change, view.staged)
		}
	}
}
