package app

import (
	"fmt"
	"net/url"
	"path/filepath"
	"see/locales"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/workspace"
)

// Links to code, as Peek's: a reference to the lines selected, to paste
// to a person or an agent, and their page on the repository's host.

// gitRelPath is path relative to the repository's root, with slashes,
// through the workspace's real location when git reports that one.
func (a *App) gitRelPath(path string) (string, bool) {
	root := a.git.root
	if root == "" {
		return "", false
	}
	if !workspace.Within(root, path) && a.realRoot != "" && workspace.Within(a.root, path) {
		if sub, err := filepath.Rel(a.root, path); err == nil {
			path = filepath.Join(a.realRoot, sub)
		}
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || !workspace.Within(root, path) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// selectedLines are the first and last lines the selection touches, from
// 1; a selection ending at the start of a line leaves that line out.
func selectedLines(st *editor.State) (first, last int) {
	s := st.Selection()
	from, to := min(s.Anchor, s.Head), max(s.Anchor, s.Head)
	p, q := st.Buffer.Pos(from), st.Buffer.Pos(to)
	if q.Line > p.Line && q.Col == 0 {
		q.Line--
	}
	return p.Line + 1, q.Line + 1
}

// codeReference is path:line, or path:first-last, the path from the
// workspace's root.
func (a *App) codeReference(d *doc, st *editor.State) string {
	first, last := selectedLines(st)
	ref := fmt.Sprintf("%s:%d", workspace.RelPath(a.root, d.path), first)
	if last > first {
		ref += fmt.Sprintf("-%d", last)
	}
	return ref
}

// webLink is the selected lines' page on the repository's host, at the
// commit checked out, or "" when there is none.
func (a *App) webLink(d *doc, st *editor.State) string {
	rel, ok := a.gitRelPath(d.path)
	if !ok || a.git.web == "" || a.git.head == "" {
		return ""
	}
	first, last := selectedLines(st)
	anchor := fmt.Sprintf("#L%d", first)
	if last > first {
		anchor += fmt.Sprintf("-L%d", last)
	}
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return a.git.web + "/blob/" + a.git.head + "/" + strings.Join(parts, "/") + anchor
}

// commitLink is a commit's page on the repository's host, or "".
func (a *App) commitLink(hash string) string {
	if a.git.web == "" {
		return ""
	}
	return a.git.web + "/commit/" + hash
}

// webLinkAt is the lines' page on the repository's host at hash, the
// commit read in a history view, where webLink's is the one checked
// out. Lines are from 1; none, both 0, link the file alone.
func (a *App) webLinkAt(hash, rel string, first, last int) string {
	if a.git.web == "" || hash == "" || rel == "" {
		return ""
	}
	anchor := ""
	if first > 0 {
		anchor = fmt.Sprintf("#L%d", first)
		if last > first {
			anchor += fmt.Sprintf("-L%d", last)
		}
	}
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return a.git.web + "/blob/" + hash + "/" + strings.Join(parts, "/") + anchor
}

// patchFileLine maps a line of the new side's text, from 0, to the
// file's, from 1: a diff shown hunk by hunk (DiffState.SetPatch) is
// the hunks' new lines one after another, the patch's hunks say where
// each is of the file; one of whole texts, as of --unified=max
// patches, is the file, its lines the text's.
func patchFileLine(p editor.FilePatch, full bool, line int) int {
	if full {
		return line + 1
	}
	textStart, fileStart := 0, 0
	for _, h := range p.Hunks {
		if textStart > line {
			break
		}
		fileStart = max(h.NewStart-1, 0) - textStart
		textStart += h.NewLines
	}
	return fileStart + line + 1
}

// patchFull reports whether a diff of p holds the files whole, as
// setPatch decides: one hunk from the files' first lines.
func patchFull(p editor.FilePatch) bool {
	if len(p.Hunks) != 1 || p.Binary {
		return false
	}
	h := p.Hunks[0]
	return h.OldStart <= 1 && h.NewStart <= 1
}

// diffFileLines are the lines of the file a range of the new side is
// of; full is patchFull's of the patch the diff shows.
func diffFileLines(p editor.FilePatch, full bool, r editor.DiffLineRange) (first, last int) {
	first, last = patchFileLine(p, full, r.Start), patchFileLine(p, full, r.End)
	if first > last {
		first, last = last, first
	}
	return first, last
}

// diffLinkTarget is what a diff tab's permalink needs: the commit the
// diff reads, the file's path in the repository, and the patch the
// diff shows, for the lines' numbers in it.
type diffLinkTarget struct {
	hash string
	rel  string
	patch editor.FilePatch
	full bool
}

// revisionLinkTarget is the link target of a revision comparison: the
// commit chosen in it, of the file compared.
func (a *App) revisionLinkTarget(v *gitDiffTab) diffLinkTarget {
	r := v.revision
	if r == nil || r.selected < 0 || r.selected >= len(r.commits) {
		return diffLinkTarget{}
	}
	rel, ok := a.gitRelPath(r.path)
	if !ok {
		return diffLinkTarget{}
	}
	// The comparison holds the commit's whole text, its lines the
	// file's.
	return diffLinkTarget{hash: r.commits[r.selected].Hash, rel: rel, full: true}
}

// commitFileLinkTarget is the link target of a commit's file open as a
// diff: the commit itself. Its patch is of --unified=max, which
// setPatch shows as the files whole when it holds them.
func (a *App) commitFileLinkTarget(v *gitDiffTab) diffLinkTarget {
	if v.hash == "" {
		return diffLinkTarget{}
	}
	// v.name is the file's path in the repository, as openCommitFile
	// is given it.
	return diffLinkTarget{hash: v.hash, rel: filepath.ToSlash(v.name), patch: v.patch, full: patchFull(v.patch)}
}

// diffLinkURL is the target's page on the repository's host, with the
// diff's selected lines when there are, or "" when the repository
// has no host or the target no commit. Lines of the old side are left
// out: the commit linked has not them, they are its parent's.
func (a *App) diffLinkURL(v *gitDiffTab, t diffLinkTarget) string {
	first, last := 0, 0
	if v.lineSelOn && v.lineSel.Side == editor.DiffNew {
		first, last = diffFileLines(t.patch, t.full, v.lineSel)
	}
	return a.webLinkAt(t.hash, t.rel, first, last)
}

// diffLinkButton is the permalink button of a diff reading a commit:
// it copies the page of the diff's lines at that commit, as the
// editor's Copy Link menu item does of the lines selected at the
// commit checked out.
func (a *App) diffLinkButton(c *ui.Context, v *gitDiffTab, t diffLinkTarget) {
	link := a.diffLinkURL(v, t)
	if link == "" {
		return
	}
	host := hostName(a.git.web)
	label := "Copy " + host + " Link"
	b := ui.ButtonBase(c).Label(label).Tooltip(link).Height(22).Padding(0, 8).Radius(4).KeepFocus().Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(colors(c).Pill)
	}
	b.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			symbolIcon(c, icLink, 11, colors(c).Muted)
			ui.Text(c, label).FontSize(11).TextColor(colors(c).Text).SingleLine()
		})
	})
	if b.Clicked() {
		a.services.WriteClipboard(link)
		a.note(locales.MsgCopiedThe()+host+locales.MsgLink(), false)
	}
}

