package app

import (
	"fmt"
	"net/url"
	"path/filepath"
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
	menu.Separator()
	if menu.Item("Copy Code Reference").Chosen() {
		ref := a.codeReference(d, st)
		a.services.WriteClipboard(ref)
		a.note("Copied "+ref+".", false)
	}
	if link := a.webLink(d, st); link != "" {
		host := hostName(a.git.web)
		if menu.Item("Copy " + host + " Link").Chosen() {
			a.services.WriteClipboard(link)
			a.note("Copied the "+host+" link.", false)
		}
		if menu.Item("Open on " + host).Chosen() {
			a.services.OpenURL(link)
		}
	}
}
