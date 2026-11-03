package app

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/config"
	"see/internal/git"
)

func (a *App) resizedWorkspace(width, height int) {
	if a.root == "" || width < 480 || height < 320 || width > 10000 || height > 10000 {
		return
	}
	if a.windowWidth != width || a.windowHeight != height {
		a.windowWidth, a.windowHeight = width, height
		a.saveConfigSoon()
	}
}

func (a *App) captureSession() {
	var snapshot func(*layout) *config.Layout
	snapshot = func(n *layout) *config.Layout {
		if n == nil {
			return nil
		}
		if n.g == nil {
			return &config.Layout{Vertical: n.vertical, Percent: n.pct, First: snapshot(n.a), Second: snapshot(n.b)}
		}
		g := n.g
		s := &config.Layout{Active: tabPath(g.active), ActivePreview: isMarkdownTab(g.active), Preview: g.preview, Focused: g == a.focus}
		if isDiffTab(g.active) {
			s.Active = g.active
		}
		for _, key := range g.order {
			if isDiffTab(key) {
				if spec := diffTabSpec(g.diffs[key]); spec != nil {
					s.Tabs = append(s.Tabs, config.Tab{Diff: spec})
				}
				continue
			}
			path := tabPath(key)
			tab := config.Tab{Path: path, MarkdownPreview: !isMarkdownTab(key) && g.markdownPreview[path], PreviewTab: isMarkdownTab(key)}
			if d := a.docs[path]; d != nil && d.untitled {
				tab.Untitled, tab.Name = true, d.name
			}
			if ed := g.eds[path]; ed != nil {
				p := ed.CursorPos()
				tab.Line, tab.Column = p.Line, p.Col
			} else if p := g.pending[path].pos; p != nil {
				tab.Line, tab.Column = p.Line, p.Col
			}
			s.Tabs = append(s.Tabs, tab)
		}
		return s
	}
	if a.cfg.Workspaces == nil {
		a.cfg.Workspaces = make(map[string]config.Workspace)
	}
	a.cfg.Workspaces[a.root] = config.Workspace{Layout: snapshot(a.layout), SidebarHidden: a.sidebarOff, Navigator: a.navigator, ChangesClosed: a.git.changesClosed, HistoryClosed: a.git.historyClosed, GitSplit: a.git.split, WindowWidth: a.windowWidth, WindowHeight: a.windowHeight}
}

func (a *App) restoreSession() {
	s, ok := a.cfg.Workspaces[a.root]
	if !ok || s.Layout == nil {
		return
	}
	a.restoringSession = true
	defer func() { a.restoringSession = false }()
	a.sidebarOff = s.SidebarHidden
	if s.Navigator >= navFiles && s.Navigator <= navGit {
		a.navigator = s.Navigator
	}
	a.git.changesClosed, a.git.historyClosed, a.git.split = s.ChangesClosed, s.HistoryClosed, s.GitSplit
	var focused *group
	var restore func(*config.Layout, *layout, int) *layout
	restore = func(saved *config.Layout, parent *layout, depth int) *layout {
		if saved == nil || depth > 32 {
			return nil
		}
		if saved.First != nil && saved.Second != nil {
			n := &layout{parent: parent, vertical: saved.Vertical, pct: saved.Percent}
			if n.pct < 5 || n.pct > 95 {
				n.pct = 50
			}
			n.a, n.b = restore(saved.First, n, depth+1), restore(saved.Second, n, depth+1)
			if n.a == nil || n.b == nil {
				return nil
			}
			return n
		}
		g := a.newGroup()
		n := &layout{g: g, parent: parent}
		for _, tab := range saved.Tabs {
			if tab.Diff != nil {
				a.restoreDiffTab(g, *tab.Diff)
				continue
			}
			key := tab.Path
			if tab.PreviewTab {
				key = markdownTab(tab.Path)
			}
			if !filepath.IsAbs(tab.Path) || g.has(key) || (tab.PreviewTab && !isMarkdown(tab.Path)) {
				continue
			}
			pos := editor.Pos{Line: max(tab.Line, 0), Col: max(tab.Column, 0)}
			if tab.Untitled && a.docs[tab.Path] == nil {
				if !validDraftPath(tab.Path) {
					continue
				}
				d := &doc{path: tab.Path, uri: uriOf(tab.Path), name: tab.Name, untitled: true, cache: &draftCache{}}
				if d.name == "" {
					d.name = "Untitled"
				}
				a.docs[tab.Path] = d
				a.loadDoc(d)
			}
			if tab.PreviewTab {
				d := a.docs[tab.Path]
				if d == nil {
					d = &doc{path: tab.Path, uri: uriOf(tab.Path), name: filepath.Base(tab.Path)}
					a.docs[tab.Path] = d
					a.loadDoc(d)
				}
				g.addTab(key, len(g.order))
				a.activateIn(g, key)
				a.showLoaded(g, d)
			} else {
				a.openIn(g, tab.Path, goTo{pos: &pos})
			}
			if tab.MarkdownPreview {
				if g.markdownPreview == nil {
					g.markdownPreview = map[string]bool{}
				}
				g.markdownPreview[tab.Path] = true
			}
		}
		active := saved.Active
		if saved.ActivePreview {
			active = markdownTab(active)
		}
		if g.has(active) {
			g.active = active
		}
		if g.has(saved.Preview) {
			g.preview = saved.Preview
		}
		if g.active != "" {
			g.mru = slices.DeleteFunc(g.mru, func(p string) bool { return p == g.active })
			g.mru = append(g.mru, g.active)
		}
		if saved.Focused {
			focused = g
		}
		return n
	}
	if restored := restore(s.Layout, nil, 0); restored != nil {
		a.layout = restored
		if focused == nil {
			focused = a.groups()[0]
		}
		a.focus = focused
		if p := a.tabFile(focused, focused.active); p != "" {
			a.revealInTree(p)
		}
	}
}