// hostName names the repository's host for a menu: GitHub, GitLab, or
// the host itself.
func hostName(web string) string {
	u, err := url.Parse(web)
	if err != nil {
		return "Remote"
	}
	switch host := strings.TrimPrefix(u.Hostname(), "www."); host {
	case "github.com":
		return "GitHub"
	case "gitlab.com":
		return "GitLab"
	case "bitbucket.org":
		return "Bitbucket"
	case "codeberg.org":
		return "Codeberg"
	default:
		return host
	}
}

// linkItems are the editor's menu items for links to its lines.
func (a *App) linkItems(menu *ui.Menu, d *doc, st *editor.State) {
	if d.untitled {
		return
	}
	if menu.Item(editorContextMenuText("editorContextCopyCodeReference", nil)).Chosen() {
		ref := a.codeReference(d, st)
		a.services.WriteClipboard(ref)
		a.note(locales.MsgCopied()+ref+".", false)
	}
	if link := a.webLink(d, st); link != "" {
		host := hostName(a.git.web)
		if menu.Item(editorContextMenuText("editorContextCopyHostLink", map[string]any{"host": host})).Chosen() {
			a.services.WriteClipboard(link)
			a.note(locales.MsgCopiedThe()+host+locales.MsgLink(), false)
		}
		if menu.Item(editorContextMenuText("editorContextOpenOnHost", map[string]any{"host": host})).Chosen() {
			a.services.OpenURL(link)
		}
	}
}
