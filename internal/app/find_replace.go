package app

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/egoist/mygo/ui"

	"see/internal/search"
	"see/locales"
)

func (a *App) findReplaceControls(c *ui.Context) {
	f := &a.find
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		if ui.TextInput(c, &f.replacement).Placeholder(locales.MsgReplaceWith()).Label(locales.MsgReplaceWith()).
			Tooltip(locales.MsgReplaceLiteralHint()).Grow(1).Shrink(1).Disabled(f.replacing).Changed() {
			f.confirm = false
			f.replaceNote = ""
		}
		ui.Button(c, locales.WorkspaceReplaceAll()+"…").Shrink(0).
			Disabled(!f.canReplace() || f.stats.Truncated || f.confirm).
			Tooltip(locales.MsgReviewWorkspaceReplacement()).OnClick(func() {
			if f.canReplace() && !f.stats.Truncated {
				f.confirm = true
			}
		})
	})
	switch {
	case f.confirm && f.canReplace():
		ui.Column(c).FillWidth().Gap(6).Padding(8).Radius(6).Background(colors(c).WarnBg).Children(func() {
			ui.Text(c, locales.MsgConfirmWorkspaceReplace(f.stats.Matches, f.stats.Files)).FontSize(12).Bold().Wrap()
			ui.Text(c, locales.MsgWorkspaceReplaceDiskHint()).FontSize(11).Wrap()
			ui.Row(c).Gap(6).Children(func() {
				ui.Button(c, locales.Cancel()).OnClick(func() { f.confirm = false })
				ui.PrimaryButton(c, locales.WorkspaceReplaceAll()).OnClick(func() { a.replaceFind(nil) })
			})
		})
	case f.replacing:
		ui.Text(c, locales.MsgWorkspaceReplacing()).FontSize(11).TextColor(colors(c).Muted).Role(ui.RoleStatus)
	case f.replaceNote != "":
		color := colors(c).Muted
		if f.replaceErr {
			color = colors(c).Err
		}
		ui.Text(c, f.replaceNote).FontSize(11).Wrap().TextColor(color).Role(ui.RoleStatus)
	case f.stats.Truncated && f.canReplace():
		ui.Text(c, locales.MsgReplaceSearchLimit()).FontSize(11).Wrap().TextColor(colors(c).Muted)
	}
}

func (f *findState) canReplace() bool {
	return f.showReplace && !f.running && !f.replacing && f.err == nil && f.opts == f.ran && f.stats.Matches > 0
}

func canonicalDiskPath(path string) string {
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return resolved
	}
	return clean
}

func sameDiskPath(a, b string) bool {
	return canonicalDiskPath(a) == canonicalDiskPath(b)
}

func (a *App) docsForDiskPath(path string) []*doc {
	var out []*doc
	for docPath, d := range a.docs {
		if docPath == path || sameDiskPath(docPath, path) {
			out = append(out, d)
		}
	}
	return out
}

func (a *App) replaceProtected(path string) bool {
	for _, d := range a.docsForDiskPath(path) {
		if d.lossy || d.conflict || d.saving || d.buf != nil && d.buf.Modified() {
			return true
		}
	}
	return false
}

// replaceFind writes only the matches from the displayed search, not a new search.
func (a *App) replaceFind(one *findKey) {
	f := &a.find
	if !f.canReplace() || one == nil && (!f.confirm || f.stats.Truncated) {
		return
	}
	files := f.results
	if one != nil {
		file := a.findFile(one.path)
		if file == nil || one.match < 0 || one.match >= len(file.Matches) {
			return
		}
		files = []search.File{{Path: file.Path, Matches: file.Matches[one.match : one.match+1]}}
	}
	f.confirm, f.replacing, f.replaceNote = false, true, ""

	var targets []search.File
	skipped := 0
	var firstIssue string
	for _, file := range files {
		if a.replaceProtected(file.Path) {
			skipped++
			if firstIssue == "" {
				firstIssue = locales.MsgReplaceProtected(filepath.Base(file.Path))
			}
			continue
		}
		targets = append(targets, file)
	}
	ctx, gen, replacement := a.wsCtx, a.gen, f.replacement
	targetRoots := make([]string, len(targets))
	for i, file := range targets {
		targetRoots[i] = a.workspaceRootForPath(file.Path)
	}
	go func() {
		var updated []string
		roots := make(map[string]*os.Root)
		rootErrors := make(map[string]error)
		count := 0
		for i, file := range targets {
			if ctx.Err() != nil {
				break
			}
			root := targetRoots[i]
			if root == "" {
				skipped++
				if firstIssue == "" {
					firstIssue = replaceIssue(file.Path, os.ErrInvalid)
				}
				continue
			}
			dir := roots[root]
			if dir == nil && rootErrors[root] == nil {
				dir, rootErrors[root] = os.OpenRoot(root)
				if dir != nil {
					roots[root] = dir
				}
			}
			if dir == nil {
				err := rootErrors[root]
				skipped++
				if firstIssue == "" {
					firstIssue = replaceIssue(file.Path, err)
				}
				continue
			}
			rel, err := filepath.Rel(root, file.Path)
			var n int
			if err == nil {
				n, err = search.ReplaceFile(ctx, dir, rel, file.Matches, replacement)
			}
			if err != nil {
				skipped++
				if firstIssue == "" {
					firstIssue = replaceIssue(file.Path, err)
				}
			} else if n > 0 {
				updated = append(updated, file.Path)
				count += n
			}
		}
		for _, dir := range roots {
			_ = dir.Close()
		}
		a.post.Post(func() {
			if gen != a.gen {
				return
			}
			f.replacing = false
			f.replaceErr = skipped > 0
			f.replaceNote = locales.MsgWorkspaceReplaced(count)
			if skipped > 0 {
				f.replaceNote += " " + locales.MsgWorkspaceReplaceSkipped(skipped) + " " + firstIssue
			}
			for _, path := range updated {
				for _, d := range a.docsForDiskPath(path) {
					d.stamp = fileStamp{}
					a.loadDoc(d)
				}
			}
			if len(updated) > 0 {
				a.invalidateHover()
				a.refreshGit()
			}
			a.runFind()
		})
	}()
}

func replaceIssue(path string, err error) string {
	name := filepath.Base(path)
	switch {
	case errors.Is(err, search.ErrStaleMatches):
		return locales.MsgReplaceStale(name)
	case errors.Is(err, search.ErrNotText):
		return locales.MsgReplaceUnsupported(name)
	default:
		return locales.MsgWorkspaceReplaceFailed(err.Error())
	}
}