// diffTabSpec is what of a Git tab the session keeps, nil for one it
// cannot read again.
func diffTabSpec(v *gitDiffTab) *config.DiffTab {
	switch {
	case v == nil:
		return nil
	case v.commit != nil || v.rev != "":
		if v.hash == "" {
			return nil
		}
		spec := &config.DiffTab{Commit: v.hash}
		if v.commit == nil {
			spec.Path, spec.OldPath = v.name, v.oldPath
		}
		return spec
	}
	return &config.DiffTab{Path: v.name, Staged: v.staged, Untracked: v.untracked}
}

// restoreDiffTab puts a Git tab of the last session in g, empty until
// the repository is read (loadPendingTabs).
func (a *App) restoreDiffTab(g *group, spec config.DiffTab) {
	if spec.Commit != "" && !isHash(spec.Commit) || spec.Path != "" && !relGitPath(spec.Path) || spec.OldPath != "" && !relGitPath(spec.OldPath) {
		return
	}
	var key string
	v := &gitDiffTab{name: spec.Path, staged: spec.Staged, untracked: spec.Untracked, hash: spec.Commit, oldPath: spec.OldPath, pending: true}
	switch {
	case spec.Commit != "" && spec.Path == "":
		key = commitTab(spec.Commit)
		v.rev, v.name = spec.Commit[:7], "Commit "+spec.Commit[:7]
		v.commit = &commitView{commit: git.Commit{Hash: spec.Commit, Short: v.rev, Subject: v.name}, loading: true}
	case spec.Commit != "":
		key, v.rev = commitFileTab(spec.Commit, spec.Path), spec.Commit[:7]
	case spec.Path != "":
		key = diffTab(spec.Path, spec.Staged)
	default:
		return
	}
	if g.has(key) {
		return
	}
	if g.diffs == nil {
		g.diffs = map[string]*gitDiffTab{}
	}
	g.diffs[key] = v
	g.addTab(key, len(g.order))
	a.git.pendingTabs = append(a.git.pendingTabs, pendingTab{key: key, v: v})
}

func isHash(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// relGitPath reports whether p is a path in a repository, as git writes
// one: relative, with slashes, and not out of it.
func relGitPath(p string) bool {
	return p != "" && !strings.HasPrefix(p, "/") && !filepath.IsAbs(p) && !slices.Contains(strings.Split(p, "/"), "..")
}

// pendingTab is a Git tab of the last session waiting to be read.
type pendingTab struct {
	key string
	v   *gitDiffTab
}

// loadPendingTabs reads the Git tabs of the last session once the
// repository is known. A change that is no longer there, or a commit no
// longer in the repository, closes its tab quietly; without a
// repository, they all close.
func (a *App) loadPendingTabs(repo bool) {
	pending := a.git.pendingTabs
	a.git.pendingTabs = nil
	for _, p := range pending {
		if !repo {
			a.dropPendingTab(p, "no Git repository")
			continue
		}
		v := p.v
		gen, ctx, root := a.gen, a.wsCtx, a.git.root
		go func() {
			var (
				cm      git.Commit
				details git.CommitDetails
				out     []byte
				err     error
			)
			switch {
			case v.commit != nil:
				if cm, err = git.ReadCommit(ctx, root, v.hash); err == nil {
					details, err = git.Show(ctx, root, v.hash)
				}
			case v.hash != "":
				out, err = git.CommitFileDiff(ctx, root, v.hash, git.FileStat{Path: v.name, OldPath: v.oldPath})
			default:
				out, err = changeDiff(ctx, root, v.name, v.staged, v.untracked)
			}
			var patches []editor.FilePatch
			if err == nil && v.commit == nil {
				patches, err = editor.ParsePatch(string(out))
				if err == nil && (len(patches) == 0 || patches[0].Binary) {
					err = errors.New("no text diff")
				}
			}
			a.post.Post(func() {
				if a.gen != gen {
					return
				}
				if err != nil {
					a.dropPendingTab(p, gitErrLine(err))
					return
				}
				v.pending = false
				if v.commit != nil {
					v.name = cm.Subject
					v.commit.commit, v.commit.details, v.commit.loading = cm, details, false
				} else {
					nv := a.newGitDiffTab(patches[0], v.name, v.staged)
					v.state, v.patch = nv.state, nv.patch
				}
				for _, g := range a.groups() {
					if g == a.focus && g.diffs[p.key] == v && g.active == p.key {
						if f := a.tabFile(g, p.key); f != "" {
							a.revealInTree(f)
						}
					}
				}
			})
		}()
	}
}

// dropPendingTab closes a Git tab of the last session that cannot be
// read again.
func (a *App) dropPendingTab(p pendingTab, why string) {
	a.logf("session: dropping %q: %s", strings.TrimPrefix(p.key, diffTabPrefix), why)
	for _, g := range a.groups() {
		if g.diffs[p.key] == p.v {
			a.closeTab(g, p.key)
		}
	}
}
